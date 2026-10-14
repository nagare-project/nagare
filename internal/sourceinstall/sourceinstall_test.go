package sourceinstall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testFetcher(server *httptest.Server) *Fetcher {
	client := server.Client()
	client.CheckRedirect = checkRedirect
	client.Timeout = 5 * time.Second
	return &Fetcher{Client: client, UserAgent: "nagare-test"}
}

func TestValidateURL(t *testing.T) {
	cases := map[string]string{
		"":                                 "不能为空",
		"http://example.test/a.yaml":       "https",
		"https://user:pw@example.test/a":   "账号",
		"https://example.test/a.yaml#frag": "片段",
		"https:///a.yaml":                  "不合法",
		"https://example.test/" + strings.Repeat("a", 2100): "过长",
	}
	for input, want := range cases {
		if _, err := ValidateURL(input); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ValidateURL(%q) = %v, want error containing %q", input, err, want)
		}
	}
	got, err := ValidateURL("  https://example.test/sources/a.yaml?x=1 ")
	if err != nil || got != "https://example.test/sources/a.yaml?x=1" {
		t.Fatalf("ValidateURL(valid) = %q, %v", got, err)
	}
}

func TestNormalizeSHA256(t *testing.T) {
	upper := strings.Repeat("AB", 32)
	if got, ok := NormalizeSHA256(upper); !ok || got != strings.ToLower(upper) {
		t.Fatalf("NormalizeSHA256(upper) = %q, %v", got, ok)
	}
	for _, bad := range []string{"", "abc", strings.Repeat("zz", 32), strings.Repeat("a", 65)} {
		if _, ok := NormalizeSHA256(bad); ok {
			t.Errorf("NormalizeSHA256(%q) accepted", bad)
		}
	}
}

func TestDetectFormat(t *testing.T) {
	if DetectFormat([]byte("\xef\xbb\xbf \n {\"id\":\"a\"}")) != "json" {
		t.Fatal("JSON with BOM and whitespace not detected")
	}
	if DetectFormat([]byte("id: demo\n")) != "yaml" {
		t.Fatal("YAML not detected")
	}
}

func TestFetchDownloadsAndHashes(t *testing.T) {
	body := "id: demo\nname: Demo\n"
	var userAgent string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	got, err := testFetcher(server).Fetch(context.Background(), server.URL+"/demo.yaml")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(body))
	if got.SHA256 != hex.EncodeToString(digest[:]) || got.Format != "yaml" || string(got.Content) != body {
		t.Fatalf("Fetch = %+v", got)
	}
	if userAgent != "nagare-test" {
		t.Fatalf("User-Agent = %q", userAgent)
	}
}

func TestFetchRejectsBadResponses(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{"status", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }, "HTTP 404"},
		{"empty", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("  \n")) }, "空的"},
		{"binary", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte{0xff, 0xfe, 0x00}) }, "UTF-8"},
		{"too large", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("a", MaxSourceBytes+1)))
		}, "1 MiB"},
		{"redirect to http", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://example.test/a.yaml", http.StatusFound)
		}, "非 https"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(tc.handler)
			defer server.Close()
			if _, err := testFetcher(server).Fetch(context.Background(), server.URL+"/a.yaml"); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Fetch error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestFetchFollowsLimitedHTTPSRedirects(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loop":
			http.Redirect(w, r, server.URL+"/loop", http.StatusFound)
		case "/once":
			http.Redirect(w, r, server.URL+"/final.json", http.StatusFound)
		default:
			_, _ = w.Write([]byte(`{"id":"demo"}`))
		}
	}))
	defer server.Close()
	fetcher := testFetcher(server)
	got, err := fetcher.Fetch(context.Background(), server.URL+"/once")
	if err != nil || got.Format != "json" {
		t.Fatalf("redirect once = %+v, %v", got, err)
	}
	if _, err := fetcher.Fetch(context.Background(), server.URL+"/loop"); err == nil || !strings.Contains(err.Error(), "重定向次数") {
		t.Fatalf("redirect loop error = %v", err)
	}
}

func TestGuardedFetcherRefusesLoopback(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("id: demo\n"))
	}))
	defer server.Close()
	fetcher := NewFetcher("nagare-test")
	fetcher.Client.Transport.(*http.Transport).Proxy = nil
	if _, err := fetcher.Fetch(context.Background(), server.URL+"/a.yaml"); err == nil || !strings.Contains(err.Error(), "公网地址") {
		t.Fatalf("loopback fetch error = %v", err)
	}
}

func testPackage(id string, pack bool, sources ...PackageSource) Package {
	return Package{
		ID: id, Name: strings.ToUpper(id), Version: "1", Pack: pack, URL: "https://example.test/" + id,
		SHA256: "a", InstalledAt: time.Unix(1, 0).UTC(), Sources: sources,
	}
}

func source(id, format string) PackageSource {
	return PackageSource{ID: id, Name: id, Kind: "bt", Version: "1", Format: format}
}

func TestStorePutReplaceRemoveAndUndo(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "installed-sources"))
	if packages, err := store.List(); err != nil || len(packages) != 0 {
		t.Fatalf("empty List = %v, %v", packages, err)
	}

	first := testPackage("demo", false, source("demo", "yaml"))
	if _, err := store.Put(first, map[string][]byte{"demo": []byte("id: demo\n")}); err != nil {
		t.Fatal(err)
	}
	yamlPath := filepath.Join(store.SourcesDir(), "demo.yaml")
	assertFile(t, yamlPath, "id: demo\n")
	if info, err := os.Stat(yamlPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("source file mode = %v, %v", info, err)
	}

	second := testPackage("demo", false, source("demo", "json"))
	second.Version = "2"
	undo, err := store.Put(second, map[string][]byte{"demo": []byte(`{"id":"demo"}`)})
	if err != nil {
		t.Fatal(err)
	}
	jsonPath := filepath.Join(store.SourcesDir(), "demo.json")
	assertFile(t, jsonPath, `{"id":"demo"}`)
	assertMissing(t, yamlPath)
	if pkg, ok, _ := store.Get("demo"); !ok || pkg.Version != "2" || pkg.Sources[0].Format != "json" {
		t.Fatalf("Get after replace = %+v, %v", pkg, ok)
	}

	if err := undo(); err != nil {
		t.Fatal(err)
	}
	assertFile(t, yamlPath, "id: demo\n")
	assertMissing(t, jsonPath)
	if pkg, ok, _ := store.Get("demo"); !ok || pkg.Version != "1" {
		t.Fatalf("Get after undo = %+v, %v", pkg, ok)
	}

	undoRemove, err := store.Remove("demo")
	if err != nil {
		t.Fatal(err)
	}
	if packages, _ := store.List(); len(packages) != 0 {
		t.Fatalf("List after remove = %v", packages)
	}
	assertMissing(t, yamlPath)
	if err := undoRemove(); err != nil {
		t.Fatal(err)
	}
	assertFile(t, yamlPath, "id: demo\n")
	if _, err := store.Remove("missing"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Remove(missing) = %v", err)
	}
}

func TestStorePackReplaceDropsRemovedMembersAndUndoRestoresThem(t *testing.T) {
	store := NewStore(t.TempDir())
	v1 := testPackage("nagare-pack", true, source("alpha", "json"), source("beta", "json"))
	if _, err := store.Put(v1, map[string][]byte{"alpha": []byte("a1"), "beta": []byte("b1")}); err != nil {
		t.Fatal(err)
	}
	v2 := testPackage("nagare-pack", true, source("alpha", "json"), source("gamma", "json"))
	v2.Version = "2"
	undo, err := store.Put(v2, map[string][]byte{"alpha": []byte("a2"), "gamma": []byte("g2")})
	if err != nil {
		t.Fatal(err)
	}
	dir := store.SourcesDir()
	assertFile(t, filepath.Join(dir, "alpha.json"), "a2")
	assertFile(t, filepath.Join(dir, "gamma.json"), "g2")
	assertMissing(t, filepath.Join(dir, "beta.json"))
	if packages, _ := store.List(); len(packages) != 1 || len(packages[0].Sources) != 2 || packages[0].Sources[1].ID != "gamma" {
		t.Fatalf("List after pack update = %+v", packages)
	}

	if err := undo(); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(dir, "alpha.json"), "a1")
	assertFile(t, filepath.Join(dir, "beta.json"), "b1")
	assertMissing(t, filepath.Join(dir, "gamma.json"))
	if pkg, ok, _ := store.Get("nagare-pack"); !ok || pkg.Version != "1" {
		t.Fatalf("Get after undo = %+v, %v", pkg, ok)
	}

	undoRemove, err := store.Remove("nagare-pack")
	if err != nil {
		t.Fatal(err)
	}
	assertMissing(t, filepath.Join(dir, "alpha.json"))
	assertMissing(t, filepath.Join(dir, "beta.json"))
	if err := undoRemove(); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(dir, "beta.json"), "b1")
}

func TestStoreRejectsSourcesOwnedByAnotherPackage(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.Put(testPackage("alpha", false, source("alpha", "yaml")), map[string][]byte{"alpha": []byte("single")}); err != nil {
		t.Fatal(err)
	}
	pack := testPackage("nagare-pack", true, source("alpha", "json"), source("beta", "json"))
	conflicts, err := store.Conflicts(pack)
	if err != nil || len(conflicts) != 1 || conflicts[0].SourceID != "alpha" || conflicts[0].PackageID != "alpha" {
		t.Fatalf("Conflicts = %+v, %v", conflicts, err)
	}
	_, err = store.Put(pack, map[string][]byte{"alpha": []byte("a"), "beta": []byte("b")})
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.PackageName != "ALPHA" || !strings.Contains(err.Error(), "请先卸载") {
		t.Fatalf("Put conflicting pack = %v", err)
	}
	assertFile(t, filepath.Join(store.SourcesDir(), "alpha.yaml"), "single")
	assertMissing(t, filepath.Join(store.SourcesDir(), "beta.json"))

	// 同一个 id 一边是来源包、一边是单个来源，也不能互相覆盖。
	if found, _ := store.Conflicts(testPackage("alpha", true, source("other", "json"))); len(found) != 1 {
		t.Fatalf("pack/single id clash not reported: %+v", found)
	}
	if found, _ := store.Conflicts(testPackage("alpha", false, source("alpha", "json"))); len(found) != 0 {
		t.Fatalf("updating the same package reported conflicts: %+v", found)
	}
}

func TestStoreUndoOfFirstInstallRemovesEverything(t *testing.T) {
	store := NewStore(t.TempDir())
	undo, err := store.Put(testPackage("fresh", true, source("one", "json"), source("two", "yaml")),
		map[string][]byte{"one": []byte("1"), "two": []byte("2")})
	if err != nil {
		t.Fatal(err)
	}
	if err := undo(); err != nil {
		t.Fatal(err)
	}
	assertMissing(t, filepath.Join(store.SourcesDir(), "one.json"))
	assertMissing(t, filepath.Join(store.SourcesDir(), "two.yaml"))
	assertMissing(t, filepath.Join(store.dir, metadataFile))
	if packages, _ := store.List(); len(packages) != 0 {
		t.Fatalf("package survived undo: %v", packages)
	}
}

func TestStoreRejectsUnsafeInput(t *testing.T) {
	store := NewStore(t.TempDir())
	for _, id := range []string{"", "a", "../x", "Demo", "a/b", strings.Repeat("a", 64)} {
		if _, err := store.Put(testPackage(id, false, source("demo", "yaml")), map[string][]byte{"demo": []byte("x")}); err == nil {
			t.Errorf("Put(package %q) accepted", id)
		}
		if _, err := store.Put(testPackage("demo", true, source(id, "yaml")), map[string][]byte{id: []byte("x")}); err == nil {
			t.Errorf("Put(source %q) accepted", id)
		}
	}
	bad := []struct {
		name  string
		pkg   Package
		files map[string][]byte
	}{
		{"unknown format", testPackage("demo", false, source("demo", "toml")), map[string][]byte{"demo": []byte("x")}},
		{"no sources", testPackage("demo", true), map[string][]byte{}},
		{"missing file", testPackage("demo", true, source("one", "json")), map[string][]byte{"two": []byte("x")}},
		{"duplicate source", testPackage("demo", true, source("one", "json"), source("one", "json")),
			map[string][]byte{"one": []byte("x"), "two": []byte("y")}},
	}
	for _, tc := range bad {
		if _, err := store.Put(tc.pkg, tc.files); err == nil {
			t.Errorf("Put(%s) accepted", tc.name)
		}
	}
	if err := os.WriteFile(filepath.Join(store.dir, metadataFile), []byte(`{"schema":"other"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(); err == nil {
		t.Error("List accepted unknown schema")
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s still exists: %v", path, err)
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("%s = %q, %v; want %q", path, got, err, want)
	}
}
