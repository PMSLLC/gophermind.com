package plantree

import "testing"

func TestParseID(t *testing.T) {
	good := map[string]Kind{
		"plan":                        KindPlan,
		"phase-001":                   KindPhase,
		"phase-001.task-002":          KindTask,
		"phase-001.task-002.step-999": KindStep,
	}
	for id, want := range good {
		got, err := ParseID(id)
		if err != nil || got != want {
			t.Errorf("ParseID(%q) = %q, %v; want %q", id, got, err, want)
		}
	}
	bad := []string{
		"", "phase-1", "phase-000", "task-001", "phase-001.step-001",
		"phase-001.task-001.step-001.substep-001", "phase-001.task-001.",
		"Phase-001", "phase-001.task-1000", "plan.phase-001",
	}
	for _, id := range bad {
		if _, err := ParseID(id); err == nil {
			t.Errorf("ParseID(%q) accepted an invalid id", id)
		}
	}
}

func TestParentID(t *testing.T) {
	cases := map[string]string{
		"plan":                        "",
		"phase-001":                   "plan",
		"phase-001.task-002":          "phase-001",
		"phase-001.task-002.step-003": "phase-001.task-002",
	}
	for id, want := range cases {
		got, err := ParentID(id)
		if err != nil || got != want {
			t.Errorf("ParentID(%q) = %q, %v; want %q", id, got, err, want)
		}
	}
}

func TestChildID(t *testing.T) {
	cases := []struct {
		parent string
		n      int
		want   string
	}{
		{"plan", 1, "phase-001"},
		{"phase-001", 12, "phase-001.task-012"},
		{"phase-001.task-002", 999, "phase-001.task-002.step-999"},
	}
	for _, c := range cases {
		got, err := ChildID(c.parent, c.n)
		if err != nil || got != c.want {
			t.Errorf("ChildID(%q, %d) = %q, %v; want %q", c.parent, c.n, got, err, c.want)
		}
	}
	if _, err := ChildID("phase-001.task-001.step-001", 1); err == nil {
		t.Error("a step must not have children")
	}
	if _, err := ChildID("plan", 0); err == nil {
		t.Error("sibling number 0 must be rejected")
	}
	if _, err := ChildID("plan", 1000); err == nil {
		t.Error("sibling number 1000 must be rejected")
	}
}

func TestPaths(t *testing.T) {
	const step = "phase-001.task-002.step-003"
	if got, _ := RelDir(step); got != "phases/phase-001/tasks/task-002/steps/step-003" {
		t.Errorf("RelDir(step) = %q", got)
	}
	if got, _ := RelDir(RootID); got != "" {
		t.Errorf("RelDir(plan) = %q, want empty", got)
	}
	if got, _ := MetaPath(RootID); got != "plan.json" {
		t.Errorf("MetaPath(plan) = %q", got)
	}
	if got, _ := MetaPath("phase-001"); got != "phases/phase-001/meta.json" {
		t.Errorf("MetaPath(phase) = %q", got)
	}
	refs := map[string]string{
		"plan":                        "",
		"phase-001":                   "../../plan.json",
		"phase-001.task-001":          "../../meta.json",
		"phase-001.task-001.step-001": "../../meta.json",
	}
	for id, want := range refs {
		if got, err := ParentRef(id); err != nil || got != want {
			t.Errorf("ParentRef(%q) = %q, %v; want %q", id, got, err, want)
		}
	}
	rel, ok, err := ContainerDir("phase-001.task-002")
	if err != nil || !ok || rel != "phases/phase-001/tasks/task-002/steps" {
		t.Errorf("ContainerDir(task) = %q, %v, %v", rel, ok, err)
	}
	rel, ok, err = ContainerDir(RootID)
	if err != nil || !ok || rel != "phases" {
		t.Errorf("ContainerDir(plan) = %q, %v, %v", rel, ok, err)
	}
	if _, ok, _ := ContainerDir(step); ok {
		t.Error("a step has no child container")
	}
}
