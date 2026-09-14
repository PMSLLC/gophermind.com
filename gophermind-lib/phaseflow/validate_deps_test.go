package phaseflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// planFixture writes a minimal roadmap plus the given assignments JSON.
func planFixture(t *testing.T, assignments string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, PlanningDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	roadmap := "# Roadmap\n\n## Phase 1: Build\n\n**Goal:** ship it\n\n**Success Criteria:** it ships\n\n### Plans\n\n- 01-01: contract\n- 01-02: build\n"
	if err := os.WriteFile(filepath.Join(dir, "ROADMAP.md"), []byte(roadmap), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assignments.json"), []byte(assignments), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func task(id, deps string, contract bool) string {
	d := ""
	if deps != "" {
		parts := strings.Split(deps, ",")
		for i := range parts {
			parts[i] = `"` + parts[i] + `"`
		}
		d = `,"depends_on":[` + strings.Join(parts, ",") + `]`
	}
	c := ""
	if contract {
		c = `,"is_contract":true`
	}
	return `{"id":"` + id + `","phase":"1","title":"t","description":"d",` +
		`"acceptance_criteria":["test x fails before, passes after"],` +
		`"agent":"coder","model":"speed","status":"pending"` + d + c + `}`
}

// A dependency naming a task that does not exist is a typo in the plan, and it
// would otherwise surface only when the run started: AssignWaves rejects it,
// which fails the whole execution after the user already approved the plan.
// Catching it at validation is the difference between fixing a typo and
// debugging a failed run.
func TestValidateRejectsAnUnknownDependency(t *testing.T) {
	root := planFixture(t, `{"tasks":[`+task("01-01", "", true)+`,`+task("01-02", "01-99", false)+`]}`)
	rep, err := New(root).ValidatePlan()
	if err != nil {
		t.Fatal(err)
	}
	if rep.Complete {
		t.Fatal("a plan with an unknown dependency was reported complete")
	}
	if !hasIssueContaining(rep.Issues, "01-99") {
		t.Errorf("no issue names the unknown dependency: %v", rep.Issues)
	}
}

// A cycle can never be scheduled, so it must not reach approval.
func TestValidateRejectsADependencyCycle(t *testing.T) {
	root := planFixture(t, `{"tasks":[`+task("01-01", "01-02", false)+`,`+task("01-02", "01-01", false)+`]}`)
	rep, err := New(root).ValidatePlan()
	if err != nil {
		t.Fatal(err)
	}
	if rep.Complete {
		t.Fatal("a plan with a dependency cycle was reported complete")
	}
	if !hasIssueContaining(rep.Issues, "cycl") {
		t.Errorf("no issue mentions the cycle: %v", rep.Issues)
	}
}

// A task cannot depend on itself.
func TestValidateRejectsASelfDependency(t *testing.T) {
	root := planFixture(t, `{"tasks":[`+task("01-01", "01-01", false)+`,`+task("01-02", "", false)+`]}`)
	rep, _ := New(root).ValidatePlan()
	if rep.Complete {
		t.Fatal("a self-dependency was reported complete")
	}
	// Assert the specific issue, not just that the plan was incomplete: this
	// fixture has other reasons to be incomplete, so "not complete" would
	// pass even with no self-dependency check at all.
	if !hasIssueContaining(rep.Issues, "depends on itself") {
		t.Errorf("no issue names the self-dependency: %v", rep.Issues)
	}
}

// More than one contract task is ambiguous: wave 0 runs alone, before
// everything, and two tasks cannot both be that.
func TestValidateRejectsTwoContractTasks(t *testing.T) {
	root := planFixture(t, `{"tasks":[`+task("01-01", "", true)+`,`+task("01-02", "", true)+`]}`)
	rep, _ := New(root).ValidatePlan()
	if rep.Complete {
		t.Fatal("two contract tasks were reported complete")
	}
	if !hasIssueContaining(rep.Issues, "contract") {
		t.Errorf("no issue mentions the contract: %v", rep.Issues)
	}
}

// A well-formed graph validates, and a plan with no dependencies at all is
// still valid: that is every plan written before dependencies existed.
func TestValidateAcceptsAWellFormedGraph(t *testing.T) {
	root := planFixture(t, `{"tasks":[`+task("01-01", "", true)+`,`+task("01-02", "01-01", false)+`]}`)
	rep, err := New(root).ValidatePlan()
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range rep.Issues {
		if strings.Contains(i, "depend") || strings.Contains(i, "contract") {
			t.Errorf("well-formed graph produced a dependency issue: %q", i)
		}
	}
}

func TestValidateAcceptsAPlanWithNoDependencies(t *testing.T) {
	root := planFixture(t, `{"tasks":[`+task("01-01", "", false)+`,`+task("01-02", "", false)+`]}`)
	rep, _ := New(root).ValidatePlan()
	for _, i := range rep.Issues {
		if strings.Contains(i, "depend") {
			t.Errorf("a plan with no dependencies produced: %q", i)
		}
	}
}

func hasIssueContaining(issues []string, sub string) bool {
	for _, i := range issues {
		if strings.Contains(strings.ToLower(i), strings.ToLower(sub)) {
			return true
		}
	}
	return false
}
