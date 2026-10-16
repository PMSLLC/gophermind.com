package serve

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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

// TestPipelineDashboardUsesTheNodeContract pins that the page talks to the new
// routes and no longer speaks the old task vocabulary.
func TestPipelineDashboardUsesTheNodeContract(t *testing.T) {
	t.Setenv("GOPHERMIND_SERVE_TOKEN", "t")
	mux, err := NewMux(Deps{Pipeline: &PipelineDeps{Root: t.TempDir()}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/pipeline")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get(PipelineSchemaHeader) != "1" {
		t.Errorf("the page response needs %s: 1", PipelineSchemaHeader)
	}
	raw, _ := io.ReadAll(resp.Body)
	body := string(raw)
	for _, want := range []string{
		"/pipeline/runs", "/pipeline/state", "/pipeline/node", "/pipeline/artifact", "/pipeline/decisions",
		"/pipeline/report", "/pipeline/events", "since", "X-Gophermind-Pipeline-Schema", "node-status", "run-status",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not mention %q", want)
		}
	}
	for _, gone := range []string{"task-status", "task-attempt", "acceptance_criteria", "revision_rounds", "contract_flagged"} {
		if strings.Contains(body, gone) {
			t.Errorf("the page still uses the old vocabulary %q", gone)
		}
	}
}

// TestPipelineDashboardResyncsOnEveryConnect pins that connectEvents refetches
// state (and the report when the run is over) after each successful connect:
// derived events are never replayed, so a run that ended while the page was
// away would otherwise stay "building".
func TestPipelineDashboardResyncsOnEveryConnect(t *testing.T) {
	page, err := os.ReadFile(filepath.Join("assets", "pipeline.html"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(page)
	i := strings.Index(s, "async function connectEvents()")
	if i < 0 {
		t.Fatal("no connectEvents")
	}
	j := strings.Index(s[i:], "\nasync function connect()")
	if j < 0 {
		t.Fatal("cannot find the end of connectEvents")
	}
	if !strings.Contains(s[i:i+j], "await resync()") {
		t.Error("connectEvents does not call resync() after a successful connect")
	}
	k := strings.Index(s, "async function resync()")
	if k < 0 {
		t.Fatal("no resync")
	}
	body := s[k : k+600]
	if !strings.Contains(body, "loadState()") || !strings.Contains(body, "'/pipeline/report'") {
		t.Error("resync does not refetch state and the report")
	}
}

// TestPipelineDashboardScriptParses runs `node --check` on the page's script
// when node is installed, so a syntax slip is a red test and not a blank page.
func TestPipelineDashboardScriptParses(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	page, err := os.ReadFile(filepath.Join("assets", "pipeline.html"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(page)
	i, j := strings.Index(s, "<script>"), strings.LastIndex(s, "</script>")
	if i < 0 || j < i {
		t.Fatal("no script block")
	}
	file := filepath.Join(t.TempDir(), "pipeline.js")
	if err := os.WriteFile(file, []byte(s[i+len("<script>"):j]), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, "--check", file).CombinedOutput(); err != nil {
		t.Fatalf("node --check failed: %v\n%s", err, out)
	}
}
