package tools

import (
	"net"
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

	tool := fetchTool(nil, true, nil) // allow loopback so httptest is reachable

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
	tool := FetchURL(nil, nil)
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
	tool := FetchURL([]string{"example.com"}, nil)
	if _, err := run(t, tool, `{"url":"`+srv.URL+`"}`); err == nil {
		t.Error("expected host to be blocked by allowlist")
	}
}

func TestFetchURLBlocksPrivateAndLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("secret"))
	}))
	defer srv.Close()

	// Production constructor: loopback must be refused (SSRF guard).
	tool := FetchURL(nil, nil)
	_, err := run(t, tool, `{"url":"`+srv.URL+`"}`)
	if err == nil {
		t.Fatal("expected loopback target to be blocked")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("error should mention a blocked address, got %v", err)
	}
}

func TestFetchURLRedirectReChecked(t *testing.T) {
	// A loopback server that redirects to a private-range IP. Even with loopback
	// allowed for the initial hop, the redirect target must be refused.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://10.0.0.1/internal", http.StatusFound)
	}))
	defer srv.Close()

	tool := fetchTool(nil, true, nil) // loopback allowed, private still blocked
	_, err := run(t, tool, `{"url":"`+srv.URL+`"}`)
	if err == nil {
		t.Fatal("expected redirect to a private IP to be blocked")
	}
}

func TestDisallowedIP(t *testing.T) {
	cases := []struct {
		ip            string
		allowLoopback bool
		want          bool
	}{
		{"127.0.0.1", false, true},
		{"127.0.0.1", true, false},
		{"::1", false, true},
		{"10.0.0.1", true, true},
		{"172.16.0.1", true, true},
		{"192.168.1.1", true, true},
		{"169.254.169.254", true, true}, // cloud metadata
		{"0.0.0.0", true, true},
		{"8.8.8.8", false, false},
		{"1.1.1.1", true, false},
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("bad test ip %q", c.ip)
		}
		if got := disallowedIP(ip, c.allowLoopback); got != c.want {
			t.Errorf("disallowedIP(%s, allowLoopback=%v) = %v, want %v", c.ip, c.allowLoopback, got, c.want)
		}
	}
}

func TestFetchURLMaxBytes(t *testing.T) {
	body := strings.Repeat("A", 5000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(body))
	}))
	defer srv.Close()

	tool := fetchTool(nil, true, nil) // allow loopback so httptest is reachable
	out, err := run(t, tool, `{"url":"`+srv.URL+`","max_bytes":100}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > 400 { // 100 body bytes + a truncation note
		t.Errorf("max_bytes not honored, got %d bytes", len(out))
	}
}
