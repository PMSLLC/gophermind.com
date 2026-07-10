package tools

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><head><style>x{}</style></head><body><h1>Title</h1><p>Hello  world</p></body></html>"))
	}))
	defer srv.Close()

	tool := FetchURL(nil)

	// happy path: HTML is stripped to readable text
	out, err := run(t, tool, `{"url":"`+srv.URL+`"}`)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !strings.Contains(out, "Title") || !strings.Contains(out, "Hello world") {
		t.Errorf("stripped text = %q", out)
	}
	if strings.Contains(out, "<h1>") || strings.Contains(out, "x{}") {
		t.Errorf("tags/style not stripped: %q", out)
	}
}

func TestFetchURLSchemeGuard(t *testing.T) {
	tool := FetchURL(nil)
	for _, bad := range []string{`{"url":"file:///etc/passwd"}`, `{"url":"ftp://x/y"}`, `{"url":"not a url"}`} {
		if _, err := run(t, tool, bad); err == nil {
			t.Errorf("expected error for %s", bad)
		}
	}
}

func TestFetchURLAllowlist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	// allowlist that does not include the test host -> blocked
	tool := FetchURL([]string{"example.com"})
	if _, err := run(t, tool, `{"url":"`+srv.URL+`"}`); err == nil {
		t.Error("expected host to be blocked by allowlist")
	}
}

func TestFetchURLMaxBytes(t *testing.T) {
	body := strings.Repeat("A", 5000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(body))
	}))
	defer srv.Close()

	tool := FetchURL(nil)
	out, err := run(t, tool, `{"url":"`+srv.URL+`","max_bytes":100}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > 400 { // 100 body bytes + a truncation note
		t.Errorf("max_bytes not honored, got %d bytes", len(out))
	}
}
