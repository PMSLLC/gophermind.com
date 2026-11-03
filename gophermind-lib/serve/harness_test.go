package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// harnessEnv is a brief env whose harness reads two example briefs: a small
// one that passes and a bigger one whose plan fails until pass is set.
func harnessEnv(t *testing.T) (*briefEnv, string) {
	t.Helper()
	old := harnessEvery
	harnessEvery = 20 * time.Millisecond
	t.Cleanup(func() { harnessEvery = old })
	ex := t.TempDir()
	var e *briefEnv
	e = newBriefEnv(t, func(o *Options, d *Deps) { o.HarnessBriefs = ex })
	small := strings.Replace(e.briefText("gm-2026-10-29-901"), "title:", "title: Small", 1)
	big := e.briefText("gm-2026-10-29-902") + "\n<!-- " + strings.Repeat("padding ", 200) + "-->\n"
	if err := os.WriteFile(filepath.Join(ex, "a-small.md"), []byte(small), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ex, "b-big.md"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ex, "README.md"), []byte("# not a brief\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return e, ex
}

func (e *briefEnv) harnessState() harnessState {
	e.t.Helper()
	w := e.get("/app/api/harness")
	if w.Code != http.StatusOK {
		e.t.Fatalf("GET /app/api/harness = %d %s", w.Code, w.Body.String())
	}
	var st harnessState
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		e.t.Fatal(err)
	}
	return st
}

func (e *briefEnv) waitHarness(ok func(harnessState) bool) harnessState {
	e.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if st := e.harnessState(); ok(st) {
			return st
		}
		time.Sleep(20 * time.Millisecond)
	}
	st := e.harnessState()
	e.t.Fatalf("harness never got there: %+v", st)
	return st
}

func TestHarnessRetriesUntilEveryBriefPasses(t *testing.T) {
	e, _ := harnessEnv(t)
	var fail = true
	e.script = func(args []string) string {
		e.mu.Lock()
		defer e.mu.Unlock()
		// The big brief's plan fails the first time it runs, then passes.
		if len(args) > 2 && args[1] == "plan" && fail {
			b, _ := os.ReadFile(args[2])
			if strings.Contains(string(b), "padding") {
				fail = false
				return "exit 1"
			}
		}
		return "echo ran"
	}
	st := e.harnessState()
	if st.Running || len(st.Briefs) != 2 || st.Briefs[0].File != "a-small.md" {
		t.Fatalf("initial state = %+v (smallest first, README left out, not running)", st)
	}
	if w := e.post("/app/api/harness/start", "", nil); w.Code != http.StatusOK {
		t.Fatalf("start = %d", w.Code)
	}
	st = e.waitHarness(func(s harnessState) bool { return s.Done })
	if st.Running {
		t.Error("the harness still runs after every brief passed")
	}
	byFile := map[string]harnessBrief{}
	for _, b := range st.Briefs {
		byFile[b.File] = b
	}
	if b := byFile["a-small.md"]; b.Attempts != 1 || b.Passes != 1 || b.LastPhase != phaseDone {
		t.Errorf("small = %+v, want one attempt that passed", b)
	}
	if b := byFile["b-big.md"]; b.Attempts != 2 || b.Passes != 1 || b.LastPhase != phaseDone {
		t.Errorf("big = %+v, want a failed attempt, then a pass", b)
	}
	// Runs carry library names; the retry is a second run of the same version.
	day := time.Now().UTC().Format("20060102")
	for file, b := range byFile {
		if strings.HasPrefix(b.LastRun, "gm-") || !strings.Contains(b.LastRun, "-v1-"+day+"-r") {
			t.Errorf("%s last run = %q, want a <family>-v1-%s-rNNN library name", file, b.LastRun, day)
		}
	}
	if r := byFile["b-big.md"].LastRun; !strings.HasSuffix(r, "-r002") {
		t.Errorf("big last run = %q, want the second run of the same version (-r002)", r)
	}
}

// A brief whose plan stops at questions is parked: the harness moves on, does
// not start it again while it waits, and counts it once the answered run ends.
func TestHarnessParksARunWaitingForAnswers(t *testing.T) {
	e, _ := harnessEnv(t)
	ask := func(args []string) string { return "echo ran" }
	e.askScript(gateFixture)
	ask, e.script = e.script, func(args []string) string {
		if len(args) > 2 && args[1] == "plan" {
			if b, _ := os.ReadFile(args[2]); !strings.Contains(string(b), "title: Small") {
				return "echo ran"
			}
		}
		return ask(args)
	}
	e.post("/app/api/harness/start", "", nil)
	st := e.waitHarness(func(s harnessState) bool {
		return len(s.Briefs) == 2 && s.Briefs[0].LastPhase == phaseWaiting && s.Briefs[1].Passes == 1 && s.Current == ""
	})
	if st.Done || !st.Running || !strings.Contains(st.Note, "answers") {
		t.Errorf("state while a run waits = %+v, want running with a note about answers", st)
	}
	small := st.Briefs[0]
	time.Sleep(100 * time.Millisecond)
	if again := e.harnessState(); again.Briefs[0].Attempts != 1 {
		t.Errorf("the waiting brief was started again: %+v", again.Briefs[0])
	}
	if w := e.post("/app/briefs/"+small.LastRun+"/answer", answerBody("clarify-q1", "sqlite"), nil); w.Code != http.StatusOK {
		t.Fatalf("first answer = %d %s", w.Code, w.Body)
	}
	if w := e.post("/app/briefs/"+small.LastRun+"/answer", answerBody("clarify-q2", "no"), nil); w.Code != http.StatusAccepted && w.Code != http.StatusOK {
		t.Fatalf("last answer = %d %s", w.Code, w.Body)
	}
	st = e.waitHarness(func(s harnessState) bool { return s.Done })
	if b := st.Briefs[0]; b.Attempts != 1 || b.Passes != 1 || b.LastPhase != phaseDone {
		t.Errorf("small = %+v, want its one answered attempt to pass", b)
	}
}

func TestHarnessStateSurvivesARestart(t *testing.T) {
	e, ex := harnessEnv(t)
	e.post("/app/api/harness/start", "", nil)
	e.waitHarness(func(s harnessState) bool { return s.Done })
	// A second server on the same folders reads the score back.
	e2 := newBriefEnv(t, func(o *Options, d *Deps) { o.HarnessBriefs = ex; o.BriefsDir = e.dir })
	st := e2.harnessState()
	if !st.Done || len(st.Briefs) != 2 || st.Briefs[0].Passes != 1 {
		t.Errorf("state after restart = %+v", st)
	}
	if w := e2.post("/app/api/harness/reset", "", nil); w.Code != http.StatusOK {
		t.Fatalf("reset = %d", w.Code)
	}
	if st := e2.harnessState(); st.Done || st.Briefs[0].Passes != 0 || st.Briefs[0].Attempts != 0 {
		t.Errorf("state after reset = %+v", st)
	}
}

func TestHarnessStopKeepsItFromStartingRuns(t *testing.T) {
	e, _ := harnessEnv(t)
	e.post("/app/api/harness/start", "", nil)
	e.post("/app/api/harness/stop", "", nil)
	st := e.waitHarness(func(s harnessState) bool { return s.Current == "" })
	time.Sleep(100 * time.Millisecond)
	after := e.harnessState()
	total := func(s harnessState) (n int) {
		for _, b := range s.Briefs {
			n += b.Attempts
		}
		return n
	}
	if after.Running || total(after) != total(st) {
		t.Errorf("a stopped harness started runs: before %+v after %+v", st, after)
	}
}

func TestHarnessPageAndRoutesNeedSignIn(t *testing.T) {
	e, _ := harnessEnv(t)
	if w := e.get("/app/harness"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `aria-current="page"`) {
		t.Errorf("page = %d", w.Code)
	}
	for _, p := range []string{"/app/api/harness/start", "/app/api/harness/stop", "/app/api/harness/reset"} {
		if w := do(e.h, "POST", p, nil); w.Code != http.StatusUnauthorized {
			t.Errorf("POST %s without a session = %d, want 401", p, w.Code)
		}
	}
}
