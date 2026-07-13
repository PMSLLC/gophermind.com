package phaseflow

import (
	"os"
	"testing"
)

// completePlanRoadmap has real phases/plans with no placeholders.
const completePlanRoadmap = `# Roadmap: Real Project

## Phases

- [ ] **Phase 1: Foundation** - base layer

## Phase Details

### Phase 1: Foundation
**Goal**: A working skeleton users can run

Plans:
- [ ] 01-01: scaffold the module
- [ ] 01-02: wire the CLI
`

// writePlan scaffolds a project with the given roadmap and assignments.
func writePlan(t *testing.T, roadmap string, assign Assignments, seed bool) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(PlanningDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(RoadmapPath(root), []byte(roadmap), 0o644); err != nil {
		t.Fatal(err)
	}
	if seed {
		if _, err := SeedCatalog(root); err != nil {
			t.Fatal(err)
		}
	}
	if len(assign.Tasks) > 0 {
		if err := assign.Save(root); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func fullAssignments() Assignments {
	return Assignments{Tasks: []Task{
		{ID: "01-01", Phase: "1", Title: "scaffold", AcceptanceCriteria: []string{"builds"}, Agent: "coder", Model: "strong", Status: StatusPending},
		{ID: "01-02", Phase: "1", Title: "cli", AcceptanceCriteria: []string{"runs"}, Agent: "coder", Model: "speed", Status: StatusPending},
	}}
}

func TestValidatePlanComplete(t *testing.T) {
	// Seed a catalog that includes "coder" so the agent is known.
	root := t.TempDir()
	_ = os.MkdirAll(PlanningDir(root), 0o755)
	_ = os.WriteFile(RoadmapPath(root), []byte(completePlanRoadmap), 0o644)
	_, _ = SeedCatalog(root)
	// Add a "coder" agent to the seeded catalog.
	_ = os.WriteFile(CatalogDir(root)+"/coder.prompt.md", []byte("---\nname: coder\ndefault_model: strong\n---\nwrite code"), 0o644)
	_ = fullAssignments().Save(root)

	rep, err := New(root).ValidatePlan()
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !rep.Complete {
		t.Errorf("expected complete plan, issues: %v", rep.Issues)
	}
	if rep.Phases != 1 || rep.Tasks != 2 {
		t.Errorf("counts wrong: phases=%d tasks=%d", rep.Phases, rep.Tasks)
	}
}

func TestValidatePlanPlaceholders(t *testing.T) {
	root := writePlan(t, starterRoadmap("X"), Assignments{}, false) // starter has [placeholders]
	rep, err := New(root).ValidatePlan()
	if err != nil {
		t.Fatal(err)
	}
	if rep.Complete {
		t.Error("starter roadmap with placeholders should not be complete")
	}
	if len(rep.Issues) == 0 {
		t.Error("expected placeholder/assignment issues")
	}
}

func TestValidatePlanMissingCriteriaAndAgent(t *testing.T) {
	bad := Assignments{Tasks: []Task{
		{ID: "01-01", Phase: "1", Title: "scaffold", Agent: "", Model: "", Status: StatusPending}, // no criteria/agent/model
		// 01-02 has no assignment at all
	}}
	root := writePlan(t, completePlanRoadmap, bad, false)
	rep, _ := New(root).ValidatePlan()
	if rep.Complete {
		t.Error("plan with missing criteria/agent/model should be incomplete")
	}
	joined := ""
	for _, i := range rep.Issues {
		joined += i + "\n"
	}
	for _, want := range []string{"01-01 has no acceptance criteria", "01-01 has no agent", "01-01 has no model", "01-02 has no assignment"} {
		if !contains(joined, want) {
			t.Errorf("missing issue %q in:\n%s", want, joined)
		}
	}
}

func TestValidatePlanUnknownAgent(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(CatalogDir(root), 0o755)
	_ = os.WriteFile(RoadmapPath(root), []byte(completePlanRoadmap), 0o644)
	if err := os.WriteFile(CatalogDir(root)+"/coder.prompt.md", []byte("---\nname: coder\n---\nx"), 0o644); err != nil {
		t.Fatal(err)
	}
	// assignment references an agent not in the catalog
	_ = Assignments{Tasks: []Task{
		{ID: "01-01", Phase: "1", AcceptanceCriteria: []string{"c"}, Agent: "coder", Model: "strong"},
		{ID: "01-02", Phase: "1", AcceptanceCriteria: []string{"c"}, Agent: "nonexistent", Model: "strong"},
	}}.Save(root)
	rep, _ := New(root).ValidatePlan()
	if rep.Complete {
		t.Error("unknown agent should make the plan incomplete")
	}
}

func TestApprovalMarker(t *testing.T) {
	e := New(t.TempDir())
	if e.Approved() {
		t.Error("fresh project should not be approved")
	}
	if err := e.Approve(); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if !e.Approved() {
		t.Error("should be approved after Approve()")
	}
	if err := e.Unapprove(); err != nil {
		t.Fatalf("unapprove: %v", err)
	}
	if e.Approved() {
		t.Error("should not be approved after Unapprove()")
	}
	// Unapprove is idempotent.
	if err := e.Unapprove(); err != nil {
		t.Errorf("second unapprove should be a no-op, got %v", err)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
