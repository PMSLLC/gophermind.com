package planner_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/settings"
	"gophermind/gophermind-lib/briefv2/tree"
	"gophermind/gophermind-lib/briefv2/vault"
)

const greeterBadTest = "testdata/greeter-badtest"

// repoFiles lists every file in the target repository outside .gophermind/.
func (g *rig) repoFiles() []string {
	g.t.Helper()
	var out []string
	err := filepath.WalkDir(g.repo, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(g.repo, p)
		if d.IsDir() {
			if rel == ".gophermind" {
				return filepath.SkipDir
			}
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		g.t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

func (g *rig) node(path string) map[string]any {
	g.t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(g.read(path), &doc); err != nil {
		g.t.Fatal(err)
	}
	return doc
}

var greeterTestFiles = []string{
	"internal/greet/fn_farewell_test.go",
	"internal/greet/fn_greet_test.go",
	"internal/greet/fn_name_error_error_test.go",
}

// The whole pipeline against canned replies, answered through the gate the
// run service and the desktop app will use.
func TestPlanEndToEnd(t *testing.T) {
	gate := human.NewProgrammatic()
	g := newRig(t, gate)
	stop := make(chan struct{})
	defer close(stop)
	shown := make(chan human.PlanSummary, 1)
	go func() {
		for {
			select {
			case <-stop:
				return
			case req := <-gate.Requests():
				switch req.Kind {
				case human.KindAsk:
					as := make([]human.Answer, len(req.Questions))
					for i, q := range req.Questions {
						as[i] = human.Answer{ID: q.ID, Text: "Yes, trim it."}
					}
					req.Answer(as)
				case human.KindApprove:
					shown <- req.Plan
					req.Decide(human.Decision{Approved: true, By: "programmatic"})
				}
			}
		}
	}()
	g.mustPlan(planner.Options{})

	// The tree is complete and consistent.
	tr, err := tree.NewStore(g.runDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.CheckStructure(); err != nil {
		t.Fatal(err)
	}
	if err := tr.CheckWaves(); err != nil {
		t.Fatal(err)
	}
	if len(tr.Nodes) != 7 {
		t.Fatalf("tree has %d nodes, want 7 (root, three components, three functions)", len(tr.Nodes))
	}
	for id, want := range map[string]int{"fn-name-error-error": 0, "fn-farewell": 0, "fn-greet": 1} {
		if n := tr.Nodes[id]; n.Wave == nil || *n.Wave != want {
			t.Errorf("wave of %s = %v, want %d", id, n.Wave, want)
		}
	}
	for _, path := range []string{"types/fn-name-error-error.json", "greeting/fn-greet.json", "farewell/fn-farewell.json"} {
		doc := g.node(path)
		if _, ok := doc["contract"].(map[string]any); !ok {
			t.Errorf("%s has no contract", path)
		}
		tests, _ := doc["tests"].([]any)
		if len(tests) == 0 {
			t.Fatalf("%s has no tests", path)
		}
		for _, x := range tests {
			tc := x.(map[string]any)
			cmd, _ := tc["command"].(string)
			if !strings.HasPrefix(cmd, "go test ./internal/greet -run ^Test") || tc["level"] != "unit" {
				t.Errorf("%s test %v = level %v, command %q; both are set by the harness", path, tc["name"], tc["level"], cmd)
			}
		}
		if deps := strs(doc["depends_on"]); len(deps) > 0 {
			if sigs := strs(doc["context"].(map[string]any)["dependency_signatures"]); len(sigs) == 0 {
				t.Errorf("%s depends on %v but has no dependency_signatures", path, deps)
			}
		}
	}
	if tests := g.rootTests(); len(tests) != 4 {
		t.Errorf("root has %d acceptance tests, want 4", len(tests))
	}

	// The repository holds the test files and nothing else; the blackboard holds one pending row per node.
	if got := g.repoFiles(); strings.Join(got, " ") != strings.Join(greeterTestFiles, " ") {
		t.Errorf("repo files = %v", got)
	}
	var forbidden []string
	if err := json.Unmarshal(g.read("_state/test_files.json"), &forbidden); err != nil || strings.Join(forbidden, " ") != strings.Join(greeterTestFiles, " ") {
		t.Errorf("test_files.json = %v, %v", forbidden, err)
	}
	rows, err := g.board.List(context.Background(), greeterID, blackboard.Filter{})
	if err != nil || len(rows) != 7 {
		t.Fatalf("blackboard rows = %d, %v; want 7", len(rows), err)
	}
	for _, row := range rows {
		if row.Status != blackboard.StatusPending {
			t.Errorf("row %s is %s, want pending", row.NodeID, row.Status)
		}
	}

	// What was approved is what stands, and it showed the coverage.
	plan := <-shown
	for _, want := range []string{"# Plan: Greeter", "| greeting | 1 | 1 |", "Functions: 3 in 2 wave(s).", "Requirements covered: 7 of 7",
		"| C2 | `gofmt` clean and `go vet` clean. | none | formatted and vetted |", "      go test ./...", "- Secrets (names only): none"} {
		if !strings.Contains(plan.Markdown, want) {
			t.Errorf("the approval summary lacks %q", want)
		}
	}
	var ap struct {
		ApprovedBy string `json:"approved_by"`
		PlanHash   string `json:"plan_hash"`
	}
	if err := json.Unmarshal(g.read("approval.json"), &ap); err != nil || ap.ApprovedBy != "programmatic" || ap.PlanHash != plan.Hash {
		t.Errorf("approval.json = %+v, %v", ap, err)
	}
	if _, hash, err := planner.RenderPlan(g.runDir); err != nil || hash != plan.Hash {
		t.Errorf("the plan no longer hashes to what was approved: %v", err)
	}
	st, err := planner.ReadStatus(greeterID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range st.Stages {
		if !s.Done {
			t.Errorf("stage %s is not done", s.Name)
		}
	}

	// A finished run has nothing left to do.
	g.wire()
	g.mustPlan(planner.Options{RunID: greeterID})
	if n := len(g.fake.Requests()); n != 0 {
		t.Errorf("resuming a finished run made %d model calls", n)
	}
}

// Every ledger row says what kind of work the call was, and a call about one
// leaf says what class of function it is, so models can be compared by both.
func TestLedgerRowsCarryTheTaskTypeAndTheNodeClass(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{})
	ctx := context.Background()
	rows, err := g.led.List(ctx, greeterID, ledger.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	byTask := map[string]int{}
	for _, r := range rows {
		byTask[r.TaskType]++
		leaf := r.TaskType == "testwrite"
		if leaf != (r.NodeClass != "") || leaf != (r.NodeID != "") {
			t.Errorf("row %s: task %q, node %q, class %q", r.Stage, r.TaskType, r.NodeID, r.NodeClass)
		}
	}
	want := map[string]int{"clarify": 1, "contract": 4, "decompose": 3, "coverage": 1, "testwrite": 3}
	for task, n := range want {
		if byTask[task] != n {
			t.Errorf("%d rows of task type %s, want %d (all: %v)", byTask[task], task, n, byTask)
		}
	}
	if len(byTask) != len(want) {
		t.Errorf("task types = %v", byTask)
	}
	sum, err := g.led.Summary(ctx, greeterID)
	if err != nil {
		t.Fatal(err)
	}
	groups := map[string]int{}
	for _, s := range sum {
		groups[s.TaskType+"/"+s.NodeClass] = s.Calls
	}
	if groups["testwrite/validation"] != 2 || groups["testwrite/pure"] != 1 || groups["contract/"] != 4 {
		t.Errorf("summary groups = %v", groups)
	}
}

// Nothing is written into the target repository before the plan is approved.
func TestNothingTouchesTheRepoWithoutApproval(t *testing.T) {
	gate := &scriptGate{decision: human.Decision{Approved: false, Note: "too big"}}
	g := newRig(t, gate)
	_, err := g.plan(planner.Options{})
	if err == nil || !strings.Contains(err.Error(), "plan not approved: too big") {
		t.Fatalf("err = %v, want the refusal", err)
	}
	if files := g.repoFiles(); len(files) != 0 {
		t.Errorf("files in the repo without approval: %v", files)
	}
	if g.has("approval.json") {
		t.Error("approval.json exists for a refused plan")
	}
	for _, s := range g.stagesCalled() {
		if strings.HasPrefix(s, "testwrite:") {
			t.Errorf("%s was called without approval", s)
		}
	}
	rows, _ := g.board.List(context.Background(), greeterID, blackboard.Filter{})
	if len(rows) != 0 {
		t.Errorf("%d blackboard rows exist for an unapproved plan", len(rows))
	}
}

// Stop after each stage in turn and resume: no stage that had finished makes
// a model call again.
func TestResumeRepeatsOnlyWhatIsUnfinished(t *testing.T) {
	order := []string{"load", "clarify", "contract", "decompose", "coverage", "approve"}
	owner := func(stage string) string {
		switch name, _, _ := strings.Cut(stage, ":"); name {
		case "coverage_fill":
			return "coverage"
		case "testwrite":
			return "testwriter"
		default:
			return name
		}
	}
	for i, stop := range order {
		t.Run("after "+stop, func(t *testing.T) {
			g := newRig(t, approving())
			g.mustPlan(planner.Options{StopAfter: stop})
			finished := map[string]bool{}
			for _, s := range order[:i+1] {
				finished[s] = true
			}
			g.wire()
			g.mustPlan(planner.Options{RunID: greeterID})
			calls := g.stagesCalled()
			for _, s := range calls {
				if finished[owner(s)] {
					t.Errorf("resume after %s called %s again", stop, s)
				}
			}
			if count(calls, "testwrite:fn-greet") != 1 {
				t.Errorf("resume after %s did not finish the run: %v", stop, calls)
			}
			if got := g.repoFiles(); len(got) != 3 {
				t.Errorf("repo files = %v", got)
			}
		})
	}

	t.Run("waiting for approval", func(t *testing.T) {
		gate := approving()
		gate.err = human.ErrWaiting
		g := newRig(t, gate, variant(t, map[string]string{"clarify.txt": "[]"}))
		out, err := g.plan(planner.Options{})
		if err != nil || out != planner.Waiting {
			t.Fatalf("Run = %q, %v; want waiting", out, err)
		}
		if st, _ := planner.ReadStatus(greeterID); st.Waiting != "approve" {
			t.Errorf("status.Waiting = %q, want approve", st.Waiting)
		}
		gate.err = nil
		g.wire()
		g.mustPlan(planner.Options{RunID: greeterID})
		for _, s := range g.stagesCalled() {
			if !strings.HasPrefix(s, "testwrite:") {
				t.Errorf("resume after approval called %s", s)
			}
		}
	})

	t.Run("in the middle of the test writer", func(t *testing.T) {
		g := newRig(t, approving(), variant(t, map[string]string{"testwrite.fn-greet.txt": "nope", "testwrite.fn-greet.2.txt": "nope"}))
		if _, err := g.plan(planner.Options{}); err == nil || !strings.Contains(err.Error(), "node fn-greet") {
			t.Fatalf("err = %v, want the test writer to fail on fn-greet", err)
		}
		g.wire()
		g.mustPlan(planner.Options{RunID: greeterID})
		if got := strings.Join(g.stagesCalled(), " "); got != "testwrite:fn-greet" {
			t.Errorf("resume called %q, want only testwrite:fn-greet", got)
		}
	})
}

// An approval is for one exact plan. Change what it covered and the test
// writer refuses to run.
func TestAChangedPlanInvalidatesTheApproval(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "approve"})
	edited := strings.Replace(string(g.read("coverage.json")), `"greeting"`, `"fn-greet"`, 1)
	if err := os.WriteFile(filepath.Join(g.runDir, "coverage.json"), []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	g.wire()
	_, err := g.plan(planner.Options{RunID: greeterID})
	if err == nil || !strings.Contains(err.Error(), "approval.json does not match the plan as it stands") {
		t.Fatalf("err = %v, want the stale approval refused", err)
	}
	if files := g.repoFiles(); len(files) != 0 || len(g.fake.Requests()) != 0 {
		t.Errorf("files %v, %d model calls after a stale approval", files, len(g.fake.Requests()))
	}
}

func TestCoverageFillRunsThroughToAFinishedTree(t *testing.T) {
	g := newRig(t, approving(), greeterGap)
	g.mustPlan(planner.Options{})
	tr, err := tree.NewStore(g.runDir).Load()
	if err != nil || len(tr.Nodes) != 8 {
		t.Fatalf("tree = %d nodes, %v; want 8 (the fill added a function)", len(tr.Nodes), err)
	}
	if !contains(g.repoFiles(), "internal/greet/fn_imports_are_standard_test.go") {
		t.Errorf("repo files = %v", g.repoFiles())
	}
	md, _, err := planner.RenderPlan(g.runDir)
	if err != nil || !strings.Contains(md, "Coverage fill rounds: 1") || !strings.Contains(md, "- Contract revision: 1") {
		t.Errorf("plan summary does not show the fill round: %v", err)
	}
}

func contains(list []string, s string) bool { return count(list, s) > 0 }

// A test file that breaks a rule is a malformed reply: the same model gets
// one more try, and only a clean file is ever written.
func TestABadTestFileIsRefusedAndRetried(t *testing.T) {
	good := string(mustRead(t, filepath.Join(greeter, "testwrite.fn-greet.txt")))
	one := `{"tests": [{"name": "greets a name", "given": "Ada", "expect": "Hello, Ada!"}], "test_file": "package greet\n\nimport \"testing\"\n\nfunc TestGreet(t *testing.T) {}\n"}`
	cases := []struct {
		name string
		dir  string
	}{
		{"an import outside the standard library and the module", greeterBadTest},
		{"fewer tests than error conditions plus one", variant(t, map[string]string{"testwrite.fn-greet.txt": one, "testwrite.fn-greet.2.txt": good})},
		{"a question first", variant(t, map[string]string{"testwrite.fn-greet.txt": "QUESTION:\nShould the greeting end with an exclamation mark?", "testwrite.fn-greet.2.txt": good})},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newRig(t, approving(), c.dir)
			g.mustPlan(planner.Options{})
			rows, _ := g.led.List(context.Background(), greeterID, ledger.Filter{Stage: "testwrite:fn-greet"})
			wantFirst := ledger.OutcomeMalformed
			if i == 2 {
				wantFirst = ledger.OutcomeOK // a question is a usable reply, not a broken one
			}
			if len(rows) != 2 || rows[0].Outcome != wantFirst || rows[1].Outcome != ledger.OutcomeOK {
				t.Fatalf("rows for testwrite:fn-greet = %+v", rows)
			}
			src, err := os.ReadFile(filepath.Join(g.repo, "internal/greet/fn_greet_test.go"))
			if err != nil || strings.Contains(string(src), "testify") || !strings.Contains(string(src), "refuses an empty name") {
				t.Errorf("the written test file is not the clean one: %v", err)
			}
		})
	}
}

// A contract file that points out of the repository never produces a write
// outside it, and a file the run did not write is never overwritten.
func TestTheTestWriterOnlyWritesItsOwnFilesInsideTheRepo(t *testing.T) {
	t.Run("a path that leaves the repository", func(t *testing.T) {
		g := newRig(t, approving())
		g.mustPlan(planner.Options{StopAfter: "approve"})
		edited := strings.Replace(string(g.read("_state/decomposed.json")), "internal/greet/greet.go", "../outside/greet.go", 1)
		if err := os.WriteFile(filepath.Join(g.runDir, "_state", "decomposed.json"), []byte(edited), 0o600); err != nil {
			t.Fatal(err)
		}
		// The edit invalidates the approval; approve again so the path guard is what is tested.
		if err := os.Remove(filepath.Join(g.runDir, "approval.json")); err != nil {
			t.Fatal(err)
		}
		g.wire()
		_, err := g.plan(planner.Options{RunID: greeterID})
		if err == nil || !strings.Contains(err.Error(), "node fn-greet") || !strings.Contains(err.Error(), "is not a clean path inside the repository") {
			t.Fatalf("err = %v, want fn-greet refused for its path", err)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(g.repo), "outside")); err == nil {
			t.Error("a directory was created outside the repository")
		}
		if count(g.stagesCalled(), "testwrite:fn-greet") != 0 {
			t.Error("the model was asked for a test file that could never be written")
		}
	})
	t.Run("a file that is already there", func(t *testing.T) {
		g := newRig(t, approving())
		mine := filepath.Join(g.repo, "internal", "greet", "fn_greet_test.go")
		if err := os.MkdirAll(filepath.Dir(mine), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(mine, []byte("package greet\n// written by a person\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := g.plan(planner.Options{})
		if err == nil || !strings.Contains(err.Error(), "fn_greet_test.go already exists") {
			t.Fatalf("err = %v, want the existing file named", err)
		}
		if got, _ := os.ReadFile(mine); !strings.Contains(string(got), "written by a person") {
			t.Error("the existing file was overwritten")
		}
	})
}

// The privacy rule, end to end: a public provider sits first in the strong
// chain. It may write tests for one function (node scope) and must never see
// a call that carries the brief or a component.
func TestAPublicProviderOnlyEverSeesNodeScopeCalls(t *testing.T) {
	g := newRig(t, approving())
	inner, err := planner.FixtureProvider(greeter)
	if err != nil {
		t.Fatal(err)
	}
	pub := provider.NewFake("pub", []provider.ModelInfo{{ID: "fixture", ContextTokens: 1 << 20}},
		func(_ int, req provider.Request) (provider.Response, error) {
			return inner.Complete(context.Background(), req)
		})
	cfg := planner.FixtureSettings()
	cfg.Providers = append(cfg.Providers, settings.ProviderConfig{Name: "pub", BaseURL: "http://public.invalid/v1",
		Visibility: settings.Public, MaxConcurrent: 1, Models: []settings.ModelEntry{{ID: "fixture", ContextTokens: 1 << 20}}})
	cfg.Models["strong"] = []string{"pub/fixture", "fake/fixture"}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	g.router = router.New(cfg, map[string]provider.Provider{"fake": g.fake, "pub": pub}, g.led, g.sink)
	g.deps.Caller, g.deps.Settings = g.router, cfg
	g.mustPlan(planner.Options{})

	if n := len(pub.Requests()); n != 3 {
		t.Errorf("the public provider got %d requests, want the 3 test-writer calls", n)
	}
	for _, r := range pub.Requests() {
		if s := planner.StageOf(r); !strings.HasPrefix(s, "testwrite:") {
			t.Errorf("the public provider was sent a %s call", s)
		}
		if strings.Contains(r.Messages[1].Content, "## Overview") {
			t.Error("the public provider was sent the brief")
		}
	}
	for _, s := range g.stagesCalled() {
		if strings.HasPrefix(s, "testwrite:") {
			t.Errorf("the private provider answered %s although the public one comes first for node scope", s)
		}
	}
}

// A secret's value is in the vault and nowhere else: not in the run folder,
// the repository, the run registry, or the database.
func TestNoSecretValueLeavesTheVault(t *testing.T) {
	g := newRig(t, approving())
	g.briefPath = writeBrief(t, g.repo, withSecret)
	store := &memSecrets{vals: map[string]string{vault.HarnessScope + "/GREETER_API_KEY": canary}}
	g.deps.OpenSecrets = func() (planner.Secrets, error) { return store, nil }
	g.mustPlan(planner.Options{})

	root := g.node("root.json")
	if got := strs(root["secrets"]); len(got) != 1 || got[0] != "GREETER_API_KEY" {
		t.Errorf("root secrets = %v, want the name only", got)
	}
	g.db.Close()
	dirs := []string{g.repo, os.Getenv("GOPHERMIND_CONFIG_DIR"), filepath.Dir(g.dbPath)}
	for _, dir := range dirs {
		filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if raw, _ := os.ReadFile(p); strings.Contains(string(raw), canary) {
					t.Errorf("%s contains the secret value", p)
				}
			}
			return nil
		})
	}
	for _, r := range g.fake.Requests() {
		for _, m := range r.Messages {
			if strings.Contains(m.Content, canary) {
				t.Errorf("a %s prompt contains the secret value", planner.StageOf(r))
			}
		}
	}
}

// A crash between writing a test file and saving the state: the file is
// recognised by its recorded hash and no model call is repeated; a file with
// other content is still refused.
func TestResumeAfterACrashBetweenTheWriteAndTheSave(t *testing.T) {
	crash := func(t *testing.T) *rig {
		g := newRig(t, approving())
		g.mustPlan(planner.Options{})
		var st struct {
			Nodes   map[string]json.RawMessage `json:"nodes"`
			Pending map[string]json.RawMessage `json:"pending"`
		}
		if err := json.Unmarshal(g.read("_state/testwriter.json"), &st); err != nil {
			t.Fatal(err)
		}
		st.Pending = map[string]json.RawMessage{"fn-greet": st.Nodes["fn-greet"]}
		delete(st.Nodes, "fn-greet")
		writeFileT(t, filepath.Join(g.runDir, "_state", "testwriter.json"), st)
		var status map[string]any
		if err := json.Unmarshal(g.read("_state/status.json"), &status); err != nil {
			t.Fatal(err)
		}
		delete(status, "planned_at")
		writeFileT(t, filepath.Join(g.runDir, "_state", "status.json"), status)
		g.wire()
		return g
	}
	t.Run("the recorded file is accepted", func(t *testing.T) {
		g := crash(t)
		g.mustPlan(planner.Options{RunID: greeterID})
		if got := g.stagesCalled(); len(got) != 0 {
			t.Errorf("resume called %v", got)
		}
	})
	t.Run("a foreign file is refused", func(t *testing.T) {
		g := crash(t)
		mine := filepath.Join(g.repo, "internal", "greet", "fn_greet_test.go")
		if err := os.WriteFile(mine, []byte("package greet\n// written by a person\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := g.plan(planner.Options{RunID: greeterID})
		if err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("err = %v", err)
		}
		if got, _ := os.ReadFile(mine); !strings.Contains(string(got), "written by a person") {
			t.Error("the file was overwritten")
		}
	})
}

func writeFileT(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A symbolic link at the final path, even a dangling one, is never written through.
func TestTheTestWriterNeverWritesThroughASymlink(t *testing.T) {
	t.Run("dangling link at the final path", func(t *testing.T) {
		g := newRig(t, approving())
		outside := filepath.Join(t.TempDir(), "target")
		dir := filepath.Join(g.repo, "internal", "greet")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(dir, "fn_greet_test.go")); err != nil {
			t.Skip(err)
		}
		if _, err := g.plan(planner.Options{}); err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("err = %v", err)
		}
		if _, err := os.Stat(outside); err == nil {
			t.Error("a file was written through the link")
		}
	})
	t.Run("parent directory linked outside", func(t *testing.T) {
		g := newRig(t, approving())
		outside := t.TempDir()
		if err := os.MkdirAll(filepath.Join(g.repo, "internal"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(g.repo, "internal", "greet")); err != nil {
			t.Skip(err)
		}
		if _, err := g.plan(planner.Options{}); err == nil || !strings.Contains(err.Error(), "outside the repository") {
			t.Fatalf("err = %v", err)
		}
		if ents, _ := os.ReadDir(outside); len(ents) != 0 {
			t.Error("a file was written outside the repository")
		}
	})
}

// A parent directory swapped for a link to the outside while the model is
// answering is caught by the checks made just before the write.
func TestAParentSwappedForALinkDuringTheCallIsRefused(t *testing.T) {
	g := newRig(t, approving())
	outside := t.TempDir()
	dir := filepath.Join(g.repo, "internal", "greet")
	swapped := false
	defer planner.SetBeforeTestWrite(func() {
		if swapped {
			return
		}
		swapped = true
		if err := os.RemoveAll(filepath.Join(g.repo, "internal")); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(g.repo, "internal"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, dir); err != nil {
			t.Skip(err)
		}
	})()
	_, err := g.plan(planner.Options{})
	if err == nil || !strings.Contains(err.Error(), "outside the repository") {
		t.Fatalf("err = %v", err)
	}
	if ents, _ := os.ReadDir(outside); len(ents) != 0 {
		t.Errorf("%d entries were written outside the repository", len(ents))
	}
}

// The approval covers what will be written to the repository: signatures,
// file paths and dependency edges, none of which the summary text shows.
func TestEditingASignatureAFilePathOrADependsOnInvalidatesTheApproval(t *testing.T) {
	cases := []struct{ name, file, from, to string }{
		{"a signature in the contract", "contracts.json", "func Greet(name string) (string, error)", "func Greet(name string, loud bool) (string, error)"},
		{"a file path in the contract", "contracts.json", "internal/greet/greet.go", "internal/greet/hello.go"},
		{"a file path in a draft", "_state/decomposed.json", "internal/greet/farewell.go", "internal/greet/bye.go"},
		{"a depends_on in a draft", "_state/decomposed.json", "re:\"depends_on\": \\[\\s*\"fn-name-error-error\"\\s*\\]", `"depends_on": []`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newRig(t, approving())
			g.mustPlan(planner.Options{StopAfter: "approve"})
			raw := string(g.read(c.file))
			edited := strings.Replace(raw, c.from, c.to, 1)
			if re, ok := strings.CutPrefix(c.from, "re:"); ok {
				edited = regexp.MustCompile(re).ReplaceAllLiteralString(raw, c.to)
			}
			if edited == raw {
				t.Fatalf("the edit %q did not change %s", c.from, c.file)
			}
			if err := os.WriteFile(filepath.Join(g.runDir, c.file), []byte(edited), 0o600); err != nil {
				t.Fatal(err)
			}
			g.wire()
			_, err := g.plan(planner.Options{RunID: greeterID})
			if err == nil || !strings.Contains(err.Error(), "approval.json does not match the plan as it stands") {
				t.Fatalf("err = %v, want the stale approval refused", err)
			}
			if files := g.repoFiles(); len(files) != 0 {
				t.Errorf("files written after a stale approval: %v", files)
			}
		})
	}
}
