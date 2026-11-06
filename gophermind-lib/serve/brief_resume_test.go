//go:build !windows

package serve

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/runview/runviewtest"
)

const resumeTestID = "gm-2026-10-08-001"

// writeResumeRecord makes the launcher's record of a run that ended.
func (e *briefEnv) writeResumeRecord(id, phase string) {
	e.t.Helper()
	dir := filepath.Join(e.dir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.t.Fatal(err)
	}
	code := 1
	b, _ := json.Marshal(briefRecord{ID: id, Title: "t", Phase: phase, ExitCode: &code, StartedAt: "2026-10-08T12:00:00Z", FinishedAt: "2026-10-08T12:30:00Z", Error: "brief run exited with code 1"})
	if err := os.WriteFile(filepath.Join(dir, "run.json"), b, 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *briefEnv) firstCall(i int) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if i >= len(e.calls) {
		return ""
	}
	return strings.Join(e.calls[i], " ")
}

func TestResumeFromStageStartsThePlannerStep(t *testing.T) {
	repos := t.TempDir()
	e := newBriefEnv(t, func(o *Options, d *Deps) { o.ReposDir = repos; d.Pipeline.ReposDir = repos })
	clone := filepath.Join(repos, resumeTestID)
	runviewtest.BuildID(t, clone, resumeTestID, runviewtest.Planned)
	if err := os.WriteFile(filepath.Join(clone, ".gophermind", resumeTestID, "brief.md"), []byte(e.briefText(resumeTestID)), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "base"}} {
		if out, err := exec.Command("git", append([]string{"-C", clone}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	e.writeResumeRecord(resumeTestID, phaseFailed)

	w := e.post("/app/briefs/"+resumeTestID+"/resume?from=coverage", "", nil)
	if w.Code != 202 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	body := decodeMap(t, w)
	if body["run_id"] != resumeTestID || body["url"] != "/app/briefs/"+resumeTestID {
		t.Errorf("body = %v", body)
	}
	rec := e.waitPhase(resumeTestID, phaseDone)
	if rec.ResumedFrom != "coverage" || rec.ResumedAt == "" || rec.Error != "" {
		t.Errorf("record = %+v", rec)
	}
	want := "brief resume " + resumeTestID + " --yes --gate file"
	if got := e.firstCall(0); got != want {
		t.Errorf("first call = %q, want %q", got, want)
	}
	if got := e.firstCall(1); !strings.HasPrefix(got, "brief run "+resumeTestID+" --repo ") {
		t.Errorf("second call = %q", got)
	}
	e.waitNotActive(resumeTestID)
	log, _ := os.ReadFile(filepath.Join(e.dir, resumeTestID, "run.log"))
	if !strings.Contains(string(log), "resume from coverage at ") {
		t.Errorf("log = %q", log)
	}
	if _, err := os.Stat(filepath.Join(clone, ".gophermind", resumeTestID, "coverage.json")); err == nil {
		t.Error("coverage.json was not moved aside")
	}
}

func TestResumeFromNodeStartsTheRunStep(t *testing.T) {
	e := newBriefEnv(t)
	runviewtest.BuildID(t, e.root, resumeTestID, runviewtest.Failed)
	e.writeResumeRecord(resumeTestID, phaseFailed)

	w := e.post("/app/briefs/"+resumeTestID+"/resume?from=node:fn-bye", "", nil)
	if w.Code != 202 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	rec := e.waitPhase(resumeTestID, phaseDone)
	if rec.ResumedFrom != "node:fn-bye" {
		t.Errorf("record = %+v", rec)
	}
	e.waitNotActive(resumeTestID)
	if got, want := e.firstCall(0), "brief run "+resumeTestID+" --repo "+e.root; got != want {
		t.Errorf("first call = %q, want %q", got, want)
	}
	if got := e.firstCall(1); got != "" {
		t.Errorf("unexpected second call %q", got)
	}
}

func TestResumeRefusesWhileActive(t *testing.T) {
	e := newBriefEnv(t)
	e.script = func(args []string) string { return "sleep 30" }
	runviewtest.BuildID(t, e.root, resumeTestID, runviewtest.Failed)
	e.writeResumeRecord(resumeTestID, phaseFailed)
	if w := e.post("/app/briefs/"+resumeTestID+"/resume?from=node:fn-bye", "", nil); w.Code != 202 {
		t.Fatalf("first resume %d: %s", w.Code, w.Body.String())
	}
	w := e.post("/app/briefs/"+resumeTestID+"/resume?from=node:fn-bye", "", nil)
	if w.Code != 409 {
		t.Fatalf("second resume %d: %s", w.Code, w.Body.String())
	}
	if got := decodeMap(t, w); got["active_run_id"] != resumeTestID || got["error"] == nil {
		t.Errorf("body = %v", got)
	}
	st := decodeMap(t, e.get("/app/briefs/"+resumeTestID+"/state"))
	if pts, ok := st["run"].(map[string]any)["resume_points"].([]any); !ok || len(pts) != 0 {
		t.Errorf("active run resume_points = %v", st["run"].(map[string]any)["resume_points"])
	}
	e.stopAndDrain(resumeTestID)
}

func TestResumeRefusesAnUnknownStep(t *testing.T) {
	e := newBriefEnv(t)
	runviewtest.BuildID(t, e.root, resumeTestID, runviewtest.Failed)
	e.writeResumeRecord(resumeTestID, phaseFailed)
	w := e.post("/app/briefs/"+resumeTestID+"/resume?from=bogus", "", nil)
	if w.Code != 400 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if msg, _ := decodeMap(t, w)["error"].(string); !strings.Contains(msg, `unknown step "bogus"`) {
		t.Errorf("error = %q", msg)
	}
	// A package refusal comes back verbatim and leaves the run alone.
	w = e.post("/app/briefs/"+resumeTestID+"/resume?from=node:fn-greet", "", nil)
	if w.Code != 400 || decodeMap(t, w)["error"] == "" {
		t.Errorf("verified node: %d %s", w.Code, w.Body.String())
	}
	if w := e.post("/app/briefs/gm-2026-10-08-009/resume?from=coverage", "", nil); w.Code != 404 {
		t.Errorf("unknown run = %d", w.Code)
	}
	if got := e.firstCall(0); got != "" {
		t.Errorf("a process started: %q", got)
	}
	e.waitNotActive(resumeTestID)
}

func TestStateCarriesResumePoints(t *testing.T) {
	e := newBriefEnv(t)
	runviewtest.BuildID(t, e.root, resumeTestID, runviewtest.Failed)
	e.writeResumeRecord(resumeTestID, phaseFailed)
	run := decodeMap(t, e.get("/app/briefs/"+resumeTestID+"/state"))["run"].(map[string]any)
	pts, _ := run["resume_points"].([]any)
	var steps []string
	for _, p := range pts {
		steps = append(steps, p.(map[string]any)["step"].(string))
	}
	if !strings.Contains(strings.Join(steps, ","), "node:fn-bye") || !strings.Contains(strings.Join(steps, ","), "coverage") {
		t.Errorf("steps = %v", steps)
	}

	// A run with a node in progress, and a run with no folder yet, list none and never null.
	runviewtest.BuildID(t, e.root, "gm-2026-10-08-002", runviewtest.Building)
	e.writeResumeRecord("gm-2026-10-08-002", phaseRunning)
	raw := e.get("/app/briefs/gm-2026-10-08-002/state").Body.String()
	if !strings.Contains(raw, `"resume_points":[]`) {
		t.Errorf("building run state lacks an empty resume_points array")
	}
	e.writeResumeRecord("gm-2026-10-08-003", phasePlanning)
	if raw := e.get("/app/briefs/gm-2026-10-08-003/state").Body.String(); !strings.Contains(raw, `"resume_points":[]`) {
		t.Errorf("run with no folder: resume_points not an empty array: %.200s", raw)
	}
}

func TestRunPageHasTheResumeControl(t *testing.T) {
	e := newBriefEnv(t)
	page := e.get("/app/briefs/" + resumeTestID).Body.String()
	for _, want := range []string{
		`<select id="resume-from" hidden></select><button id="resume" type="button" hidden>Resume from this step</button>`,
		`/resume?from=`, `window.location.reload()`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("run page lacks %q", want)
		}
	}
}
