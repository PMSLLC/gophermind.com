package executor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
)

func sha(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func TestLoadPlanGreeter(t *testing.T) {
	g := newRig(t)
	p := g.plan

	if p.RunID != greeterID || p.RunDir != g.runDir || p.Repo != g.repo {
		t.Errorf("identity = %q %q %q", p.RunID, p.RunDir, p.Repo)
	}
	if p.Brief == nil || p.Brief.Front.ID != greeterID || p.BriefRepo != g.repo {
		t.Errorf("brief = %v, BriefRepo = %q", p.Brief, p.BriefRepo)
	}
	if p.Contracts == nil || p.Contracts.Module != "example.com/greeter" {
		t.Fatal("contracts not loaded")
	}

	var order []string
	for _, l := range p.Leaves {
		order = append(order, l.ID)
	}
	if want := []string{"fn-farewell", "fn-greet", "fn-bye", "fn-hello", "fn-serve"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("leaves sorted by (wave, id) = %v, want %v", order, want)
	}

	serve := p.Leaf("fn-serve")
	if serve == nil || p.Leaf("fn-missing") != nil {
		t.Fatal("Leaf lookup")
	}
	checks := []struct{ name, got, want string }{
		{"FuncID", serve.FuncID, "fn-serve"},
		{"Class", serve.Class, "wiring"},
		{"Tier", serve.Tier, "any"},
		{"Package", serve.Package, "main"},
		{"File", serve.File, "cmd/greeter/serve.go"},
		{"Dir", serve.Dir, "cmd/greeter"},
		{"StubFile", serve.StubFile, "cmd/greeter/zz_gm_stub_fn-serve.go"},
		{"Signature", serve.Signature, "func serve(addr string) error"},
		{"TestFile", serve.TestFile, "cmd/greeter/fn_serve_test.go"},
		{"TestFunc", serve.TestFunc, "TestServe"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("fn-serve %s = %q, want %q", c.name, c.got, c.want)
		}
	}
	if serve.Wave != 2 || !reflect.DeepEqual(serve.DependsOn, []string{"fn-bye", "fn-hello"}) {
		t.Errorf("fn-serve wave %d deps %v", serve.Wave, serve.DependsOn)
	}
	if serve.TestSHA256 != sha(t, filepath.Join(g.repo, serve.TestFile)) {
		t.Error("TestSHA256 is not the hash of the test file the planner wrote")
	}
	if len(serve.DepSignatures) == 0 || !strings.Contains(strings.Join(serve.DepSignatures, "\n"), "byeHandler") {
		t.Errorf("fn-serve dependency signatures = %v, want the handlers' signatures", serve.DepSignatures)
	}
	if !reflect.DeepEqual(serve.Constraints, []string{"gofmt and go vet clean", "Standard library only"}) {
		t.Errorf("fn-serve constraints = %v", serve.Constraints)
	}
	if serve.MaxContextTokens != 0 || serve.MaxRevisions != -1 {
		t.Errorf("unset budget = %d, %d; want 0 and -1", serve.MaxContextTokens, serve.MaxRevisions)
	}
	greet := p.Leaf("fn-greet")
	if greet.Dir != "internal/greet" || greet.TestFunc != "TestGreet" || greet.Wave != 0 || greet.Class != "validation" {
		t.Errorf("fn-greet = %+v", greet)
	}

	if p.Classes["fn-bye"] != "handler" || len(p.Classes) != 5 {
		t.Errorf("classes = %v", p.Classes)
	}
	if len(p.Requirements) != 7 || len(p.Coverage.Covered) != 7 || p.Deps == nil || len(p.Deps) != 0 {
		t.Errorf("requirements %d, covered %d, deps %v", len(p.Requirements), len(p.Coverage.Covered), p.Deps)
	}
	if p.Policy().Module != "example.com/greeter" || len(p.Policy().Deps) != 0 {
		t.Errorf("policy = %+v", p.Policy())
	}

	// Hashes cover contracts.json and every node file, and are the file hashes.
	if p.Hashes["contracts.json"] != sha(t, filepath.Join(g.runDir, "contracts.json")) {
		t.Error("contracts.json hash")
	}
	nodeFiles := 0
	for k, v := range p.Hashes {
		if !strings.HasPrefix(k, "tree/") {
			continue
		}
		nodeFiles++
		if v != sha(t, filepath.Join(g.runDir, filepath.FromSlash(strings.TrimPrefix(k, "tree/")))) {
			t.Errorf("hash of %s", k)
		}
	}
	// root, three components, five leaves
	if nodeFiles != 9 {
		t.Errorf("Hashes holds %d node files, want 9", nodeFiles)
	}
	for _, k := range []string{"tree/root.json", "tree/server/fn-serve.json", "tree/greet/component.json"} {
		if p.Hashes[k] == "" {
			t.Errorf("Hashes lacks %s", k)
		}
	}

	// View and Policy carry what the packer needs.
	v := p.View(serve, "package main\n")
	if v.ID != "fn-serve" || v.FuncID != "fn-serve" || v.Package != "main" || v.File != serve.File || v.Signature != serve.Signature ||
		v.TestFile != serve.TestFile || v.TestSource != "package main\n" || !reflect.DeepEqual(v.DependencySignatures, serve.DepSignatures) ||
		!reflect.DeepEqual(v.Constraints, serve.Constraints) || v.MaxContextTokens != 0 {
		t.Errorf("View = %+v", v)
	}
}

func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() {
			out[rel+"/"] = ""
			return nil
		}
		out[rel] = sha(t, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestLoadPlanNeverWrites(t *testing.T) {
	g := newRig(t)
	before := snapshot(t, g.runDir)
	repoBefore := g.gitCmd("status", "--porcelain", "--untracked-files=all")
	if _, err := LoadPlan(g.runDir, g.repo); err != nil {
		t.Fatal(err)
	}
	if after := snapshot(t, g.runDir); !reflect.DeepEqual(before, after) {
		t.Error("LoadPlan changed the run folder")
	}
	if g.gitCmd("status", "--porcelain", "--untracked-files=all") != repoBefore {
		t.Error("LoadPlan changed the repository")
	}
}

func TestLoadPlanRefusesChangedPlan(t *testing.T) {
	g := newRig(t)
	deps := filepath.Join(g.runDir, "dependencies.json")
	raw, err := os.ReadFile(deps)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deps, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	want := planner.VerifyApproval(g.runDir)
	if want == nil {
		t.Fatal("test setup: one byte of dependencies.json must break the approval")
	}
	p, err := LoadPlan(g.runDir, g.repo)
	if err == nil || err.Error() != want.Error() {
		t.Fatalf("LoadPlan = %v, %v; want VerifyApproval's error %q unchanged", p, err, want)
	}
	if p != nil {
		t.Error("a refused plan must not be returned")
	}
}

func TestLoadPlanRefusesBrokenPlans(t *testing.T) {
	edit := func(t *testing.T, path string, f func(doc map[string]any)) {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := jsonUnmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		f(doc)
		out, err := jsonMarshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, out, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	serveNode := func(g *rig) string { return filepath.Join(g.runDir, "server", "fn-serve.json") }

	t.Run("leaf with no recorded test", func(t *testing.T) {
		g := newRig(t)
		path := filepath.Join(g.runDir, "_state", "leaf_tests.json")
		edit(t, path, func(doc map[string]any) { delete(doc, "fn-bye") })
		_, err := LoadPlan(g.runDir, g.repo)
		if err == nil || err.Error() != "executor: leaf fn-bye has no recorded test" {
			t.Fatalf("LoadPlan = %v", err)
		}
	})
	t.Run("test file missing from the repository", func(t *testing.T) {
		g := newRig(t)
		if err := os.Remove(filepath.Join(g.repo, "cmd", "greeter", "fn_bye_test.go")); err != nil {
			t.Fatal(err)
		}
		_, err := LoadPlan(g.runDir, g.repo)
		if err == nil || err.Error() != "executor: leaf fn-bye: its test file is missing from the repository" {
			t.Fatalf("LoadPlan = %v", err)
		}
	})
	t.Run("unknown dependency", func(t *testing.T) {
		g := newRig(t)
		edit(t, serveNode(g), func(doc map[string]any) { doc["depends_on"] = []any{"fn-bye", "fn-nope"} })
		refusedByTree(t, g, "an unknown dependency")
	})
	t.Run("cycle", func(t *testing.T) {
		g := newRig(t)
		edit(t, filepath.Join(g.runDir, "greet", "fn-greet.json"), func(doc map[string]any) { doc["depends_on"] = []any{"fn-serve"} })
		refusedByTree(t, g, "a dependency cycle")
	})
	t.Run("duplicate id", func(t *testing.T) {
		g := newRig(t)
		raw, err := os.ReadFile(serveNode(g))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(g.runDir, "server", "fn-serve-copy.json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		refusedByTree(t, g, "a second file for one node id")
	})
	t.Run("wave that disagrees with the dependencies", func(t *testing.T) {
		g := newRig(t)
		edit(t, serveNode(g), func(doc map[string]any) { doc["wave"] = 0 })
		refusedByTree(t, g, "a wave that is not the computed one")
	})
}

func jsonUnmarshal(raw []byte, v any) error { return json.Unmarshal(raw, v) }
func jsonMarshal(v any) ([]byte, error)     { return json.MarshalIndent(v, "", "  ") }

// refusedByTree: the plan still has its approval (node files are not part of
// the hash), so the refusal must come from the loader's own checks.
func refusedByTree(t *testing.T, g *rig, what string) {
	t.Helper()
	if err := planner.VerifyApproval(g.runDir); err != nil {
		t.Fatalf("test setup: the approval must still match: %v", err)
	}
	_, err := LoadPlan(g.runDir, g.repo)
	if err == nil {
		t.Fatalf("LoadPlan accepted %s", what)
	}
	if !strings.HasPrefix(err.Error(), "executor: the task tree is not") {
		t.Errorf("LoadPlan error for %s = %q, want the loader's tree refusal", what, err)
	}
}

// editNode rewrites one node file of the run folder (node files are not part
// of the approval hash, so the approval still matches).
func editNode(t *testing.T, path string, f func(doc map[string]any)) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	f(doc)
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestLeafNetworkComesFromRoot: the brief's network block sits on the root
// node only; it applies to every leaf unless the leaf has its own list.
func TestLeafNetworkComesFromRoot(t *testing.T) {
	hosts := []any{
		map[string]any{"host": "api.example.com", "critical": true},
		map[string]any{"host": "cdn.example.com", "critical": false},
	}
	want := []NetHost{{Host: "api.example.com", Critical: true}, {Host: "cdn.example.com"}}

	t.Run("root list reaches every leaf", func(t *testing.T) {
		g := newRig(t)
		editNode(t, filepath.Join(g.runDir, "root.json"), func(d map[string]any) { d["network"] = hosts })
		p, err := LoadPlan(g.runDir, g.repo)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range p.Leaves {
			if !reflect.DeepEqual(l.Network, want) {
				t.Errorf("%s Network = %v, want %v", l.ID, l.Network, want)
			}
		}
	})
	t.Run("a leaf's own list wins", func(t *testing.T) {
		g := newRig(t)
		editNode(t, filepath.Join(g.runDir, "root.json"), func(d map[string]any) { d["network"] = hosts })
		editNode(t, filepath.Join(g.runDir, "server", "fn-serve.json"), func(d map[string]any) {
			d["network"] = []any{map[string]any{"host": "own.example.com", "critical": false}}
		})
		p, err := LoadPlan(g.runDir, g.repo)
		if err != nil {
			t.Fatal(err)
		}
		if got := p.Leaf("fn-serve").Network; !reflect.DeepEqual(got, []NetHost{{Host: "own.example.com"}}) {
			t.Errorf("fn-serve Network = %v", got)
		}
		if got := p.Leaf("fn-bye").Network; !reflect.DeepEqual(got, want) {
			t.Errorf("fn-bye Network = %v, want the root list", got)
		}
	})
	t.Run("an empty root gives none", func(t *testing.T) {
		g := newRig(t)
		for _, l := range g.plan.Leaves {
			if len(l.Network) != 0 {
				t.Errorf("%s Network = %v, want none", l.ID, l.Network)
			}
		}
	})
}
