package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	errs "github.com/nagare-project/nagare/internal/errors"
)

const fields = `id isAdult type countryOfOrigin title { romaji english native } coverImage { extraLarge large } bannerImage trailer { id site } seasonYear season episodes meanScore genres description(asHtml:false) status format duration source startDate { year month day } nextAiringEpisode { episode airingAt } studios(isMain:true) { nodes { name } }`
const detailQuery = `query($id:Int!) { Media(id:$id,type:ANIME) { ` + fields + ` relations { edges { relationType node { ` + fields + ` } } } recommendations(perPage:12,sort:RATING_DESC) { nodes { mediaRecommendation { ` + fields + ` } } } characters(perPage:12,sort:ROLE) { edges { role node { name { full } image { large } } voiceActors(language:JAPANESE) { name { full } image { large } } } } rankings { rank type context year season } } }`

var tags = regexp.MustCompile(`<[^>]*>`)
var trailerID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

type cached struct {
	data    json.RawMessage
	expires time.Time
}

// Client 对公开查询限速、去重并缓存；图片地址只从可信元数据登记，浏览器不能指定任意 URL。
type Client struct {
	mu          sync.Mutex
	http        *http.Client
	endpoint    string
	cache       map[string]cached
	lastRequest time.Time
	imageMu     sync.RWMutex
	images      map[string]string
	prefix      string
}

func New(endpoint string, hc *http.Client) *Client {
	if endpoint == "" {
		endpoint = "https://graphql.anilist.co"
	}
	if hc == nil {
		hc = &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return &Client{http: hc, endpoint: endpoint, cache: map[string]cached{}, images: map[string]string{}}
}
func (c *Client) SetArtPrefix(prefix string) {
	c.imageMu.Lock()
	defer c.imageMu.Unlock()
	c.prefix = prefix + "/catalog/"
}
func (c *Client) ImageSource(key string) (string, bool) {
	c.imageMu.RLock()
	defer c.imageMu.RUnlock()
	v, ok := c.images[key]
	return v, ok
}
func (c *Client) Artwork(source string) string { return c.image(source) }
func (c *Client) image(source string) string {
	u, err := url.Parse(source)
	if err != nil || u.Scheme != "https" || u.Hostname() != "s4.anilist.co" || u.User != nil {
		return ""
	}
	sum := sha256.Sum256([]byte(source))
	key := hex.EncodeToString(sum[:])
	c.imageMu.Lock()
	defer c.imageMu.Unlock()
	if c.prefix == "" {
		return ""
	}
	c.images[key] = source
	return c.prefix + key
}
func (c *Client) query(ctx context.Context, query string, variables map[string]any) (json.RawMessage, error) {
	body, _ := json.Marshal(map[string]any{"query": query, "variables": variables})
	key := string(body)
	// 单个公开元数据请求串行，避免页面组件同时挂载时冲击 AniList 配额。
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.cache[key]; ok && time.Now().Before(v.expires) {
		return v.data, nil
	}
	if delay := time.Until(c.lastRequest.Add(time.Second)); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "nagare/catalog")
	c.lastRequest = time.Now()
	res, err := c.http.Do(req)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryNetwork, "catalog.query", "无法连接作品目录", "请稍后重试", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, errs.New(errs.CategoryUpstream, "catalog.query", fmt.Sprintf("作品目录暂不可用（HTTP %d），请稍后重试", res.StatusCode), "")
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	var env struct {
		Data   json.RawMessage
		Errors []struct{ Message string }
	}
	if json.Unmarshal(raw, &env) != nil || len(env.Errors) > 0 || len(env.Data) == 0 || string(env.Data) == "null" {
		return nil, errs.New(errs.CategoryUpstream, "catalog.decode", "作品目录返回了无效数据，请稍后重试", "")
	}
	for k, v := range c.cache {
		if time.Now().After(v.expires) {
			delete(c.cache, k)
		}
	}
	if len(c.cache) >= 64 {
		for k := range c.cache {
			delete(c.cache, k)
			break
		}
	}
	c.cache[key] = cached{env.Data, time.Now().Add(10 * time.Minute)}
	return env.Data, nil
}
func (c *Client) convert(r rawMedia) Media {
	title := r.Title.Romaji
	if title == "" {
		title = r.Title.English
	}
	if title == "" {
		title = r.Title.Native
	}
	season := map[string]string{"WINTER": "冬", "SPRING": "春", "SUMMER": "夏", "FALL": "秋"}[r.Season]
	cover := r.CoverImage.ExtraLarge
	if cover == "" {
		cover = r.CoverImage.Large
	}
	m := Media{ID: r.ID, Title: title, TitleNative: r.Title.Native, TitleEnglish: r.Title.English, Cover: c.image(cover), Banner: c.image(r.BannerImage), Year: r.SeasonYear, Season: season, Episodes: r.Episodes, Score: r.MeanScore, Genres: r.Genres, Description: html.UnescapeString(tags.ReplaceAllString(r.Description, "")), Status: r.Status, Format: r.Format, Duration: r.Duration, Source: r.Source}
	if m.Year == 0 {
		m.Year = r.StartDate.Year
	}
	if m.Genres == nil {
		m.Genres = []string{}
	}
	if r.Trailer != nil && r.Trailer.Site == "youtube" && trailerID.MatchString(r.Trailer.ID) {
		m.TrailerID = r.Trailer.ID
	}
	if r.StartDate.Year > 0 {
		m.StartDate = fmt.Sprintf("%04d-%02d-%02d", r.StartDate.Year, max(1, r.StartDate.Month), max(1, r.StartDate.Day))
	}
	if r.NextAiringEpisode != nil {
		m.NextAiring = &Airing{r.NextAiringEpisode.Episode, r.NextAiringEpisode.AiringAt}
	}
	for _, s := range r.Studios.Nodes {
		m.Studios = append(m.Studios, s.Name)
	}
	for _, e := range r.Relations.Edges {
		if e.Node != nil && e.Node.ID > 0 && e.Node.Format != "MANGA" && e.Node.Format != "NOVEL" && e.Node.Format != "ONE_SHOT" && e.Node.Format != "MUSIC" && e.RelationType != "CHARACTER" {
			m.Relations = append(m.Relations, Relation{e.RelationType, c.convert(*e.Node)})
		}
	}
	for _, e := range r.Recommendations.Nodes {
		if e.MediaRecommendation != nil {
			m.Recommendations = append(m.Recommendations, c.convert(*e.MediaRecommendation))
		}
	}
	for _, e := range r.Characters.Edges {
		ch := Character{Name: e.Node.Name.Full, Image: c.image(e.Node.Image.Large), Role: e.Role}
		if len(e.VoiceActors) > 0 {
			ch.Actor = e.VoiceActors[0].Name.Full
			ch.ActorImage = c.image(e.VoiceActors[0].Image.Large)
		}
		m.Characters = append(m.Characters, ch)
	}
	m.Rankings = r.Rankings
	return m
}
func (c *Client) Detail(ctx context.Context, id int) (Media, error) {
	raw, err := c.query(ctx, detailQuery, map[string]any{"id": id})
	if err != nil {
		return Media{}, err
	}
	var data struct{ Media *rawMedia }
	if json.Unmarshal(raw, &data) != nil || data.Media == nil {
		return Media{}, errs.New(errs.CategoryFS, "catalog.detail", "找不到这部作品", "")
	}
	return c.convert(*data.Media), nil
}

var sectionArgs = map[string]string{
	"trending":   `sort:TRENDING_DESC,genre:$genre`,
	"thisSeason": `season:$season,seasonYear:$year,sort:SCORE_DESC,genre:$genre`,
	"pastSeason": `season:$pastSeason,seasonYear:$pastYear,sort:SCORE_DESC,genre:$genre`,
	"upcoming":   `status:NOT_YET_RELEASED,sort:TRENDING_DESC`,
	"movies":     `format:MOVIE,sort:TRENDING_DESC`,
}

func (c *Client) Discover(ctx context.Context, section, genre string, now time.Time) (map[string][]Media, error) {
	seasons := []string{"WINTER", "SPRING", "SUMMER", "FALL"}
	quarter := (int(now.Month()) - 1) / 3
	past := (quarter + 3) % 4
	pastYear := now.Year()
	if quarter == 0 {
		pastYear--
	}
	vars := map[string]any{"season": seasons[quarter], "year": now.Year(), "pastSeason": seasons[past], "pastYear": pastYear, "genre": nil, "now": now.Unix(), "week": now.Add(-14 * 24 * time.Hour).Unix()}
	if genre != "" {
		vars["genre"] = genre
	}
	definitions := map[string]string{"season": "MediaSeason", "year": "Int", "pastSeason": "MediaSeason", "pastYear": "Int", "genre": "String", "now": "Int", "week": "Int"}
	var blocks []string
	for _, name := range []string{"trending", "recent", "thisSeason", "pastSeason", "upcoming", "movies"} {
		if section != "" && name != section {
			continue
		}
		if name == "recent" {
			blocks = append(blocks, `recent:Page(perPage:50) { airingSchedules(airingAt_greater:$week,airingAt_lesser:$now,sort:TIME_DESC) { episode airingAt media { `+fields+` } } }`)
		} else {
			blocks = append(blocks, name+`:Page(perPage:20) { media(type:ANIME,isAdult:false,`+sectionArgs[name]+`) { `+fields+` } }`)
		}
	}
	if len(blocks) == 0 {
		return nil, errs.New(errs.CategoryInput, "catalog.discover", "无效的发现分类", "")
	}
	content := strings.Join(blocks, " ")
	var params []string
	for _, name := range []string{"season", "year", "pastSeason", "pastYear", "genre", "now", "week"} {
		if strings.Contains(content, "$"+name) {
			params = append(params, "$"+name+":"+definitions[name])
		} else {
			delete(vars, name)
		}
	}
	query := "query"
	if len(params) > 0 {
		query += "(" + strings.Join(params, ",") + ")"
	}
	query += " { " + content + " }"
	// 时间取整到十分钟，避免每次请求都生成新的缓存键。
	if _, ok := vars["now"]; ok {
		vars["now"] = now.Unix() / 600 * 600
		vars["week"] = now.Unix()/600*600 - 14*86400
	}
	raw, err := c.query(ctx, query, vars)
	if err != nil {
		return nil, err
	}
	var pages map[string]struct {
		Media           []rawMedia
		AiringSchedules []struct {
			Episode  int
			AiringAt int64
			Media    rawMedia
		}
	}
	if err = json.Unmarshal(raw, &pages); err != nil {
		return nil, err
	}
	result := map[string][]Media{}
	for name, page := range pages {
		result[name] = []Media{}
		for _, r := range page.Media {
			result[name] = append(result[name], c.convert(r))
		}
		for _, a := range page.AiringSchedules {
			if a.Media.IsAdult || a.Media.CountryOfOrigin != "JP" || a.Media.Type != "ANIME" || a.Media.Format == "TV_SHORT" {
				continue
			}
			m := c.convert(a.Media)
			m.RecentAiring = &Airing{a.Episode, a.AiringAt}
			result[name] = append(result[name], m)
		}
	}
	return result, nil
}
