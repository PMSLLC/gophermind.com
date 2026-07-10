package tools

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPRequestPostEchoes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Method", r.Method)
		w.Header().Set("X-Custom", r.Header.Get("X-Custom"))
		w.WriteHeader(201)
		w.Write([]byte("echo:" + string(body)))
	}))
	defer srv.Close()

	tool := httpTool(nil, true, nil) // allow loopback for httptest
	out, err := run(t, tool, `{"method":"POST","url":"`+srv.URL+`","body":"hello","headers":{"X-Custom":"abc"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "201") || !strings.Contains(out, "echo:hello") {
		t.Errorf("response missing status/body: %q", out)
	}
}

func TestHTTPRequestDefaultsToGET(t *testing.T) {
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	if _, err := run(t, httpTool(nil, true, nil), `{"url":"`+srv.URL+`"}`); err != nil {
		t.Fatal(err)
	}
	if gotMethod != "GET" {
		t.Errorf("default method = %q, want GET", gotMethod)
	}
}

func TestHTTPRequestBlocksPrivate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	// production constructor blocks loopback (SSRF guard, shared with fetch_url)
	if _, err := run(t, HTTPRequest(nil, nil), `{"url":"`+srv.URL+`"}`); err == nil {
		t.Error("loopback should be blocked by the SSRF guard")
	}
}

func TestHTTPRequestRejectsBadMethod(t *testing.T) {
	if _, err := run(t, httpTool(nil, true, nil), `{"url":"http://x.local","method":"FETCH"}`); err == nil {
		t.Error("invalid method should be rejected")
	}
}

func TestHTTPRequestSchema(t *testing.T) {
	// The tool advertises method/url/headers/body.
	tool := HTTPRequest(nil, nil)
	b, _ := json.Marshal(tool.Schema)
	for _, k := range []string{"method", "url", "headers", "body"} {
		if !strings.Contains(string(b), k) {
			t.Errorf("schema missing %q", k)
		}
	}
}
