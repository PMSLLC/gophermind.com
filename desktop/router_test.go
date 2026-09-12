package main

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// backendStub stands in for a gophermind server: it records the Authorization
// header it was presented and can stream an SSE response slowly, which is how
// the proxy's flushing behaviour is exercised.
type backendStub struct {
	gotAuth   chan string
	gotPath   chan string
	sseChunks []string
	chunkGap  time.Duration
	// streamType is the Content-Type used for the streaming response.
	// net/http/httputil special-cases "text/event-stream" and streams it
	// whatever FlushInterval says, so a second content type is needed to
	// exercise FlushInterval itself.
	streamType string
}

func newBackendStub() *backendStub {
	return &backendStub{
		gotAuth: make(chan string, 8),
		gotPath: make(chan string, 8),
	}
}

func (b *backendStub) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case b.gotAuth <- r.Header.Get("Authorization"):
		default:
		}
		select {
		case b.gotPath <- r.URL.Path:
		default:
		}
		if len(b.sseChunks) > 0 && strings.HasSuffix(r.URL.Path, "/stream") {
			ct := b.streamType
			if ct == "" {
				ct = "text/event-stream"
			}
			w.Header().Set("Content-Type", ct)
			w.WriteHeader(http.StatusOK)
			fl, _ := w.(http.Flusher)
			for _, c := range b.sseChunks {
				_, _ = io.WriteString(w, c)
				if fl != nil {
					fl.Flush()
				}
				time.Sleep(b.chunkGap)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"path":"`+r.URL.Path+`"}`)
	}
}

// routerFor builds a router over one stub backend named "local", plus the
// router's own front-door token.
func routerFor(t *testing.T, stub *backendStub) (*httptest.Server, string) {
	t.Helper()
	up := httptest.NewServer(stub.handler())
	t.Cleanup(up.Close)

	frontToken, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	reg := &backendRegistry{}
	if err := reg.Add(Backend{Name: "local", Kind: BackendLocal, BaseURL: up.URL, Token: "upstream-secret", Available: true}); err != nil {
		t.Fatal(err)
	}

	r, err := newRouter(reg, frontToken)
	if err != nil {
		t.Fatal(err)
	}
	front := httptest.NewServer(r)
	t.Cleanup(front.Close)
	return front, frontToken
}

func do(t *testing.T, method, url, token, body string) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// The frontend presents the ROUTER's token. The backend's own token must
// never reach the frontend, and the router must substitute it upstream.
// Forwarding the frontend's token verbatim would mean every backend had to
// share one secret, and leaking a backend token to the WebView would hand a
// page-level XSS the keys to a remote machine rather than a local one.
func TestRouterSwapsTheTokenPerBackend(t *testing.T) {
	stub := newBackendStub()
	front, frontToken := routerFor(t, stub)

	resp := do(t, http.MethodGet, front.URL+"/b/local/session", frontToken, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d, want 200", resp.StatusCode)
	}

	gotAuth := <-stub.gotAuth
	if gotAuth != "Bearer upstream-secret" {
		t.Errorf("upstream saw %q, want the backend's own token", gotAuth)
	}
	if strings.Contains(gotAuth, frontToken) {
		t.Error("the router forwarded the frontend's token upstream")
	}
}

// The backend prefix is stripped before the request reaches the backend: the
// server knows nothing about routing and must see its own ordinary paths.
func TestRouterStripsTheBackendPrefix(t *testing.T) {
	stub := newBackendStub()
	front, frontToken := routerFor(t, stub)

	resp := do(t, http.MethodGet, front.URL+"/b/local/session/abc/messages", frontToken, "")
	defer resp.Body.Close()

	if got := <-stub.gotPath; got != "/session/abc/messages" {
		t.Errorf("backend saw path %q, want /session/abc/messages", got)
	}
}

// Phase 1 must not require a frontend change: an unprefixed path keeps
// working and goes to the default backend, exactly as it did when the
// frontend talked to the embedded server directly.
func TestRouterSendsUnprefixedPathsToTheDefaultBackend(t *testing.T) {
	stub := newBackendStub()
	front, frontToken := routerFor(t, stub)

	resp := do(t, http.MethodGet, front.URL+"/session", frontToken, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d, want 200", resp.StatusCode)
	}
	if got := <-stub.gotPath; got != "/session" {
		t.Errorf("backend saw path %q, want /session", got)
	}
}

// The router is the front door and is itself token-gated. Without this the
// tunnel would end at an unauthenticated local port that any process on the
// machine could drive.
func TestRouterRequiresItsOwnToken(t *testing.T) {
	stub := newBackendStub()
	front, _ := routerFor(t, stub)

	for _, tc := range []struct{ name, token string }{
		{"no token", ""},
		{"wrong token", "not-the-token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := do(t, http.MethodGet, front.URL+"/b/local/session", tc.token, "")
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("got %d, want 401", resp.StatusCode)
			}
		})
	}
}

// An unknown backend is a 404 from the router, never a request sent somewhere
// unintended.
func TestRouterRejectsAnUnknownBackend(t *testing.T) {
	stub := newBackendStub()
	front, frontToken := routerFor(t, stub)

	resp := do(t, http.MethodGet, front.URL+"/b/nope/session", frontToken, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("got %d, want 404", resp.StatusCode)
	}
	select {
	case p := <-stub.gotPath:
		t.Errorf("request reached a backend anyway, at %q", p)
	default:
	}
}

// GET /backends is how the frontend learns what it can talk to. It must never
// include a backend's token: the whole point of the swap above is that the
// WebView never holds credentials for a remote machine.
func TestRouterListsBackendsWithoutLeakingTokens(t *testing.T) {
	stub := newBackendStub()
	front, frontToken := routerFor(t, stub)

	resp := do(t, http.MethodGet, front.URL+"/backends", frontToken, "")
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(raw), "upstream-secret") {
		t.Fatalf("GET /backends leaked a backend token: %s", raw)
	}

	var got []struct {
		Name    string `json:"name"`
		Kind    string `json:"kind"`
		Default bool   `json:"default"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode: %v (body %s)", err, raw)
	}
	if len(got) != 1 || got[0].Name != "local" || !got[0].Default {
		t.Fatalf("got %+v, want one default backend named local", got)
	}
}

// An agent turn arrives as SSE, one frame at a time over many seconds, and a
// proxy that buffers turns a live transcript into a long silence followed by
// everything at once. An approval prompt that arrives after the user gave up
// waiting is worse than no approval prompt.
//
// What this test proves is that frames cross the router as they are produced.
// What it does NOT prove is that the router's FlushInterval is what makes
// that true: measured both ways, net/http/httputil streams this case with
// FlushInterval at 0 as well, so the test cannot catch that setting being
// changed. It is an end-to-end guard, not a guard on the configuration.
func TestRouterStreamsSSEWithoutBuffering(t *testing.T) {
	stub := newBackendStub()
	stub.sseChunks = []string{
		"event: token\ndata: one\n\n",
		"event: token\ndata: two\n\n",
		"event: done\ndata: {}\n\n",
	}
	stub.chunkGap = 120 * time.Millisecond
	front, frontToken := routerFor(t, stub)

	resp := do(t, http.MethodPost, front.URL+"/b/local/session/x/stream", frontToken, "go")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d, want 200", resp.StatusCode)
	}

	type arrival struct {
		line string
		at   time.Duration
	}
	start := time.Now()
	var arrivals []arrival
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data: ") {
			arrivals = append(arrivals, arrival{line, time.Since(start)})
		}
	}
	if len(arrivals) != 3 {
		t.Fatalf("got %d data frames, want 3: %+v", len(arrivals), arrivals)
	}
	// If the proxy buffered, every frame lands at once at the end. Requiring
	// the first to arrive well before the last is what distinguishes
	// streaming from buffering; asserting only that all three arrived would
	// pass against a fully buffering proxy.
	if gap := arrivals[2].at - arrivals[0].at; gap < 100*time.Millisecond {
		t.Errorf("frames arrived %v apart, so the router buffered the stream "+
			"instead of flushing each frame", gap)
	}
}

// The router exempts exactly the paths internal/serve exempts, and nothing
// else. A liveness probe that needs a credential is not a liveness probe, and
// the frontend polls /healthz during startup before it has an endpoint. But
// the exemption must not widen: anything that reads or writes a session has
// to present the token, and a prefix match rather than an exact one would
// make "/healthz/../session" reachable.
func TestRouterExemptsOnlyHealthPaths(t *testing.T) {
	stub := newBackendStub()
	front, _ := routerFor(t, stub)

	for _, open := range []string{"/healthz", "/readyz", "/metrics"} {
		resp := do(t, http.MethodGet, front.URL+open, "", "")
		resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized {
			t.Errorf("%s requires a token; a probe that needs a credential is not a probe", open)
		}
	}

	for _, closed := range []string{
		"/session",
		"/backends",
		"/b/local/session",
		"/healthz/../session",
		"/healthzz",
	} {
		resp := do(t, http.MethodGet, front.URL+closed, "", "")
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s returned %d without a token, want 401", closed, resp.StatusCode)
		}
	}
}
