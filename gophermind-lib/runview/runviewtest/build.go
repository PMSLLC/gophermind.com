// Package runviewtest builds run folders for tests with the real writers, so a
// change to the layout of a run folder breaks the tests of every reader.
package runviewtest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/artifacts"
	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/report"
	"gophermind/gophermind-lib/briefv2/runfs"
	"gophermind/gophermind-lib/briefv2/tree"
)

// RunID is the id of every fixture run unless BuildID is used.
const RunID = "gm-2026-10-08-001"

// T0 is the fixed clock of the fixtures' attempts and files.
var T0 = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

// Stage says how far the fixture run got.
type Stage int

const (
	Planning Stage = iota
	Waiting
	Planned
	Building
	Verified
	Failed
	HarnessFault
	// PlanFailed is a /project run that stopped in a planner stage: the plan
	// files, no approval.json, no report.json, and _state/project.json.
	PlanFailed
)

// Fixture is a built run folder. BB writes more runtime state into it, which
// also appends to its _state/events.jsonl.
type Fixture struct {
	Root, Dir, RunID string
	BB               *blackboard.FS
}

func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func writeFile(t testing.TB, path string, data []byte) {
	t.Helper()
	must(t, runfs.WriteFileAtomic(path, data))
}

func writeJSON(t testing.TB, path string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	must(t, err)
	writeFile(t, path, append(b, '\n'))
}

// Build builds the fixture run RunID under root/.gophermind.
func Build(t testing.TB, root string, stage Stage) Fixture { return BuildID(t, root, RunID, stage) }

// BuildID builds a fixture run with the given id.
func BuildID(t testing.TB, root, runID string, stage Stage) Fixture {
	t.Helper()
	dir := filepath.Join(root, ".gophermind", runID)
	must(t, os.MkdirAll(filepath.Join(dir, "_state"), 0o700))
	writeFile(t, filepath.Join(dir, "brief.md"), []byte("# Greeter\n\n## Features\n\n### Greeting\n- Greets a visitor by name.\n"))
	f := Fixture{Root: root, Dir: dir, RunID: runID}

	if stage == Planning || stage == Waiting {
		waiting := ""
		if stage == Waiting {
			waiting = "clarify"
		}
		writeJSON(t, filepath.Join(dir, "_state", "status.json"),
			map[string]any{"allow_public": false, "ledger_errors": 0, "waiting": waiting, "planned_at": ""})
		return f
	}

	writePlan(t, dir, runID)
	writeJSON(t, filepath.Join(dir, "requirements.json"),
		[]planner.Requirement{{ID: "F1", Kind: planner.ReqFeature, Name: "Greeting", Text: "Greets a visitor by name.", Line: 5}})
	writeJSON(t, filepath.Join(dir, "coverage.json"),
		planner.CoverageFile{Covered: []planner.Covered{{Requirement: "F1", Nodes: []string{"fn-greet"}}}, RootTests: []planner.RootTest{}, Warnings: []string{}})
	writeJSON(t, filepath.Join(dir, "_state", "status.json"),
		map[string]any{"allow_public": false, "ledger_errors": 0, "waiting": "", "planned_at": runfs.TS(T0)})
	if stage == PlanFailed {
		// Mirrors projectrun's _state/project.json: run_dir and secrets must
		// never be served by a reader.
		writeJSON(t, filepath.Join(dir, "_state", "project.json"), map[string]any{
			"status": "failed", "stop_reason": "plan:coverage", "exit_code": 5,
			"run_dir": dir, "secrets": []string{"FAKE_SECRET_NAME", "OTHER_FAKE_SECRET"},
		})
		return f
	}
	writeJSON(t, filepath.Join(dir, "approval.json"),
		map[string]any{"approved_at": runfs.TS(T0.Add(time.Minute)), "approved_by": "test", "plan_hash": "abc"})
	if stage == Planned {
		return f
	}

	writeJSON(t, filepath.Join(dir, "_state", "executor.json"), map[string]any{"started_at": runfs.TS(T0.Add(2 * time.Minute))})
	ctx := context.Background()
	bb := blackboard.NewFS(runfs.Fixed(dir))
	f.BB = bb
	must(t, bb.InitRun(ctx, runID, []string{"fn-greet", "fn-bye", "fn-late"}, map[string]int{"fn-greet": 0, "fn-bye": 1, "fn-late": 1}))

	// fn-greet: one failed attempt, then a passing one, then verified.
	toInProgress(t, bb, runID, "fn-greet")
	must(t, bb.AppendAttempt(ctx, runID, "fn-greet", attempt(1, blackboard.VerdictFail, "compile: undefined: name")))
	must(t, bb.AppendAttempt(ctx, runID, "fn-greet", attempt(2, blackboard.VerdictPass, "")))
	must(t, bb.SetResult(ctx, runID, "fn-greet", blackboard.Result{FilesChanged: []string{"greeting/fn-greet.go"}, Commit: "abc1234"}))
	must(t, bb.SetStatus(ctx, runID, "fn-greet", blackboard.StatusInProgress, blackboard.StatusVerified))

	w := artifacts.New(dir, true, 0, nil)
	_, err := w.Save("fn-greet", artifacts.Meta{Provider: "mini", Model: "qwen3.6", Order: 1, Verdict: "fail", Class: "compile", Reason: "undefined: name", StartedAt: T0, DurationMS: 900},
		artifacts.Files{Raw: "reply one", CheckOutput: "greeting/fn-greet.go:3: undefined: name"})
	must(t, err)

	led := ledger.NewFS(runfs.Fixed(dir))
	for i, n := range []string{"fn-greet", "fn-greet"} {
		c := ledger.Call{RunID: runID, At: T0.Add(time.Duration(i) * time.Minute), Stage: "implement:" + n, NodeID: n, Scope: "node",
			TaskType: "implement", NodeClass: "pure", Provider: "mini", ModelRequested: "qwen3.6", ModelServed: "qwen3.6",
			DurationMS: 900, Outcome: ledger.OutcomeOK}
		must(t, led.Record(ctx, &c))
	}

	switch stage {
	case Building:
		toInProgress(t, bb, runID, "fn-bye")
		must(t, bb.AppendAttempt(ctx, runID, "fn-bye", attempt(1, blackboard.VerdictFail, "test: TestBye failed")))
		_, err := w.Save("fn-bye", artifacts.Meta{Provider: "mini", Model: "qwen3.6", Order: 1, Verdict: "fail", Class: "test", Reason: "TestBye failed", StartedAt: T0, DurationMS: 1300},
			artifacts.Files{Raw: "reply for bye", CheckOutput: "--- FAIL: TestBye\n    bye_test.go:9: want bye, got hello"})
		must(t, err)
		_, err = planner.NewReviser(planner.Deps{Now: func() time.Time { return T0.Add(5 * time.Minute) }}).RecordDecision(planner.RevisionDecision{
			RunDir: dir, NodeID: "fn-bye", Revision: 1, Class: "test", Action: "retry", Note: "try once more", AnsweredBy: "human", Round: 1,
		})
		must(t, err)
	case Verified:
		for _, id := range []string{"fn-bye", "fn-late"} {
			toInProgress(t, bb, runID, id)
			must(t, bb.AppendAttempt(ctx, runID, id, attempt(1, blackboard.VerdictPass, "")))
			must(t, bb.SetResult(ctx, runID, id, blackboard.Result{FilesChanged: []string{"greeting/" + id + ".go"}, Commit: "def5678"}))
			must(t, bb.SetStatus(ctx, runID, id, blackboard.StatusInProgress, blackboard.StatusVerified))
		}
		WriteReport(t, f, "verified", "", 0, report.NodeCounts{Total: 3, Verified: 3})
	case Failed, HarnessFault:
		toInProgress(t, bb, runID, "fn-bye")
		must(t, bb.AppendAttempt(ctx, runID, "fn-bye", attempt(1, blackboard.VerdictFail, "test: TestBye failed")))
		must(t, bb.SetStatus(ctx, runID, "fn-bye", blackboard.StatusInProgress, blackboard.StatusFailed))
		reason, code := "leaf_failed", 1
		if stage == HarnessFault {
			reason, code = "harness_fault", 7
		}
		WriteReport(t, f, "failed", reason, code, report.NodeCounts{Total: 3, Verified: 1, Failed: 1, Blocked: 1})
	}
	return f
}

// WriteReport writes report.json into the fixture, as the executor does when a
// run ends. A verified report carries one revised leaf, one rewritten leaf and
// Parallel{Workers 2, ParallelLevels 1}; other statuses carry none. A test uses it to finish a Building run while a reader is watching.
func WriteReport(t testing.TB, f Fixture, status, reason string, code int, n report.NodeCounts) {
	t.Helper()
	var revised []report.RevisedLeaf
	var par *report.Parallel
	rewritten := 0
	if status == "verified" {
		revised = []report.RevisedLeaf{{NodeID: "fn-bye", Revisions: 1, Changed: []string{"tests"}, TestsRewritten: true}}
		rewritten = 1
		par = &report.Parallel{Workers: 2, ParallelLevels: 1}
	}
	rep := report.Report{
		SchemaVersion: report.SchemaVersion, RunID: f.RunID,
		StartedAt: runfs.TS(T0.Add(2 * time.Minute)), FinishedAt: runfs.TS(T0.Add(20 * time.Minute)),
		Status: status, StopReason: reason, ExitCode: code, Sandbox: "none",
		Requirements: report.Coverage{Covered: 1, Total: 1}, Waves: 2, Nodes: n,
		Revised: revised, Rewritten: rewritten, Parallel: par,
		ByTaskType: []report.TaskModel{}, Leaves: []report.LeafModel{},
	}
	must(t, report.Write(f.Dir, rep, report.WriteOptions{RepoRoot: f.Root}))
}

// Verify takes one node of a built fixture from its current pending state to
// verified through the real transitions (each appends to the event log).
func Verify(t testing.TB, f Fixture, id string) {
	t.Helper()
	ctx := context.Background()
	toInProgress(t, f.BB, f.RunID, id)
	must(t, f.BB.AppendAttempt(ctx, f.RunID, id, attempt(1, blackboard.VerdictPass, "")))
	must(t, f.BB.SetResult(ctx, f.RunID, id, blackboard.Result{FilesChanged: []string{"greeting/" + id + ".go"}, Commit: "fed9876"}))
	must(t, f.BB.SetStatus(ctx, f.RunID, id, blackboard.StatusInProgress, blackboard.StatusVerified))
}

func toInProgress(t testing.TB, bb *blackboard.FS, runID, id string) {
	t.Helper()
	ctx := context.Background()
	must(t, bb.SetStatus(ctx, runID, id, blackboard.StatusPending, blackboard.StatusReady))
	ok, err := bb.Claim(ctx, runID, id, "worker-1")
	must(t, err)
	if !ok {
		t.Fatalf("claim of %s lost", id)
	}
	must(t, bb.SetStatus(ctx, runID, id, blackboard.StatusClaimed, blackboard.StatusInProgress))
}

func attempt(order int, v blackboard.Verdict, reason string) blackboard.Attempt {
	a := blackboard.Attempt{
		Model: "qwen3.6", Provider: "mini", Order: order, StartedAt: T0.Add(time.Duration(order) * time.Minute),
		DurationMS: 1200, Verdict: v, TestsTotal: 2, FailureReason: reason,
	}
	if v == blackboard.VerdictPass {
		a.TestsPassed = 2
	} else {
		a.TestsPassed = 1
		a.FailedTests = []string{"TestBye"}
	}
	return a
}

func writePlan(t testing.TB, dir, runID string) {
	t.Helper()
	st := tree.NewStore(dir)
	greetExtra := `,"model_tier":"standard","node_class":"pure","requirement_ids":["F1"],` +
		`"rationale":"Serves the greeting feature so a visitor is welcomed by name.",` +
		`"construction":{"approach_chosen":"Concatenate a fixed salutation and the name.","steps":["Trim the name","Return the greeting"]}`
	for _, js := range []string{
		fmt.Sprintf(`{"spec_version":"2.0","id":%q,"kind":"root","children":["greeting"],"title":"Greeter","description":"A tiny greeting service.","brief_ref":"#overview","status":"pending","wave":0,`+
			`"decisions":[{"id":"q1","question":"Which store?","kind":"decision","options":["memory","postgres"],"recommended":"memory","answer":"memory","answered_by":"accepted","round":1,"raised_by":"clarify","settled_at":"2026-10-08T00:00:00Z"}]}`, runID),
		fmt.Sprintf(`{"spec_version":"2.0","id":"greeting","kind":"component","parent":%q,"children":["fn-greet","fn-bye","fn-late"],"title":"Greeting","description":"Greeting functions.","brief_ref":"#greeting","status":"pending","wave":0}`, runID),
		fnJSON("fn-greet", 0, nil, greetExtra),
		fnJSON("fn-bye", 1, []string{"fn-greet"}, ""),
		fnJSON("fn-late", 1, []string{"fn-greet"}, ""),
	} {
		n, err := tree.ParseNode([]byte(js))
		must(t, err)
		must(t, st.Write(n))
	}
}

func fnJSON(id string, wave int, deps []string, extra string) string {
	if deps == nil {
		deps = []string{}
	}
	d, _ := json.Marshal(deps)
	return fmt.Sprintf(`{"spec_version":"2.0","id":%q,"kind":"function","parent":"greeting","title":"Function %s","description":"Implements %s.","brief_ref":"#greeting","status":"pending","wave":%d,"depends_on":%s,`+
		`"contract":{"package":"greeting","file":"greeting/%s.go","signature":"func F()","inputs":[],"outputs":[]},`+
		`"tests":[{"name":"t","level":"unit","given":"g","expect":"e","command":"go test ./greeting"}]%s}`, id, id, id, wave, d, id, extra)
}

// Digest hashes the names, sizes, modification times and contents of every
// file under dir, so a test can prove a reader wrote nothing.
func Digest(t testing.TB, dir string) string {
	t.Helper()
	h := sha256.New()
	var paths []string
	must(t, filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, p)
		return nil
	}))
	sort.Strings(paths)
	for _, p := range paths {
		fi, err := os.Lstat(p)
		must(t, err)
		fmt.Fprintf(h, "%s|%d|%d|%v\n", p, fi.Size(), fi.ModTime().UnixNano(), fi.Mode())
		if fi.Mode().IsRegular() {
			b, err := os.ReadFile(p)
			must(t, err)
			h.Write(b)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}
