//go:build !windows

package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// briefEnv is a mux with the brief routes on, a fake launcher and a signed-in
// cookie.
type briefEnv struct {
	t      *testing.T
	h      http.Handler
	root   string
	dir    string
	cookie *http.Cookie
	mu     sync.Mutex
	calls  [][]string
	script func(args []string) string // shell snippet per invocation
	logs   lockedBuf                  // the manager's structured log lines
}

func newBriefEnv(t *testing.T) *briefEnv {
	t.Helper()
	e := &briefEnv{t: t, root: t.TempDir(), dir: t.TempDir()}
	e.script = func(args []string) string { return "echo ran" }
	a := newTestAuth(newFakeGoogle(t, goodClaims()))
	d := Deps{
		Run:      func(ctx context.Context, task string) (string, error) { return "", nil },
		Pipeline: &PipelineDeps{Root: e.root},
	}
	mux, err := NewMux(d, Options{
		Token: testBearer, Google: a, BriefsDir: e.dir,
		BriefLogger: slog.New(slog.NewTextHandler(&e.logs, nil)),
		BriefCommand: func(args []string) *exec.Cmd {
			e.mu.Lock()
			e.calls = append(e.calls, append([]string(nil), args...))
			e.mu.Unlock()
			return exec.Command("sh", "-c", e.script(args))
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	e.h = a.Middleware(mux)
	e.cookie = a.sessionCookie("john@brahy.com", time.Now())
	return e
}

func (e *briefEnv) briefText(id string) string {
	e.t.Helper()
	src, err := os.ReadFile("../build/testdata/example/brief.md")
	if err != nil {
		e.t.Fatal(err)
	}
	s := strings.Replace(string(src), "gm-2026-09-29-001", id, 1)
	return strings.Replace(s, "repo: ~/src/acme-api", "repo: "+e.root, 1)
}

func (e *briefEnv) post(path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	return doBody(e.h, "POST", path, body, hdr, e.cookie)
}

func (e *briefEnv) get(path string) *httptest.ResponseRecorder {
	return do(e.h, "GET", path, nil, e.cookie)
}

// waitNotActive polls until the run is no longer the active one: a stop of it
// answers 409. The run goroutine closes its log before it frees the slot, so
// this also means nothing writes into the run folder any more.
func (e *briefEnv) waitNotActive(id string) {
	e.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		w := e.get("/app/briefs/" + id + "/state")
		if w.Code == http.StatusOK && !strings.Contains(w.Body.String(), `"active":true`) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.t.Fatalf("run %s is still active", id)
}

// stopAndDrain stops the run and waits until its process group is reaped and
// its log is closed.
func (e *briefEnv) stopAndDrain(id string) {
	e.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		w := e.post("/app/briefs/"+id+"/stop", "", nil)
		if w.Code == http.StatusConflict {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.t.Fatalf("run %s did not stop", id)
}

func (e *briefEnv) waitPhase(id string, phases ...string) briefRecord {
	e.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(filepath.Join(e.dir, id, "run.json"))
		if err == nil {
			var rec briefRecord
			if json.Unmarshal(b, &rec) == nil {
				for _, p := range phases {
					if rec.Phase == p {
						return rec
					}
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.t.Fatalf("run %s never reached %v", id, phases)
	return briefRecord{}
}

func decodeMap(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("body %q: %v", w.Body.String(), err)
	}
	return m
}

func TestBriefCreateRunsPlanThenRun(t *testing.T) {
	e := newBriefEnv(t)
	w := e.post("/app/briefs", e.briefText("gm-2026-10-23-001"), nil)
	if w.Code != http.StatusAccepted {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}
	m := decodeMap(t, w)
	if m["run_id"] != "gm-2026-10-23-001" || m["url"] != "/app/briefs/gm-2026-10-23-001" {
		t.Errorf("body %v", m)
	}
	rec := e.waitPhase("gm-2026-10-23-001", phaseDone, phaseFailed)
	if rec.Phase != phaseDone || rec.ExitCode == nil || *rec.ExitCode != 0 {
		t.Fatalf("record %+v", rec)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.calls) != 2 {
		t.Fatalf("calls %v", e.calls)
	}
	p := e.calls[0]
	if len(p) != 4 || p[0] != "brief" || p[1] != "plan" || p[2] != filepath.Join(e.dir, "gm-2026-10-23-001", "brief.md") || p[3] != "--yes" {
		t.Errorf("plan call %v", p)
	}
	r := e.calls[1]
	if strings.Join(r, " ") != "brief run gm-2026-10-23-001 --repo "+e.root {
		t.Errorf("run call %v", r)
	}
	logb, err := os.ReadFile(filepath.Join(e.dir, "gm-2026-10-23-001", "run.log"))
	if err != nil || !strings.Contains(string(logb), "ran") || !strings.Contains(string(logb), "== gophermind brief plan") {
		t.Errorf("log %q %v", logb, err)
	}
	saved, _ := os.ReadFile(filepath.Join(e.dir, "gm-2026-10-23-001", "brief.md"))
	if string(saved) != e.briefText("gm-2026-10-23-001") {
		t.Error("saved brief differs from the upload")
	}
}

func TestBriefCreateMultipart(t *testing.T) {
	e := newBriefEnv(t)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("brief", "b.md")
	fw.Write([]byte(e.briefText("gm-2026-10-23-002")))
	mw.Close()
	w := e.post("/app/briefs", buf.String(), map[string]string{"Content-Type": mw.FormDataContentType()})
	if w.Code != http.StatusAccepted {
		t.Fatalf("multipart create = %d %s", w.Code, w.Body)
	}
	e.waitPhase("gm-2026-10-23-002", phaseDone)

	// A form without a brief part is a client error.
	buf.Reset()
	mw = multipart.NewWriter(&buf)
	mw.WriteField("other", "x")
	mw.Close()
	w = e.post("/app/briefs", buf.String(), map[string]string{"Content-Type": mw.FormDataContentType()})
	if w.Code != http.StatusBadRequest {
		t.Errorf("no brief part = %d", w.Code)
	}
}

func TestBriefCreateRefusals(t *testing.T) {
	e := newBriefEnv(t)
	good := e.briefText("gm-2026-10-23-003")
	cases := []struct {
		name, body string
		want       int
	}{
		{"empty", "  \n", http.StatusBadRequest},
		{"not a brief", "just words, no frontmatter", http.StatusBadRequest},
		{"bad id", strings.Replace(good, "gm-2026-10-23-003", "../etc", 1), http.StatusBadRequest},
		{"other repo", strings.Replace(good, "repo: "+e.root, "repo: /somewhere/else", 1), http.StatusBadRequest},
		{"tilde repo", strings.Replace(good, "repo: "+e.root, "repo: ~/src/x", 1), http.StatusBadRequest},
		{"too big", good + strings.Repeat("x", maxBriefBytes), http.StatusRequestEntityTooLarge},
	}
	for _, c := range cases {
		w := e.post("/app/briefs", c.body, nil)
		if w.Code != c.want {
			t.Errorf("%s = %d (%s), want %d", c.name, w.Code, w.Body, c.want)
		}
		if m := decodeMap(t, w); m["error"] == nil || m["error"] == "" {
			t.Errorf("%s: no error text", c.name)
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.calls) != 0 {
		t.Errorf("a refused brief launched %v", e.calls)
	}
	if ents, _ := os.ReadDir(e.dir); len(ents) != 0 {
		t.Errorf("a refused brief left %d entries", len(ents))
	}
}

func TestBriefValidatedLikeCLI(t *testing.T) {
	e := newBriefEnv(t)
	// The CLI's validator rejects this; so must the route, with its reason.
	bad := strings.Replace(e.briefText("gm-2026-10-23-004"), "## Features", "## Not Features", 1)
	w := e.post("/app/briefs", bad, nil)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid brief") {
		t.Errorf("= %d %s", w.Code, w.Body)
	}
}

func TestBriefConflictWhileActive(t *testing.T) {
	e := newBriefEnv(t)
	e.script = func(args []string) string { return "sleep 30" }
	if w := e.post("/app/briefs", e.briefText("gm-2026-10-23-010"), nil); w.Code != http.StatusAccepted {
		t.Fatalf("first = %d %s", w.Code, w.Body)
	}
	w := e.post("/app/briefs", e.briefText("gm-2026-10-23-011"), nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("second = %d %s", w.Code, w.Body)
	}
	if m := decodeMap(t, w); m["active_run_id"] != "gm-2026-10-23-010" {
		t.Errorf("conflict body %v", m)
	}
	if _, err := os.Stat(filepath.Join(e.dir, "gm-2026-10-23-011")); err == nil {
		t.Error("the refused run left a folder")
	}
	e.post("/app/briefs/gm-2026-10-23-010/stop", "", nil)
	e.waitPhase("gm-2026-10-23-010", phaseStopped)
}

func TestBriefDuplicateIDConflicts(t *testing.T) {
	e := newBriefEnv(t)
	body := e.briefText("gm-2026-10-23-012")
	if w := e.post("/app/briefs", body, nil); w.Code != http.StatusAccepted {
		t.Fatalf("first = %d", w.Code)
	}
	e.waitPhase("gm-2026-10-23-012", phaseDone)
	w := e.post("/app/briefs", body, nil)
	if w.Code != http.StatusConflict || decodeMap(t, w)["run_id"] != "gm-2026-10-23-012" {
		t.Errorf("duplicate = %d %s", w.Code, w.Body)
	}
}

func TestBriefStopKillsProcessGroup(t *testing.T) {
	e := newBriefEnv(t)
	pidFile := filepath.Join(t.TempDir(), "pid")
	// A grandchild in the same group must die with the run.
	e.script = func(args []string) string { return "sleep 30 & echo $! > " + pidFile + "; wait" }
	if w := e.post("/app/briefs", e.briefText("gm-2026-10-23-020"), nil); w.Code != http.StatusAccepted {
		t.Fatalf("create = %d", w.Code)
	}
	// Wait until the process is running.
	deadline := time.Now().Add(10 * time.Second)
	for {
		e.mu.Lock()
		n := len(e.calls)
		e.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	w := e.post("/app/briefs/gm-2026-10-23-020/stop", "", nil)
	if w.Code != http.StatusAccepted {
		t.Fatalf("stop = %d %s", w.Code, w.Body)
	}
	rec := e.waitPhase("gm-2026-10-23-020", phaseStopped)
	if rec.FinishedAt == "" {
		t.Errorf("record %+v", rec)
	}
	pidb, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(pidb)))
	gone := false
	for i := 0; i < 100 && !gone; i++ {
		gone = syscall.Kill(pid, 0) != nil
		time.Sleep(20 * time.Millisecond)
	}
	if !gone {
		t.Errorf("grandchild %d still alive after stop", pid)
	}
	e.mu.Lock()
	n := len(e.calls)
	e.mu.Unlock()
	if n != 1 {
		t.Errorf("a stopped plan went on to run: %d calls", n)
	}
	// The run is over, so the slot is free and a second stop is a conflict.
	// The record turns stopped before the run goroutine closes its log and
	// frees the slot, so wait for the conflict instead of expecting it at once.
	e.waitNotActive("gm-2026-10-23-020")
	if w := e.post("/app/briefs/gm-2026-10-23-020/stop", "", nil); w.Code != http.StatusConflict {
		t.Errorf("second stop = %d", w.Code)
	}
	if w := e.post("/app/briefs", e.briefText("gm-2026-10-23-021"), nil); w.Code != http.StatusAccepted {
		t.Fatalf("slot not freed: %d %s", w.Code, w.Body)
	}
	// The second run is a live process group writing run.log into the test's
	// temp dir: stop it and wait until it is reaped and its log closed, or the
	// directory cleanup races a writer ("directory not empty").
	e.stopAndDrain("gm-2026-10-23-021")
}

func TestBriefStopUnknownRun(t *testing.T) {
	e := newBriefEnv(t)
	if w := e.post("/app/briefs/gm-2026-10-23-099/stop", "", nil); w.Code != http.StatusConflict {
		t.Errorf("unknown = %d", w.Code)
	}
	if w := e.post("/app/briefs/not-an-id/stop", "", nil); w.Code != http.StatusNotFound {
		t.Errorf("bad id = %d", w.Code)
	}
}

func TestBriefFailedStepRecorded(t *testing.T) {
	e := newBriefEnv(t)
	e.script = func(args []string) string { return "echo boom; exit 3" }
	e.post("/app/briefs", e.briefText("gm-2026-10-23-030"), nil)
	rec := e.waitPhase("gm-2026-10-23-030", phaseFailed)
	if rec.ExitCode == nil || *rec.ExitCode != 3 || !strings.Contains(rec.Error, "plan") {
		t.Errorf("record %+v", rec)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.calls) != 1 {
		t.Errorf("run started after a failed plan: %v", e.calls)
	}
}

func TestBriefRoutesNeedSignIn(t *testing.T) {
	e := newBriefEnv(t)
	for _, tc := range []struct{ method, path string }{
		{"POST", "/app/briefs"},
		{"POST", "/app/briefs/gm-2026-10-23-001/stop"},
		{"GET", "/app/briefs/gm-2026-10-23-001/state"},
		{"GET", "/app/api/briefs"},
	} {
		w := do(e.h, tc.method, tc.path, nil)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a session = %d, want 401", tc.method, tc.path, w.Code)
		}
	}
	// A browser page request without a session is sent to sign in.
	w := do(e.h, "GET", "/app/briefs", map[string]string{"Accept": "text/html"})
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/auth/login" {
		t.Errorf("page = %d %s", w.Code, w.Header().Get("Location"))
	}
	// A wrong bearer is refused by the route itself.
	w = do(e.h, "POST", "/app/briefs", map[string]string{"Authorization": "Bearer nope"})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("wrong bearer = %d", w.Code)
	}
}

func TestBriefStateChangesRefuseCrossSite(t *testing.T) {
	e := newBriefEnv(t)
	body := e.briefText("gm-2026-10-23-040")
	for name, hdr := range map[string]map[string]string{
		"other origin":    {"Origin": "https://evil.example"},
		"cross-site":      {"Sec-Fetch-Site": "cross-site"},
		"same-site other": {"Sec-Fetch-Site": "same-site"},
	} {
		if w := e.post("/app/briefs", body, hdr); w.Code != http.StatusForbidden {
			t.Errorf("create, %s = %d, want 403", name, w.Code)
		}
		if w := e.post("/app/briefs/gm-2026-10-23-040/stop", "", hdr); w.Code != http.StatusForbidden {
			t.Errorf("stop, %s = %d, want 403", name, w.Code)
		}
	}
	if len(e.calls) != 0 {
		t.Error("a refused request launched something")
	}
	// The same-origin browser path works.
	w := e.post("/app/briefs", body, map[string]string{"Origin": "https://gophermind.example", "Sec-Fetch-Site": "same-origin"})
	if w.Code != http.StatusAccepted {
		t.Errorf("same origin = %d %s", w.Code, w.Body)
	}
	e.waitPhase("gm-2026-10-23-040", phaseDone)
}

func TestBriefStateAndListRoutes(t *testing.T) {
	e := newBriefEnv(t)
	e.script = func(args []string) string { return "echo first-line; echo second-line" }
	e.post("/app/briefs", e.briefText("gm-2026-10-23-050"), nil)
	e.waitPhase("gm-2026-10-23-050", phaseDone)

	w := e.get("/app/briefs/gm-2026-10-23-050/state")
	if w.Code != http.StatusOK {
		t.Fatalf("state = %d %s", w.Code, w.Body)
	}
	var st BriefState
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Run.ID != "gm-2026-10-23-050" || st.Run.Phase != phaseDone || st.Run.Active || st.Run.Title == "" {
		t.Errorf("run %+v", st.Run)
	}
	if !strings.Contains(strings.Join(st.LogTail, "\n"), "second-line") {
		t.Errorf("log tail %v", st.LogTail)
	}
	if !strings.Contains(w.Body.String(), `"nodes":[]`) {
		t.Errorf("body %s", w.Body)
	}
	if w := e.get("/app/briefs/gm-2026-10-23-777/state"); w.Code != http.StatusNotFound {
		t.Errorf("unknown state = %d", w.Code)
	}
	if w := e.get("/app/briefs/x/state"); w.Code != http.StatusNotFound {
		t.Errorf("bad id state = %d", w.Code)
	}

	w = e.get("/app/api/briefs")
	m := decodeMap(t, w)
	runs, _ := m["runs"].([]any)
	if w.Code != http.StatusOK || len(runs) != 1 || runs[0].(map[string]any)["id"] != "gm-2026-10-23-050" {
		t.Errorf("list = %d %s", w.Code, w.Body)
	}
}

func TestBriefPagesServed(t *testing.T) {
	e := newBriefEnv(t)
	for path, marker := range map[string]string{
		"/app/briefs":                   "gm-briefs",
		"/app/briefs/new":               "gm-brief-new",
		"/app/briefs/gm-2026-10-23-001": "gm-brief",
	} {
		w := e.get(path)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), marker) {
			t.Errorf("GET %s = %d", path, w.Code)
		}
		csp := w.Body.String()
		if !strings.Contains(csp, "Content-Security-Policy") || strings.Contains(csp, "innerHTML") {
			t.Errorf("%s: policy missing or innerHTML used", path)
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("%s content type %q", path, ct)
		}
	}
}

func TestBriefRoutesOffWithoutDir(t *testing.T) {
	d := Deps{Run: func(ctx context.Context, task string) (string, error) { return "", nil }, Pipeline: &PipelineDeps{Root: t.TempDir()}}
	mux, err := NewMux(d, Options{Token: testBearer})
	if err != nil {
		t.Fatal(err)
	}
	w := do(mux, "GET", "/app/briefs", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("= %d, want 404 when BriefsDir is unset", w.Code)
	}
}

func TestStaleLiveRecordsMarkedInterrupted(t *testing.T) {
	dir := t.TempDir()
	id := "gm-2026-10-23-060"
	if err := os.MkdirAll(filepath.Join(dir, id), 0o755); err != nil {
		t.Fatal(err)
	}
	rec, _ := json.Marshal(briefRecord{ID: id, Phase: phaseRunning})
	os.WriteFile(filepath.Join(dir, id, "run.json"), rec, 0o644)
	m, err := newBriefManager(Options{BriefsDir: dir}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	got, ok := m.readRecord(id)
	if !ok || got.Phase != phaseInterrupted || got.Error == "" {
		t.Errorf("record %+v", got)
	}
}

func TestChildEnvDropsServerSecrets(t *testing.T) {
	t.Setenv("GOPHERMIND_TOKEN", "t0k")
	t.Setenv("GOPHERMIND_SESSION_SECRET", "s3c")
	t.Setenv("GOPHERMIND_VAULT_PATH", "/keep/me")
	env := strings.Join(childEnv(), "\n")
	if strings.Contains(env, "t0k") || strings.Contains(env, "s3c") {
		t.Error("server secrets reach the child")
	}
	if !strings.Contains(env, "GOPHERMIND_VAULT_PATH=/keep/me") {
		t.Error("vault path should pass through")
	}
}
