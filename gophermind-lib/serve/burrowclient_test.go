package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const fakeBurrowToken = "fake-token-0123456789abcdef"

func burrowTokenFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(p, []byte(fakeBurrowToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func burrowKind(t *testing.T, err error) string {
	t.Helper()
	be, ok := err.(*burrowError)
	if !ok {
		t.Fatalf("error %T %v is not *burrowError", err, err)
	}
	return be.Kind
}

var testContract = map[string]any{"verb": "Count", "object": "Text.Words"}

func TestBurrowLookupHit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/lookup" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+fakeBurrowToken {
			t.Errorf("auth header = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content type = %q", got)
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if _, ok := body["contract"]; !ok {
			t.Error("no contract key")
		}
		if _, ok := body["lang"]; !ok {
			t.Error("no lang key")
		}
		if _, ok := body["vectors"]; ok {
			t.Error("vectors key must be absent")
		}
		w.Write([]byte(`{"gate":"legacy","outcome":"exact","key":"text/words/count@d10","contract":{"verb":"Count"},"rendering":{"lang":"go","version":1,"body":"package main","verified_at":"2026-10-25T18:30:00Z","extra":1},"eligible":true,"awaiting_confirmation":false,"unknown_field":[1]}`))
	}))
	defer srv.Close()
	c := newBurrowClient(srv.URL, burrowTokenFile(t))
	got, err := c.lookup(context.Background(), testContract, "go")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != "exact" || got.Key != "text/words/count@d10" || !got.Eligible || got.AwaitingConfirmation {
		t.Errorf("got %+v", got)
	}
	if got.Rendering == nil || got.Rendering.Body != "package main" || got.Rendering.Version != 1 || got.Rendering.Lang != "go" || got.Rendering.VerifiedAt == "" {
		t.Errorf("rendering %+v", got.Rendering)
	}
	if !strings.Contains(string(got.Contract), "Count") {
		t.Errorf("contract %s", got.Contract)
	}
}

func TestBurrowLookupMiss(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"gate":"legacy","outcome":"none","key":null,"contract":null,"rendering":null,"considered":[],"eligible":true,"awaiting_confirmation":false}`))
	}))
	defer srv.Close()
	c := newBurrowClient(srv.URL, burrowTokenFile(t))
	got, err := c.lookup(context.Background(), testContract, "go")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != "none" || got.Rendering != nil || got.Key != "" {
		t.Errorf("got %+v", got)
	}
}

func TestBurrowLookupErrors(t *testing.T) {
	status := func(code int, body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			w.Write([]byte(body))
		}))
	}
	tf := burrowTokenFile(t)
	ctx := context.Background()

	s := status(401, `{"error":{"code":"unauthorized","message":"bad token"}}`)
	_, err := newBurrowClient(s.URL, tf).lookup(ctx, testContract, "go")
	s.Close()
	if k := burrowKind(t, err); k != "unauthorized" {
		t.Errorf("401 kind = %s", k)
	}

	s = status(400, `{"error":{"code":"invalid_request","message":"contract: verb must not be empty"}}`)
	_, err = newBurrowClient(s.URL, tf).lookup(ctx, testContract, "go")
	s.Close()
	if k := burrowKind(t, err); k != "bad_request" || !strings.Contains(err.Error(), "contract: verb must not be empty") {
		t.Errorf("400 kind = %s err = %v", k, err)
	}

	s = status(500, `{"error":{"code":"internal","message":"internal error"}}`)
	_, err = newBurrowClient(s.URL, tf).lookup(ctx, testContract, "go")
	s.Close()
	if k := burrowKind(t, err); k != "server" {
		t.Errorf("500 kind = %s", k)
	}

	s = status(502, `not json`)
	_, err = newBurrowClient(s.URL, tf).lookup(ctx, testContract, "go")
	s.Close()
	if k := burrowKind(t, err); k != "server" {
		t.Errorf("502 kind = %s", k)
	}

	closed := httptest.NewServer(http.NotFoundHandler())
	url := closed.URL
	closed.Close()
	_, err = newBurrowClient(url, tf).lookup(ctx, testContract, "go")
	if k := burrowKind(t, err); k != "unreachable" {
		t.Errorf("closed kind = %s", k)
	}

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	c := newBurrowClient(slow.URL, tf)
	c.hc.Timeout = 50 * time.Millisecond
	_, err = c.lookup(ctx, testContract, "go")
	if k := burrowKind(t, err); k != "timeout" {
		t.Errorf("slow kind = %s err = %v", k, err)
	}
}

func TestBurrowTokenNeverInError(t *testing.T) {
	tf := burrowTokenFile(t)
	cases := []*httptest.Server{
		httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":{"code":"unauthorized","message":"you sent ` + r.Header.Get("Authorization") + `"}}`))
		})),
		httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(500)
			w.Write([]byte(r.Header.Get("Authorization")))
		})),
		httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(400)
			w.Write([]byte(`{"error":{"code":"invalid_request","message":"echo ` + fakeBurrowToken + `"}}`))
		})),
	}
	for i, s := range cases {
		_, err := newBurrowClient(s.URL, tf).lookup(context.Background(), testContract, "go")
		s.Close()
		if err == nil || strings.Contains(err.Error(), fakeBurrowToken) {
			t.Errorf("case %d: err = %v", i, err)
		}
	}
	closed := httptest.NewServer(http.NotFoundHandler())
	url := closed.URL
	closed.Close()
	_, err := newBurrowClient(url, tf).lookup(context.Background(), testContract, "go")
	if err == nil || strings.Contains(err.Error(), fakeBurrowToken) {
		t.Errorf("unreachable err = %v", err)
	}
}

func TestBurrowNoRedirect(t *testing.T) {
	var hits atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/v1/lookup", http.StatusFound)
	}))
	defer srv.Close()
	_, err := newBurrowClient(srv.URL, burrowTokenFile(t)).lookup(context.Background(), testContract, "go")
	if k := burrowKind(t, err); k != "other" {
		t.Errorf("kind = %s err = %v", k, err)
	}
	if hits.Load() != 0 {
		t.Error("redirect target was contacted")
	}
}

func TestBurrowBodyCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"outcome":"exact","key":"`))
		w.Write([]byte(strings.Repeat("a", 2<<20)))
		w.Write([]byte(`"}`))
	}))
	defer srv.Close()
	_, err := newBurrowClient(srv.URL, burrowTokenFile(t)).lookup(context.Background(), testContract, "go")
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Errorf("err = %v", err)
	}
}

func TestBurrowTokenFileMissing(t *testing.T) {
	var hit atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit.Add(1) }))
	defer srv.Close()
	missing := filepath.Join(t.TempDir(), "nope", "token")
	_, err := newBurrowClient(srv.URL, missing).lookup(context.Background(), testContract, "go")
	if k := burrowKind(t, err); k != "other" {
		t.Errorf("kind = %s", k)
	}
	if hit.Load() != 0 {
		t.Error("request sent without a token")
	}
	empty := filepath.Join(t.TempDir(), "token")
	os.WriteFile(empty, []byte("  \n"), 0o600)
	if _, err := newBurrowClient(srv.URL, empty).lookup(context.Background(), testContract, "go"); err == nil {
		t.Error("empty token accepted")
	}
}
