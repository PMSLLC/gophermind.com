package tree_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/tree"
)

func TestStoreWriteLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := tree.NewStore(dir)
	tr := mustTree(t, fn(t, "fn-a", "comp", 9), fn(t, "fn-b", "comp", 9, "fn-a"))
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
	if len(back.Nodes) != 2 {
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
	tr := mustTree(t, fn(t, "fn-a", "c", 1, "fn-b"), fn(t, "fn-b", "c", 1, "fn-a"))
	if err := s.WriteAll(tr); err == nil {
		t.Fatal("cyclic tree must not be written")
	}
}

func TestLoadSkipsNonNodeFilesAndRuntime(t *testing.T) {
	dir := t.TempDir()
	s := tree.NewStore(dir)
	if err := s.Write(fn(t, "fn-a", "comp", 0)); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"contracts.json", "answers.json", "approval.json", "report.json", "comp/fn-a.runtime.json"} {
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
