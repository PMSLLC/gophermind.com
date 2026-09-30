package schema_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/schema"
)

const exampleTree = "../testdata/example/tree/gm-2026-09-29-001"

func TestExampleNodesValidate(t *testing.T) {
	n := 0
	err := filepath.WalkDir(exampleTree, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(p) == "contracts.json" {
			return err
		}
		raw, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		if verr := schema.Validate(schema.KindNode, raw); verr != nil {
			t.Errorf("%s: %v", p, verr)
		}
		n++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 7 {
		t.Fatalf("expected 7 example node files, found %d", n)
	}
}

func TestExampleContractsValidate(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(exampleTree, "contracts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(schema.KindContract, raw); err != nil {
		t.Fatal(err)
	}
}

// mutate loads a fixture, applies fn to its decoded form, and re-encodes it.
func mutate(t *testing.T, rel string, fn func(map[string]any)) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(exampleTree, rel))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	fn(m)
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRejects(t *testing.T) {
	cases := []struct {
		name string
		doc  []byte
		kind schema.Kind
	}{
		{"function test without command", mutate(t, "registration/fn-validate-email.json", func(m map[string]any) {
			for _, x := range m["tests"].([]any) {
				delete(x.(map[string]any), "command")
			}
		}), schema.KindNode},
		{"component without parent", mutate(t, "registration/component.json", func(m map[string]any) { delete(m, "parent") }), schema.KindNode},
		{"bad status", mutate(t, "root.json", func(m map[string]any) { m["status"] = "done" }), schema.KindNode},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := schema.Validate(c.kind, c.doc); err == nil {
				t.Fatal("expected a validation error")
			}
		})
	}
}

func TestBriefErrorsNameTheField(t *testing.T) {
	cases := map[string]string{
		"spec_version": `{"id":"gm-2026-09-29-001","title":"t","language":"go","repo":"r","base_branch":"main","landing":"commit","on_ambiguity":"halt"}`,
		"language":     `{"spec_version":"2.0","id":"gm-2026-09-29-001","title":"t","language":"python","repo":"r","base_branch":"main","landing":"commit","on_ambiguity":"halt"}`,
	}
	for field, doc := range cases {
		err := schema.Validate(schema.KindBrief, []byte(doc))
		if err == nil || !strings.Contains(err.Error(), field) {
			t.Errorf("%s: want an error naming the field, got %v", field, err)
		}
	}
}

func TestUnknownKindAndBadJSON(t *testing.T) {
	if err := schema.Validate("nope", []byte(`{}`)); err == nil {
		t.Error("unknown kind must error")
	}
	if err := schema.Validate(schema.KindNode, []byte(`{`)); err == nil {
		t.Error("malformed JSON must error")
	}
}

func TestRawReturnsTheEmbeddedSchema(t *testing.T) {
	raw, err := schema.Raw(schema.KindContract)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("Raw(KindContract) is not JSON: %v", err)
	}
	if doc["$id"] != "https://gophermind.local/schema/contract/2.0" {
		t.Errorf("$id = %v", doc["$id"])
	}
	if _, err := schema.Raw("nope"); err == nil {
		t.Error("unknown kind must error")
	}
}
