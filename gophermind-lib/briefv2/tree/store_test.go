package tree_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/tree"
)

// rootN builds a root node with no children listed.
func rootN(t *testing.T, id string) tree.Node {
	t.Helper()
	raw := fmt.Sprintf(`{"spec_version":"2.0","id":%q,"kind":"root","title":"t","description":"d","brief_ref":"#x","status":"pending","children":[]}`, id)
	n, err := tree.ParseNode([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// comp builds a component node under parent.
func comp(t *testing.T, id, parent string) tree.Node {
	t.Helper()
	raw := fmt.Sprintf(`{"spec_version":"2.0","id":%q,"kind":"component","parent":%q,"title":"t","description":"d","brief_ref":"#x","status":"pending","children":[]}`, id, parent)
	n, err := tree.ParseNode([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestStoreWriteLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := tree.NewStore(dir)
	tr := mustTree(t, rootN(t, "rt"), comp(t, "comp", "rt"), fn(t, "fn-a", "comp", 9), fn(t, "fn-b", "comp", 9, "fn-a"))
	if err := s.WriteAll(tr); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "comp", "fn-a.json")); err != nil {
		t.Fatalf("expected comp/fn-a.json: %v", err)
	}
	back, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Nodes) != 4 {
		t.Fatalf("loaded %d nodes", len(back.Nodes))
	}
	if err := back.CheckWaves(); err != nil {
		t.Errorf("WriteAll must recompute waves: %v", err)
	}
	if w := *back.Nodes["fn-b"].Wave; w != 1 {
		t.Errorf("fn-b wave = %d, want 1", w)
	}
}

func TestWriteAllRejectsCycles(t *testing.T) {
	s := tree.NewStore(t.TempDir())
	tr := mustTree(t, rootN(t, "rt"), comp(t, "c", "rt"), fn(t, "fn-a", "c", 1, "fn-b"), fn(t, "fn-b", "c", 1, "fn-a"))
	err := s.WriteAll(tr)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cyclic tree must not be written, got %v", err)
	}
}

func TestLoadSkipsNonNodeFilesAndRuntime(t *testing.T) {
	dir := t.TempDir()
	s := tree.NewStore(dir)
	if err := s.Write(fn(t, "fn-a", "comp", 0)); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"contracts.json", "answers.json", "approval.json", "report.json", "dependencies.json", "comp/fn-a.runtime.json"} {
		p := filepath.Join(dir, f)
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		if err := os.WriteFile(p, []byte(`{"not":"a node"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tr, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Nodes) != 1 {
		t.Errorf("loaded %d nodes, want 1", len(tr.Nodes))
	}
}

func TestLoadRejectsNodeInWrongDirectory(t *testing.T) {
	dir := t.TempDir()
	n := fn(t, "fn-a", "comp", 0)
	raw, _ := n.Marshal()
	if err := os.MkdirAll(filepath.Join(dir, "elsewhere"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "elsewhere", "fn-a.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := tree.NewStore(dir).Load(); err == nil || !strings.Contains(err.Error(), "comp/fn-a.json") {
		t.Fatalf("want a wrong-location error naming the expected path, got %v", err)
	}
}

func TestWriteRejectsInvalidNode(t *testing.T) {
	n := fn(t, "fn-a", "comp", 0)
	n.SetWave(-1) // schema minimum is 0
	if err := tree.NewStore(t.TempDir()).Write(n); err == nil {
		t.Fatal("schema-invalid node must not be written")
	}
}

func TestPathLikeIDCannotEscapeTheStore(t *testing.T) {
	raw := `{"spec_version":"2.0","id":"../evil","kind":"root","title":"t","description":"d","brief_ref":"#x","status":"pending"}`
	if _, err := tree.ParseNode([]byte(raw)); err == nil {
		t.Fatal("an ID with path separators must be rejected by the schema")
	}
}

func TestParseRejectsPathyReferences(t *testing.T) {
	base := func(parent, children, deps string) string {
		return `{"spec_version":"2.0","id":"fn-a","kind":"function","parent":"` + parent + `","title":"t","description":"d","brief_ref":"#x","status":"pending","wave":0,"depends_on":` + deps + `,
"contract":{"package":"p","file":"p/a.go","signature":"func F()","inputs":[],"outputs":[]},
"tests":[{"name":"n","level":"unit","given":"g","expect":"e","command":"go test ./p"}]}`
	}
	for _, c := range []struct{ name, raw string }{
		{"parent traversal", base("../../x", "", "[]")},
		{"parent slash", base("a/b", "", "[]")},
		{"depends_on slash", base("comp", "", `["a/b"]`)},
		{"depends_on dotdot", base("comp", "", `[".."]`)},
	} {
		if _, err := tree.ParseNode([]byte(c.raw)); err == nil {
			t.Errorf("%s: must be rejected", c.name)
		}
	}
	comp := `{"spec_version":"2.0","id":"comp","kind":"component","title":"t","description":"d","brief_ref":"#x","status":"pending","children":["a/b"]}`
	if _, err := tree.ParseNode([]byte(comp)); err == nil {
		t.Error("children entry with slash must be rejected")
	}
	comp = `{"spec_version":"2.0","id":"comp","kind":"component","title":"t","description":"d","brief_ref":"#x","status":"pending","children":[".."]}`
	if _, err := tree.ParseNode([]byte(comp)); err == nil {
		t.Error("children entry dotdot must be rejected")
	}
}

func TestWriteCannotEscapeStore(t *testing.T) {
	outer := t.TempDir()
	dir := filepath.Join(outer, "store")
	n := fn(t, "fn-a", "comp", 0)
	n.Parent = "../evil" // bypasses ParseNode; Write must still refuse
	if err := tree.NewStore(dir).Write(n); err == nil {
		t.Fatal("Write must refuse a path outside the store")
	}
	if _, err := os.Stat(filepath.Join(outer, "evil")); err == nil {
		t.Fatal("a directory was created outside the store")
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("nothing should have been written")
	}
}

func TestNodesNamedLikeRunArtifactsSurviveLoad(t *testing.T) {
	dir := t.TempDir()
	s := tree.NewStore(dir)
	tr := mustTree(t, rootN(t, "rt"), comp(t, "comp", "rt"), fn(t, "report", "comp", 0), fn(t, "contracts", "comp", 0))
	if err := s.WriteAll(tr); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"contracts.json", "report.json"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(`{"not":"a node"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	back, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Nodes) != 4 || back.Nodes["report"].ID == "" || back.Nodes["contracts"].ID == "" {
		t.Errorf("nodes lost on load: %d", len(back.Nodes))
	}
}

func TestLogsComponentRejectedAndRootLogsDirSkipped(t *testing.T) {
	raw := `{"spec_version":"2.0","id":"logs","kind":"component","parent":"root-x","children":[],"title":"t","description":"d","brief_ref":"#x","status":"pending"}`
	if _, err := tree.ParseNode([]byte(raw)); err == nil {
		t.Fatal("component id logs must be rejected")
	}
	dir := t.TempDir()
	s := tree.NewStore(dir)
	if err := s.Write(fn(t, "fn-a", "comp", 0)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "logs", "x.json"), []byte(`{"not":"a node"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	back, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Nodes) != 1 {
		t.Errorf("loaded %d nodes, want 1", len(back.Nodes))
	}
}

func TestWriteAllWritesNothingForABadStructure(t *testing.T) {
	dir := t.TempDir()
	// function parented under "logs": no such component can exist.
	tr := mustTree(t, rootN(t, "rt"), comp(t, "comp", "rt"), fn(t, "fn-a", "logs", 0))
	if err := tree.NewStore(dir).WriteAll(tr); err == nil {
		t.Fatal("bad structure must not be written")
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Fatalf("WriteAll wrote %d entries for a bad tree", len(ents))
	}
}

func TestLoadSkipsPlannerArtifacts(t *testing.T) {
	dir := t.TempDir()
	s := tree.NewStore(dir)
	if err := s.Write(fn(t, "fn-a", "comp", 0)); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"requirements.json", "coverage.json", "_state/decomposed.json", "_state/status.json"} {
		p := filepath.Join(dir, f)
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		if err := os.WriteFile(p, []byte(`{"not":"a node"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tr, err := s.Load()
	if err != nil {
		t.Fatalf("planner artifacts must not be read as nodes: %v", err)
	}
	if len(tr.Nodes) != 1 {
		t.Errorf("loaded %d nodes, want 1", len(tr.Nodes))
	}
}
