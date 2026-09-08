package sourceplugin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	maximumJSONBytes  = 1 << 20
	maximumEventBytes = 1 << 20
	maximumCandidates = 500
)

type Client struct {
	base     string
	http     *http.Client
	manifest Manifest
}

func NewClient(base string) (*Client, error) {
	normalized, err := validateLoopbackURL(base)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   3 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		DisableCompression: true,
	}
	return &Client{base: normalized, http: &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("plugin redirects are not allowed")
		},
	}}, nil
}

func validateLoopbackURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("plugin URL must be a plain loopback HTTP origin")
	}
	if u.Path != "" && u.Path != "/" {
		return "", errors.New("plugin URL must not contain a path")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return "", errors.New("plugin URL must use an explicit loopback IP")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return "", errors.New("plugin URL must include a valid port")
	}
	u.Path = ""
	return strings.TrimSuffix(u.String(), "/"), nil
}

func (c *Client) Negotiate(ctx context.Context) (Manifest, error) {
	var manifest Manifest
	if err := c.getJSON(ctx, "/v1/manifest", false, &manifest); err != nil {
		return Manifest{}, err
	}
	if strings.TrimSpace(manifest.ID) == "" || strings.TrimSpace(manifest.Name) == "" || strings.TrimSpace(manifest.Version) == "" {
		return Manifest{}, errors.New("plugin manifest identity is incomplete")
	}
	if !containsInt(manifest.ProtocolVersions, ProtocolV1) {
		return Manifest{}, errors.New("plugin does not support Plugin API v1")
	}
	if !containsInt(manifest.SourceSchemaVersions, 1) {
		return Manifest{}, errors.New("plugin does not support Source Schema v1")
	}
	c.manifest = manifest
	return manifest, nil
}

func (c *Client) Sources(ctx context.Context) ([]Source, error) {
	var result struct {
		Sources []Source `json:"sources"`
	}
	if err := c.getJSON(ctx, "/v1/sources", true, &result); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, source := range result.Sources {
		if source.ID == "" || source.Name == "" || (source.Kind != "web" && source.Kind != "bt") || source.Tier < 0 || source.Tier > 9 {
			return nil, errors.New("plugin returned an invalid source")
		}
		if seen[source.ID] {
			return nil, fmt.Errorf("plugin returned duplicate source %q", source.ID)
		}
		seen[source.ID] = true
	}
	return result.Sources, nil
}

func (c *Client) Candidates(ctx context.Context, input ResolveRequest, emit func(Event) error) error {
	if input.Schema != "nagare-resolve-request/v1" || len(input.Subject.IDs) == 0 || len(input.Subject.Titles) == 0 || input.Episode.Number == "" {
		return errors.New("resolve request is incomplete")
	}
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	if len(body) > maximumJSONBytes {
		return errors.New("resolve request exceeds 1 MiB")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/candidates", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Nagare-Protocol-Version", "1")
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("call source plugin: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return responseError(response)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-ndjson" {
		return errors.New("plugin returned an invalid candidate content type")
	}

	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), maximumEventBytes)
	done := false
	candidates := 0
	for scanner.Scan() {
		if done {
			return errors.New("plugin emitted data after done")
		}
		event, err := decodeEvent(scanner.Bytes())
		if err != nil {
			return err
		}
		if event.Event == "candidate" {
			candidates++
			if candidates > maximumCandidates {
				return errors.New("plugin candidate limit exceeded")
			}
		}
		if event.Event == "done" {
			done = true
		}
		if err := emit(event); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read plugin candidate stream: %w", err)
	}
	if !done {
		return errors.New("plugin candidate stream ended without done")
	}
	return nil
}

func (c *Client) Close() {
	if transport, ok := c.http.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
}

func decodeEvent(line []byte) (Event, error) {
	var envelope struct {
		Event string `json:"event"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		return Event{}, errors.New("plugin emitted invalid NDJSON")
	}
	switch envelope.Event {
	case "candidate":
		var wire struct {
			Event     string     `json:"event"`
			Candidate *Candidate `json:"candidate"`
		}
		if err := decodeStrict(line, &wire); err != nil || wire.Candidate == nil {
			return Event{}, errors.New("candidate event has no candidate")
		}
		if err := validateCandidate(*wire.Candidate); err != nil {
			return Event{}, err
		}
		return Event{Event: wire.Event, Candidate: wire.Candidate}, nil
	case "source_error":
		var wire struct {
			Event     string `json:"event"`
			SourceID  string `json:"sourceId"`
			Category  string `json:"category"`
			Message   string `json:"message"`
			Retryable *bool  `json:"retryable"`
		}
		if err := decodeStrict(line, &wire); err != nil || wire.SourceID == "" || wire.Category == "" || wire.Message == "" || wire.Retryable == nil {
			return Event{}, errors.New("plugin emitted an invalid source_error event")
		}
		return Event{Event: wire.Event, SourceID: wire.SourceID, Category: wire.Category, Message: wire.Message, Retryable: *wire.Retryable}, nil
	case "done":
		var wire struct {
			Event      string `json:"event"`
			Queried    *int   `json:"queried"`
			Succeeded  *int   `json:"succeeded"`
			Failed     *int   `json:"failed"`
			DurationMS *int64 `json:"durationMs"`
		}
		if err := decodeStrict(line, &wire); err != nil || wire.Queried == nil || wire.Succeeded == nil || wire.Failed == nil || wire.DurationMS == nil || *wire.Queried < 0 || *wire.Succeeded < 0 || *wire.Failed < 0 || *wire.DurationMS < 0 || *wire.Queried != *wire.Succeeded+*wire.Failed {
			return Event{}, errors.New("plugin emitted an invalid done event")
		}
		return Event{Event: wire.Event, Queried: *wire.Queried, Succeeded: *wire.Succeeded, Failed: *wire.Failed, DurationMS: *wire.DurationMS}, nil
	default:
		return Event{}, fmt.Errorf("plugin emitted unknown event %q", envelope.Event)
	}
}

func validateCandidate(candidate Candidate) error {
	if candidate.Schema != "nagare-candidate/v1" || candidate.ID == "" || len(candidate.ID) > 256 || !validSourceID(candidate.SourceID) || candidate.Tier < 0 || candidate.Tier > 9 || candidate.MatchConfidence < 0 || candidate.MatchConfidence > 1 || len(candidate.Match.Basis) == 0 {
		return errors.New("plugin emitted an invalid candidate")
	}
	bases := map[string]bool{}
	for _, basis := range candidate.Match.Basis {
		switch basis {
		case "episode_id", "subject_id", "title_episode", "air_date":
		default:
			return errors.New("plugin candidate has an invalid match basis")
		}
		if bases[basis] {
			return errors.New("plugin candidate repeats a match basis")
		}
		bases[basis] = true
	}
	switch candidate.Transport.Type {
	case "hls", "http":
		u, err := url.Parse(candidate.Transport.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			return errors.New("plugin candidate has an invalid media URL")
		}
		if candidate.Transport.Magnet != "" || candidate.Transport.InfoHash != "" || candidate.Transport.TorrentURL != "" || candidate.Transport.FileIndex != nil || len(candidate.Transport.Trackers) > 0 {
			return errors.New("plugin media candidate mixes transport fields")
		}
	case "torrent":
		if candidate.Transport.Magnet == "" && candidate.Transport.InfoHash == "" && candidate.Transport.TorrentURL == "" {
			return errors.New("plugin torrent candidate has no locator")
		}
		if candidate.Transport.URL != "" || len(candidate.Transport.Headers) > 0 || candidate.Transport.ExpiresAt != 0 {
			return errors.New("plugin torrent candidate mixes transport fields")
		}
		if candidate.Transport.Magnet != "" && !strings.HasPrefix(candidate.Transport.Magnet, "magnet:?") {
			return errors.New("plugin torrent candidate has an invalid magnet")
		}
		if candidate.Transport.TorrentURL != "" {
			u, err := url.Parse(candidate.Transport.TorrentURL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
				return errors.New("plugin torrent candidate has an invalid torrent URL")
			}
		}
		if candidate.Transport.FileIndex != nil && *candidate.Transport.FileIndex < 0 {
			return errors.New("plugin torrent candidate has an invalid file index")
		}
	default:
		return errors.New("plugin candidate has an unknown transport")
	}
	for name, value := range candidate.Transport.Headers {
		if !validHeader(name, value) {
			return errors.New("plugin candidate has an invalid HTTP header")
		}
	}
	return nil
}

// ValidateCandidate 对从本机 HTTP API 回传的候选执行与插件流相同的边界检查。
// 这样播放端点不会因为候选绕了一次浏览器就降低校验强度。
func ValidateCandidate(candidate Candidate) error { return validateCandidate(candidate) }

func validSourceID(value string) bool {
	if len(value) < 2 || len(value) > 63 || !((value[0] >= 'a' && value[0] <= 'z') || (value[0] >= '0' && value[0] <= '9')) {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
			return false
		}
	}
	return true
}

func validHeader(name, value string) bool {
	if name == "" || strings.TrimSpace(name) != name || strings.ContainsAny(value, "\r\n") {
		return false
	}
	for _, char := range value {
		if (char < 0x20 && char != '\t') || char == 0x7f {
			return false
		}
	}
	for _, char := range name {
		if char <= 32 || char >= 127 || strings.ContainsRune("()<>@,;:\\\"/[]?={}\t", char) {
			return false
		}
	}
	return true
}

func (c *Client) getJSON(ctx context.Context, path string, negotiated bool, output any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	if negotiated {
		request.Header.Set("X-Nagare-Protocol-Version", "1")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("call source plugin: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return responseError(response)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maximumJSONBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maximumJSONBytes {
		return errors.New("plugin JSON response exceeds 1 MiB")
	}
	return decodeStrict(data, output)
}

func responseError(response *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	return fmt.Errorf("plugin returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
}

func decodeStrict(data []byte, output any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("JSON response contains trailing data")
	}
	return nil
}

func containsInt(values []int, wanted int) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
