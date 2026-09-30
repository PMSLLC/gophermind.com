package planner_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
)

func TestFinishTreeWritesLeafTests(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{})
	lt, err := planner.ReadLeafTests(g.runDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(lt) != 3 {
		t.Fatalf("leaf tests = %d, want the 3 function nodes", len(lt))
	}
	got := lt["fn-greet"]
	if got.TestFunc != "TestGreet" {
		t.Errorf("fn-greet test_func = %q", got.TestFunc)
	}
	raw, err := os.ReadFile(filepath.Join(g.repo, filepath.FromSlash(got.TestFile)))
	if err != nil {
		t.Fatalf("test_file %q is not on disk: %v", got.TestFile, err)
	}
	sum := sha256.Sum256(raw)
	if got.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("sha256 = %s, want the hash of the file on disk", got.SHA256)
	}
}

func TestVerifyApproval(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "coverage"})
	if err := planner.VerifyApproval(g.runDir); err == nil {
		t.Error("passed with no approval.json")
	}
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "approve"})
	if err := planner.VerifyApproval(g.runDir); err != nil {
		t.Errorf("match: %v", err)
	}
	if err := os.WriteFile(filepath.Join(g.runDir, "approval.json"), []byte(`{"plan_hash": "0000"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := planner.VerifyApproval(g.runDir)
	const want = "approval.json does not match the plan as it stands; remove it and resume to approve the plan again"
	if err == nil || err.Error() != want {
		t.Errorf("mismatch: %v", err)
	}
}

func TestReadAccessors(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{})

	deps, err := planner.ReadDependencies(g.runDir)
	if err != nil || deps == nil || len(deps) != 0 {
		t.Errorf("deps = %#v, %v", deps, err)
	}
	if err := os.Remove(filepath.Join(g.runDir, "dependencies.json")); err != nil {
		t.Fatal(err)
	}
	if deps, err = planner.ReadDependencies(g.runDir); err != nil || deps == nil || len(deps) != 0 {
		t.Errorf("absent deps = %#v, %v; want empty list", deps, err)
	}
	body := `[{"module": "github.com/x/y", "version": "v1.2.3", "purpose": "p"}]`
	if err := os.WriteFile(filepath.Join(g.runDir, "dependencies.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if deps, err = planner.ReadDependencies(g.runDir); err != nil || len(deps) != 1 || deps[0].Version != "v1.2.3" {
		t.Errorf("deps = %+v, %v", deps, err)
	}

	classes, err := planner.ReadClasses(g.runDir)
	if err != nil || len(classes) == 0 || classes["fn-greet"] == "" {
		t.Errorf("classes = %v, %v", classes, err)
	}
	reqs, err := planner.ReadRequirements(g.runDir)
	if err != nil || len(reqs) == 0 || reqs[0].ID == "" {
		t.Errorf("requirements = %+v, %v", reqs, err)
	}
	lt, err := planner.ReadLeafTests(g.runDir)
	if err != nil || lt["fn-farewell"].TestFunc != "TestFarewell" {
		t.Errorf("leaf tests = %+v, %v", lt, err)
	}
	if planner.JSONErr(os.ErrNotExist) == "" || planner.SyntaxErr(os.ErrNotExist) == "" {
		t.Error("JSONErr or SyntaxErr returned nothing")
	}
}
