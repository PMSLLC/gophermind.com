package planner

import (
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/contract"
)

func TestGroupSummaryCountsNotApplicableAnswersAndBoundaries(t *testing.T) {
	na := map[string]any{"not_applicable": "this group has nothing to say here at all"}
	c := &contract.Contracts{Components: []contract.Component{{ID: "greeting"}}}
	dec := decomposed{Components: map[string][]map[string]any{"greeting": {
		{"id": "fn-a", "security": map[string]any{"trust_boundary": "external_input"}, "alternatives": na, "performance": na, "open_questions": []any{}},
		{"id": "fn-b", "security": map[string]any{"trust_boundary": "none"}, "alternatives": na, "open_questions": []any{}},
		{"id": "fn-c", "security": na, "open_questions": []any{"Which store?"}},
	}}}
	est := enrichedState{Root: map[string]any{}, Warnings: []string{"fn-b: 1 type name(s) in the signature are not declared in the contract"}}
	cov := CoverageFile{Covered: []Covered{{Requirement: "F1", Nodes: []string{"fn-a"}}}}
	var b strings.Builder
	writeGroupSummary(&b, c, dec, est, cov)
	out := b.String()
	for _, want := range []string{
		"## Node groups", "Function nodes: 3",
		"| alternatives | 2 |", "| performance | 1 |", "| security | 1 |",
		"- greeting: ", "external_input 1", "none 1",
		"Nodes that no requirement names: fn-b, fn-c",
		"fn-b: 1 type name(s)",
		"Open questions: fn-c",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary lacks %q:\n%s", want, out)
		}
	}
}

func TestGroupSummaryWithNothingToReport(t *testing.T) {
	var b strings.Builder
	writeGroupSummary(&b, &contract.Contracts{}, decomposed{}, enrichedState{}, CoverageFile{})
	for _, want := range []string{"Function nodes: 0", "Nodes that no requirement names: none", "Open questions: none"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("summary lacks %q:\n%s", want, b.String())
		}
	}
}

func TestGroupSummaryShowsBoundariesPerComponentAndStructureAssumptions(t *testing.T) {
	c := &contract.Contracts{Components: []contract.Component{{ID: "alpha"}, {ID: "beta"}}}
	sec := func(b string) map[string]any { return map[string]any{"trust_boundary": b} }
	dec := decomposed{Components: map[string][]map[string]any{
		"alpha": {{"id": "a1", "security": sec("both")}},
		"beta":  {{"id": "b1", "security": sec("none")}, {"id": "b2", "security": sec("not-a-boundary")}},
	}}
	long := strings.Repeat("word ", 200)
	est := enrichedState{
		Components: map[string]map[string]any{"alpha": {"assumptions": []any{"alpha assumes\nthe store is local"}}, "beta": {"assumptions": []any{long}}},
		Root:       map[string]any{"assumptions": []any{"root assumes one process"}},
	}
	var b strings.Builder
	writeGroupSummary(&b, c, dec, est, CoverageFile{})
	out := b.String()
	for _, want := range []string{
		"- alpha: none 0, internal 0, external_input 0, external_output 0, both 1, other 0",
		"- beta: none 1, internal 0, external_input 0, external_output 0, both 0, other 1",
		"Component assumptions:", "- alpha: alpha assumes the store is local",
		"Root assumptions:", "- root assumes one process",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, long) || strings.Contains(out, "alpha assumes\n") {
		t.Errorf("assumption text is not bounded and on one line:\n%s", out)
	}
}

func TestChecksSkippedNamesTheGoModChecks(t *testing.T) {
	if got := checksSkipped(factsFile{OS: "darwin"}); !strings.Contains(got, "Checks skipped: ") || !strings.Contains(got, "go_min") || !strings.Contains(got, "no go.mod") || strings.Contains(got, "none") {
		t.Errorf("no go.mod: %q", got)
	}
	if got := checksSkipped(factsFile{}); !strings.Contains(got, "go_min") || !strings.Contains(got, "facts unavailable") || strings.Contains(got, "go.mod)") {
		t.Errorf("no facts: %q", got)
	}
	if got := checksSkipped(factsFile{OS: "darwin", GoVersion: "1.22"}); got != "Checks skipped: none." {
		t.Errorf("with go.mod: %q", got)
	}
}

func TestGroupSummaryWhenEnrichHasNotRun(t *testing.T) {
	c := &contract.Contracts{Components: []contract.Component{{ID: "alpha"}}}
	var b strings.Builder
	writeGroupSummary(&b, c, decomposed{}, enrichedState{}, CoverageFile{})
	out := b.String()
	if !strings.Contains(out, "Enrich has not run for this plan.") {
		t.Errorf("lacks the not-run line:\n%s", out)
	}
	for _, bad := range []string{"Enrich warnings:", "Component assumptions:", "Root assumptions:"} {
		if strings.Contains(out, bad) {
			t.Errorf("not-run summary prints %q:\n%s", bad, out)
		}
	}
	if !strings.Contains(out, "Open questions: none") || !strings.Contains(out, "Trust boundaries per component") {
		t.Errorf("other sections missing:\n%s", out)
	}
}

func TestGroupSummaryCleanEnrichPassStillPrintsNone(t *testing.T) {
	c := &contract.Contracts{Components: []contract.Component{{ID: "alpha"}}}
	est := enrichedState{Components: map[string]map[string]any{"alpha": {"assumptions": []any{}}}, Root: map[string]any{"assumptions": []any{}}}
	var b strings.Builder
	writeGroupSummary(&b, c, decomposed{}, est, CoverageFile{})
	out := b.String()
	for _, want := range []string{"Enrich warnings:\n\nNone.", "Component assumptions:\n\nNone.", "Root assumptions:\n\nNone."} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "has not run") {
		t.Errorf("clean pass reported as not run:\n%s", out)
	}
}
