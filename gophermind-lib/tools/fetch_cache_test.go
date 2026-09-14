package tools

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if _, ok := cacheGet(dir, "http://x/y"); ok {
		t.Error("empty cache should miss")
	}
	cachePut(dir, "http://x/y", "cached body")
	got, ok := cacheGet(dir, "http://x/y")
	if !ok || got != "cached body" {
		t.Errorf("cache get = %q,%v", got, ok)
	}
	// A different URL is a distinct entry.
	if _, ok := cacheGet(dir, "http://x/z"); ok {
		t.Error("distinct URL should miss")
	}
}

func TestFetchServesFromCacheOffline(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("GOPHERMIND_FETCH_CACHE_DIR", cacheDir)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("live content"))
	}))
	url := srv.URL

	tool := fetchTool(nil, true, nil) // loopback allowed for httptest
	out, err := run(t, tool, `{"url":"`+url+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "live content") {
		t.Fatalf("first fetch missing content: %q", out)
	}

	// Take the server offline; a second fetch must be served from cache.
	srv.Close()
	out2, err := run(t, tool, `{"url":"`+url+`"}`)
	if err != nil {
		t.Fatalf("offline fetch should hit cache: %v", err)
	}
	if !strings.Contains(out2, "live content") {
		t.Errorf("offline fetch missing cached content: %q", out2)
	}
}
