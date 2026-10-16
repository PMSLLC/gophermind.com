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

// pipelineHarness loads the page's script into a node vm with a minimal stub
// DOM, fetch and AbortController, then runs one named scenario against it.
const pipelineHarness = `
const vm = require('vm');
const fs = require('fs');
const src = fs.readFileSync(process.argv[2], 'utf8');
const scenario = process.argv[3];
const flush = async () => { for (let i = 0; i < 30; i++) await new Promise(r => setImmediate(r)); };
function assert(c, m){ if(!c){ console.error('ASSERT FAILED: ' + m); process.exit(1); } }

function mkEl(tag){
  const e = { tag, textContent: '', className: '', children: [], parent: null, dataset: {}, style: {},
    value: '', hidden: false, scrollTop: 0, scrollHeight: 100, clientHeight: 100, listeners: {} };
  e.classList = { add(){}, remove(){} };
  e.appendChild = c => { c.parent = e; e.children.push(c); return c; };
  e.replaceChildren = (...cs) => { e.children.forEach(c => { c.parent = null; }); e.children = []; cs.forEach(e.appendChild); };
  e.remove = () => { if(e.parent){ e.parent.children = e.parent.children.filter(c => c !== e); e.parent = null; } };
  e.insertAdjacentElement = (where, c) => {
    c.remove();
    const i = e.parent.children.indexOf(e);
    e.parent.children.splice(i + 1, 0, c);
    c.parent = e.parent;
    return c;
  };
  e.addEventListener = (n, f) => { e.listeners[n] = f; };
  return e;
}

// deferred fetch: the test resolves each pending request by hand.
function makeEnv(){
  const els = {};
  const pending = [];   // { path, params, resolve, reject, signal }
  const eventCalls = [];
  const document = {
    getElementById: id => els[id] || (els[id] = mkEl('#' + id)),
    createElement: mkEl,
    createTextNode: t => ({ textContent: t }),
  };
  function resp(body, status){
    return { ok: (status || 200) < 400, status: status || 200,
      headers: { get: () => '1' }, json: async () => body, text: async () => String(body) };
  }
  const fetchStub = (url, opts) => new Promise((resolve, reject) => {
    const u = new URL(url, 'http://x');
    const call = { path: u.pathname, params: Object.fromEntries(u.searchParams), resolve: b => resolve(resp(b)), reject, signal: opts && opts.signal };
    if(u.pathname === '/pipeline/events'){
      eventCalls.push(call);
      const r = resp('');
      r.body = { getReader: () => ({ read: () => new Promise((_, rej) => {
        call.signal.addEventListener('abort', () => rej(new Error('aborted')));
      }) }) };
      resolve(r);
      return;
    }
    pending.push(call);
  });
  const ctx = vm.createContext({ document, fetch: fetchStub, URLSearchParams, AbortController, TextDecoder,
    setTimeout, console, Number, Date, Math, Array, Set, JSON, Promise,
    location: { hash: '', pathname: '/pipeline' }, history: { replaceState(){} } });
  vm.runInContext(src, ctx);
  const get = e => vm.runInContext(e, ctx);
  const take = path => { const i = pending.findIndex(p => p.path === path); assert(i >= 0, 'no pending ' + path); return pending.splice(i, 1)[0]; };
  return { ctx, get, els, pending, eventCalls, take };
}
const state = (seq, status, att) => ({ event_seq: seq, generated_at: '2026-01-01T00:00:00Z',
  run: { status: status || 'building', coverage: { covered: 0, total: 1 } },
  nodes: [{ id: 'n1', kind: 'function', status: 'pending', wave: 1, attempt_count: att || 0, revision: 0, parent: 'c' }] });
const nodeBody = { node: { id: 'n1', kind: 'function' }, runtime: { attempts: [] }, artifacts: [] };

const scenarios = {
  async skipsFramesAtOrBelowStateSeq(){
    const env = makeEnv();
    env.get('runId = "r1"');
    const p = env.get('loadState()');
    env.take('/pipeline/state').resolve(state(10));
    await flush();
    env.take('/pipeline/node').resolve(nodeBody);
    await p;
    assert(env.get('stateSeq') === 10, 'stateSeq 10');
    const att = JSON.stringify({ id: 'n1', attempt: { started_at: 'a' } });
    env.get('handleFrame')('node-attempt', '10', att);
    env.get('handleFrame')('node-attempt', '3', att);
    assert(env.get('nodes[0].attempt_count') === 0, 'old frames must be skipped');
    assert(env.get('lastSeq') === 10, 'old frames must not move lastSeq');
    env.get('handleFrame')('node-attempt', '11', att);
    assert(env.get('nodes[0].attempt_count') === 1, 'new frame applied once');
    assert(env.get('lastSeq') === 11, 'lastSeq 11');
    env.get('handleFrame')('node-attempt', 'abc', JSON.stringify({ id: 'n1', attempt: { started_at: 'b' } }));
    assert(env.get('lastSeq') === 11, 'non-numeric id leaves lastSeq alone');
    // a derived frame shares the latest line's id, so an id equal to stateSeq
    // is still applied: run-status refetches the state, run-report is kept.
    const before = env.pending.length;
    env.get('handleFrame')('run-status', '10', JSON.stringify({ status: 'verified' }));
    await flush();
    assert(env.pending.some(c => c.path === '/pipeline/state'), 'run-status at stateSeq must refetch the state');
    assert(env.pending.length > before, 'run-status issued requests');
    env.get('handleFrame')('run-report', '10', JSON.stringify({ status: 'verified', complete: true }));
    assert(env.get('finalReport && finalReport.status') === 'verified', 'run-report at stateSeq must be kept');
  },
  async staleLoadStateIsDiscarded(){
    const env = makeEnv();
    env.get('runId = "r1"');
    const p1 = env.get('loadState()');
    const p2 = env.get('loadState()');
    const first = env.take('/pipeline/state'), second = env.take('/pipeline/state');
    second.resolve(state(10, 'building', 7));
    await flush();
    first.resolve(state(5, 'building', 1));
    await flush();
    while(env.pending.length) env.pending.shift().resolve(nodeBody);
    await flush();
    assert((await p1) === null, 'stale response reports null');
    assert(env.get('stateSeq') === 10, 'stateSeq kept at 10, got ' + env.get('stateSeq'));
    assert(env.get('nodes[0].attempt_count') === 7, 'nodes not overwritten by the stale response');
    await p2;
  },
  async framesDuringStateLoadAreQueued(){
    const env = makeEnv();
    env.get('runId = "r1"');
    const p = env.get('loadState()');
    // frame 9 is already in the snapshot below (attempt_count 1); frame 11 is not.
    env.get('handleFrame')('node-attempt', '9', JSON.stringify({ id: 'n1', attempt: { started_at: 'a9' } }));
    env.get('handleFrame')('node-attempt', '11', JSON.stringify({ id: 'n1', attempt: { started_at: 'a11' } }));
    assert(env.get('queuedFrames.length') === 2, 'frames queued while the state request is in flight');
    assert(env.get('lastSeq') === 11, 'queued frames still advance lastSeq');
    env.take('/pipeline/state').resolve(state(10, 'building', 1));
    await flush();
    env.take('/pipeline/node').resolve(nodeBody);
    await p;
    assert(env.get('stateSeq') === 10, 'stateSeq from the snapshot');
    assert(env.get('nodes[0].attempt_count') === 2, 'frame 11 applied once, frame 9 not twice; got ' + env.get('nodes[0].attempt_count'));
    assert(env.get('nodes[0].last_attempt.started_at') === 'a11', 'frame 11 is the last attempt');
    assert(env.get('queuedFrames.length') === 0 && env.get('stateLoading') === false, 'queue cleared');
    env.get('handleFrame')('node-status', '12', JSON.stringify({ id: 'n1', status: 'verified', wave: 1, revision: 0 }));
    assert(env.get('nodes[0].status') === 'verified', 'frames after the load apply directly');
  },
  async staleStateKeepsQueueFailedStateFlushesIt(){
    const env = makeEnv();
    env.get('runId = "r1"');
    const p0 = env.get('loadState()');
    env.take('/pipeline/state').resolve(state(5));
    await flush();
    env.take('/pipeline/node').resolve(nodeBody);
    await p0;
    const p1 = env.get('loadState()');
    const p2 = env.get('loadState()');
    const first = env.take('/pipeline/state'), second = env.take('/pipeline/state');
    env.get('handleFrame')('node-attempt', '11', JSON.stringify({ id: 'n1', attempt: { started_at: 'a11' } }));
    first.resolve(state(20, 'building', 5));
    assert((await p1) === null, 'stale response reports null');
    assert(env.get('queuedFrames.length') === 1, 'a stale response leaves the queue to the newer request');
    assert(env.get('nodes[0].attempt_count') === 0, 'a stale response applies nothing');
    second.reject(new Error('down'));
    let failed = false;
    try{ await p2; } catch(e){ failed = true; }
    assert(failed, 'the newest request failed');
    assert(env.get('queuedFrames.length') === 0 && env.get('stateLoading') === false, 'queue flushed after failure');
    assert(env.get('stateSeq') === 5, 'stateSeq unchanged by the failure');
    assert(env.get('nodes[0].attempt_count') === 1, 'the queued frame was applied to the old state');
  },
  async runSwitchAbortsOldStream(){
    const env = makeEnv();
    env.els.tokenInput = mkEl('input'); env.els.tokenInput.value = 'tok';
    const c = env.get('connect()');
    env.take('/pipeline/runs').resolve({ runs: [{ run_id: 'r1', status: 'building' }, { run_id: 'r2', status: 'building' }] });
    await c; await flush();
    assert(env.eventCalls.length === 1, 'one stream open');
    const old = env.eventCalls[0];
    assert(old.params.run === 'r1', 'old stream is for r1');
    env.take('/pipeline/state').resolve(state(40));
    await flush();
    env.take('/pipeline/node').resolve(nodeBody);
    await flush();
    assert(env.get('lastSeq') === 40, 'lastSeq from state');
    env.get('switchRun')('r2');
    assert(old.signal.aborted, 'old stream aborted');
    assert(env.get('lastSeq') === 0 && env.get('stateSeq') === 0, 'counters reset');
    await flush();
    assert(env.eventCalls.length === 2 && env.eventCalls[1].params.run === 'r2' && env.eventCalls[1].params.since === '0', 'new stream for r2 from 0');
    // an old stream frame arriving late must not raise the new run's counters
    env.take('/pipeline/state').resolve(state(3));
    await flush();
    assert(env.get('lastSeq') === 3, 'new run lastSeq from its own state');
  },
};
let done = false;
process.on('exit', () => { if(!done){ console.error('scenario ' + scenario + ' never finished (a promise hung)'); process.exitCode = 1; } });
scenarios[scenario]().then(() => { done = true; console.log('ok ' + scenario); process.exit(0); },
  e => { console.error(e && e.stack || e); process.exit(1); });
`

// TestPipelineDashboardBehavior runs the page's script in node against a
// stubbed DOM and fetch: replayed frames are skipped, a stale state response
// loses to a newer one, and a run switch aborts the old stream.
func TestPipelineDashboardBehavior(t *testing.T) {
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
	dir := t.TempDir()
	script := filepath.Join(dir, "pipeline.js")
	harness := filepath.Join(dir, "harness.js")
	if err := os.WriteFile(script, []byte(s[i+len("<script>"):j]), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(harness, []byte(pipelineHarness), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"skipsFramesAtOrBelowStateSeq", "staleLoadStateIsDiscarded", "framesDuringStateLoadAreQueued", "staleStateKeepsQueueFailedStateFlushesIt", "runSwitchAbortsOldStream"} {
		t.Run(name, func(t *testing.T) {
			out, err := exec.Command(node, harness, script, name).CombinedOutput()
			if err != nil || !strings.Contains(string(out), "ok "+name) {
				t.Fatalf("%v\n%s", err, out)
			}
		})
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
