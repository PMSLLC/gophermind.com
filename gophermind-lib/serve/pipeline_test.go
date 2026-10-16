package serve

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// TestPipelineDashboardServesWithoutToken checks that GET /pipeline serves
// the dashboard HTML with no Authorization header required - a plain
// browser navigation cannot attach one, so the page itself cannot be
// behind the same auth as the data routes. It also checks the served page
// has no hardcoded demo data (the mockup's `const tasks = [...]`), since
// the source spec's own test requires the dashboard to show real state,
// not a replayed mock.
//
// It also guards a real finding from review: the bearer token this page
// asks for can run shell commands and write files on the machine, so
// leaking it is leaking code execution, not just a session id. The page
// must never read it from location.search (the query string reaches
// server access logs, browser history, and - since a subresource fetch
// with no referrer policy sends the full URL as Referer - third-party
// servers too) and must carry a no-referrer meta tag so nothing on the
// page can leak its URL to a subresource origin.
func TestPipelineDashboardServesWithoutToken(t *testing.T) {
	t.Setenv("GOPHERMIND_SERVE_TOKEN", "t")
	mux, err := NewMux(Deps{Pipeline: &PipelineDeps{Root: t.TempDir()}}, Options{})
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/pipeline")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /pipeline with no token: got %d, want 200", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, "const tasks = [") {
		t.Error("dashboard still embeds the mockup's hardcoded demo tasks")
	}
	if !strings.Contains(body, "/pipeline/state") || !strings.Contains(body, "/pipeline/events") {
		t.Error("dashboard does not reference the real state/events routes")
	}
	if strings.Contains(body, "location.search") {
		t.Error("dashboard reads a token (or anything else) from the query string - " +
			"it reaches server logs, browser history and, with no referrer policy, " +
			"third-party subresource requests; the token must only ever come from the URL fragment")
	}
	if !strings.Contains(body, `<meta name="referrer" content="no-referrer">`) {
		t.Error("dashboard is missing <meta name=\"referrer\" content=\"no-referrer\">, " +
			"so a subresource fetch could leak the page URL (and any token in it) to a third party")
	}
}

// Case-transforming the output of esc() corrupts what the reader sees. A
// verdict containing "&" escapes to "&amp;" and then uppercases to "&AMP;",
// which the browser renders literally as "&AMP;" rather than "&".
//
// This is NOT a security hole, and an earlier review of this file claimed it
// was. The claim was that &LT; and &GT; are valid HTML5 named references, so
// uppercasing "&lt;script&gt;" yields "&LT;SCRIPT&GT;" and innerHTML parses
// a live tag back out of it. Checked in a real browser, it does not: a
// character reference is tokenized in the data state and emits a character,
// never a tag-open. Setting innerHTML to esc(payload).toUpperCase() creates
// zero elements, against one for the unescaped control. Only a literal "<"
// opens a tag, and esc() removes every one of those.
//
// The rule is still worth pinning, on its own honest terms: transform the
// raw value, then escape the result. Escaping last is the ordering that
// cannot go wrong, and it keeps the displayed text correct.
func TestPipelineDashboardNeverCaseTransformsEscapedOutput(t *testing.T) {
	t.Setenv("GOPHERMIND_SERVE_TOKEN", "t")
	mux, err := NewMux(Deps{Pipeline: &PipelineDeps{Root: t.TempDir()}}, Options{})
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/pipeline")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	body := string(raw)

	bad := regexp.MustCompile(`esc\([^()]*\)\s*\.\s*to(Upper|Lower)Case`)
	if m := bad.FindString(body); m != "" {
		t.Errorf("dashboard case-transforms escaped output (%q): this displays "+
			"an escaped ampersand as the literal text \"&AMP;\". Uppercase the raw "+
			"value first, then escape the result", m)
	}

	// Defence in depth, which is a separate concern from the check above and
	// the one that actually carries security weight here. This page holds a
	// token that runs shell commands. The page loads nothing external, so the
	// policy can be strict enough that an injected script would have no origin
	// to load from and no endpoint to reach.
	if !strings.Contains(body, "Content-Security-Policy") {
		t.Error("dashboard has no Content-Security-Policy meta tag; it holds a " +
			"token that runs shell commands and should not depend on escaping alone")
	}
}

// TestPipelineDashboardSkippedWhenPipelineNil checks that GET /pipeline is
// simply not registered when Deps.Pipeline is nil, matching every other
// optional route's nil-disables convention.
func TestPipelineDashboardSkippedWhenPipelineNil(t *testing.T) {
	t.Setenv("GOPHERMIND_SERVE_TOKEN", "t")
	mux, err := NewMux(Deps{}, Options{})
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/pipeline")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /pipeline with Pipeline unset: got %d, want 404", resp.StatusCode)
	}
}
