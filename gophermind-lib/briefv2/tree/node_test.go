package tree_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"gophermind/gophermind-lib/briefv2/tree"
)

const ex = "../testdata/example/tree/gm-2026-09-29-001"

func readNode(t *testing.T, rel string) tree.Node {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(ex, rel))
	if err != nil {
		t.Fatal(err)
	}
	n, err := tree.ParseNode(raw)
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	return n
}

// fn builds a valid function node with the given wave (schema requires one).
func fn(t *testing.T, id, parent string, wave int, deps ...string) tree.Node {
	t.Helper()
	dj := "[]"
	if len(deps) > 0 {
		dj = "["
		for i, d := range deps {
			if i > 0 {
				dj += ","
			}
			dj += fmt.Sprintf("%q", d)
		}
		dj += "]"
	}
	raw := fmt.Sprintf(`{"spec_version":"2.0","id":%q,"kind":"function","parent":%q,"title":"t","description":"d","brief_ref":"#x","status":"pending","wave":%d,"depends_on":%s,
"contract":{"package":"p","file":"p/%s.go","signature":"func F()","inputs":[],"outputs":[]},
"tests":[{"name":"n","level":"unit","given":"g","expect":"e","command":"go test ./p"}]}`, id, parent, wave, dj, id)
	n, err := tree.ParseNode([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestParseExampleNodes(t *testing.T) {
	root := readNode(t, "root.json")
	if root.Kind != tree.KindRoot || root.Path() != "root.json" || len(root.Children) != 4 {
		t.Errorf("root: %+v", root)
	}
	c := readNode(t, "registration/component.json")
	if c.Path() != "registration/component.json" || c.Wave == nil || *c.Wave != 1 || c.DependsOn[0] != "types" {
		t.Errorf("component: %+v", c)
	}
	f := readNode(t, "registration/fn-validate-email.json")
	if f.Path() != "registration/fn-validate-email.json" || f.Parent != "registration" {
		t.Errorf("function: %+v", f)
	}
}

func TestParseRejectsInvalid(t *testing.T) {
	if _, err := tree.ParseNode([]byte(`{"id":"x"}`)); err == nil {
		t.Fatal("schema-invalid node must be rejected")
	}
}

func TestMarshalKeepsUnmodelledFields(t *testing.T) {
	n := readNode(t, "registration/fn-register-handler.json")
	n.SetWave(7)
	raw, err := n.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	back, err := tree.ParseNode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if *back.Wave != 7 || len(back.DependsOn) != 5 {
		t.Errorf("round trip lost data: %+v", back)
	}
}
