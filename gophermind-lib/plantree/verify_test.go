package plantree

import (
	"strings"
	"testing"
)

func addStep(t *testing.T, r *Repo, id string, deps ...string) {
	t.Helper()
	n := mk(t, id)
	n.DependsOn = deps
	if err := r.Create(n); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyCleanTree(t *testing.T) {
	r, _ := newRepo(t)
	addStep(t, r, "phase-001.task-001.step-002", step1)
	if err := r.Verify(); err != nil {
		t.Errorf("Verify on a clean tree: %v", err)
	}
}

func TestVerifyMissingDependency(t *testing.T) {
	r, _ := newRepo(t)
	addStep(t, r, "phase-001.task-001.step-002", "phase-001.task-001.step-009")
	err := r.Verify()
	if err == nil || !strings.Contains(err.Error(), "step-009") {
		t.Errorf("Verify = %v, want an error naming the missing dependency", err)
	}
}

func TestVerifyDetectsCycleAndNamesPath(t *testing.T) {
	r, _ := newRepo(t)
	const a, b, c = "phase-001.task-001.step-002", "phase-001.task-001.step-003", "phase-001.task-001.step-004"
	addStep(t, r, a, c)
	addStep(t, r, b, a)
	addStep(t, r, c, b)
	err := r.Verify()
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("Verify = %v, want a cycle error", err)
	}
	for _, id := range []string{a, b, c} {
		if !strings.Contains(err.Error(), id) {
			t.Errorf("cycle error does not name %s: %v", id, err)
		}
	}
}
