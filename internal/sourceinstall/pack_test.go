package sourceinstall

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func packJSON(t *testing.T, fields map[string]any) []byte {
	t.Helper()
	document := map[string]any{
		"schema": PackSchema, "id": "nagare-source-demo", "name": " 示例来源包 ", "description": "两个来源",
		"version": "abc123", "sources": []any{
			map[string]any{"id": "alpha", "name": "Alpha"},
			map[string]any{"id": "beta", "name": "Beta <b>"},
		},
	}
	for key, value := range fields {
		if value == nil {
			delete(document, key)
		} else {
			document[key] = value
		}
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(document); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestParseBundleSplitsPackIntoIndependentJSONSources(t *testing.T) {
	bundle, err := ParseBundle(Download{Format: "json", Content: packJSON(t, map[string]any{"extra": true})})
	if err != nil {
		t.Fatal(err)
	}
	if !bundle.Pack || bundle.ID != "nagare-source-demo" || bundle.Name != "示例来源包" || bundle.Version != "abc123" || bundle.Description != "两个来源" {
		t.Fatalf("bundle header = %+v", bundle)
	}
	if len(bundle.Members) != 2 {
		t.Fatalf("members = %d", len(bundle.Members))
	}
	want := "{\n  \"id\": \"beta\",\n  \"name\": \"Beta <b>\"\n}\n"
	if bundle.Members[1].Format != "json" || string(bundle.Members[1].Content) != want {
		t.Fatalf("member = %q (%s), want %q", bundle.Members[1].Content, bundle.Members[1].Format, want)
	}
}

func TestParseBundleTreatsOtherDocumentsAsSingleSource(t *testing.T) {
	cases := []Download{
		{Format: "yaml", Content: []byte("schema: nagare-source-pack/v1\nid: demo\n")},
		{Format: "json", Content: []byte(`{"id":"demo","schema":"nagare-source/v1"}`)},
		{Format: "json", Content: []byte(`{"id":"demo","schema":7}`)},
		{Format: "json", Content: []byte(`{"id":`)},
	}
	for _, download := range cases {
		bundle, err := ParseBundle(download)
		if err != nil {
			t.Fatalf("ParseBundle(%s) = %v", download.Content, err)
		}
		if bundle.Pack || len(bundle.Members) != 1 || string(bundle.Members[0].Content) != string(download.Content) || bundle.Members[0].Format != download.Format {
			t.Fatalf("ParseBundle(%s) = %+v", download.Content, bundle)
		}
	}
}

func TestParseBundleRejectsInvalidPacks(t *testing.T) {
	tooMany := make([]any, MaxPackSources+1)
	for index := range tooMany {
		tooMany[index] = map[string]any{"id": fmt.Sprintf("s%02d", index)}
	}
	cases := []struct {
		name    string
		content []byte
		want    string
	}{
		{"future schema", packJSON(t, map[string]any{"schema": "nagare-source-pack/v2"}), "不支持的来源包格式"},
		{"bad id", packJSON(t, map[string]any{"id": "../pack"}), "ID"},
		{"empty name", packJSON(t, map[string]any{"name": "  "}), "名称"},
		{"long name", packJSON(t, map[string]any{"name": strings.Repeat("名", 81)}), "名称"},
		{"control in name", packJSON(t, map[string]any{"name": "a\u0007b"}), "名称"},
		{"long description", packJSON(t, map[string]any{"description": strings.Repeat("说", 501)}), "说明"},
		{"missing version", packJSON(t, map[string]any{"version": nil}), "版本"},
		{"padded version", packJSON(t, map[string]any{"version": " 1 "}), "版本"},
		{"no sources", packJSON(t, map[string]any{"sources": []any{}}), "没有来源"},
		{"too many sources", packJSON(t, map[string]any{"sources": tooMany}), "最多"},
		{"non-object member", packJSON(t, map[string]any{"sources": []any{"id: demo"}}), "不是 JSON 对象"},
		{"null member", packJSON(t, map[string]any{"sources": []any{nil}}), "不是 JSON 对象"},
		{"wrong field type", packJSON(t, map[string]any{"sources": "alpha"}), "格式不对"},
		{"trailing content", append(packJSON(t, nil), []byte(" {}")...), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseBundle(Download{Format: "json", Content: tc.content})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ParseBundle error = %v, want %q", err, tc.want)
			}
			if tc.name == "future schema" && !errors.Is(err, ErrUnsupportedPack) {
				t.Fatalf("future schema error is not ErrUnsupportedPack: %v", err)
			}
		})
	}
}
