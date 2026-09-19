package plan

import (
	"fmt"
	"strings"
	"testing"

	"gophermind/gophermind-lib/plantree"
)

func node(t *testing.T, id, title, digest, objective string) plantree.Node {
	t.Helper()
	n, err := newSkeleton(id, title, digest, objective)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPass2PromptCarriesOneTaskAndItsContext(t *testing.T) {
	phase := node(t, "phase-001", "Foundation", "the base of everything", "set up the base")
	task := node(t, "phase-001.task-001", "Repo layout", "where code lives", "")
	st1 := node(t, s1, "Create module", "needed to compile", "")
	st2 := node(t, s2, "Add CI", "catch breakage", "")
	p := Pass2Prompt("demo", "an overview", phase, task, []plantree.Node{st1, st2}, []plantree.Node{st2}, "[part 1 of 1]\nthe brief text")
	for _, want := range []string{
		`"demo"`, "an overview", "Phase: Foundation", "the base of everything", "Task: Repo layout", "where code lives",
		"- " + s1 + ": Create module", "Steps to specify now:", "- " + s2 + ": Add CI. Why: catch breakage",
		"<<<BRIEF EXCERPTS", "the brief text", "EARLIER step", `"depends_on"`, "ONE JSON object",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
	section := p[strings.Index(p, "Steps to specify now:"):strings.Index(p, "Brief excerpts")]
	if strings.Contains(section, s1) {
		t.Error("the steps to specify now must list only the batch")
	}
}

func TestPass2PromptWithoutExcerptsSaysSo(t *testing.T) {
	phase := node(t, "phase-001", "P", "d", "")
	task := node(t, "phase-001.task-001", "T", "d", "")
	st := node(t, s1, "S", "d", "")
	p := Pass2Prompt("demo", "", phase, task, []plantree.Node{st}, []plantree.Node{st}, "")
	if !strings.Contains(p, "(not available)") || strings.Contains(p, "<<<BRIEF EXCERPTS") {
		t.Errorf("no excerpts: %q", p[strings.Index(p, "Brief excerpts"):])
	}
}

func TestPass2PromptBoundsTheStepList(t *testing.T) {
	phase := node(t, "phase-001", "P", "d", "")
	task := node(t, "phase-001.task-001", "T", "d", "")
	var steps []plantree.Node
	for i := 1; i <= 100; i++ {
		steps = append(steps, node(t, fmt.Sprintf("phase-001.task-001.step-%03d", i), strings.Repeat("a long step title ", 10), "d", ""))
	}
	p := Pass2Prompt("demo", "", phase, task, steps, steps[:1], "")
	if !strings.Contains(p, "more steps not shown") {
		t.Error("an oversize step list must say how many steps it left out")
	}
	list := p[strings.Index(p, "in order."):strings.Index(p, "Steps to specify now:")]
	if len(list) > siblingListCapBytes+200 {
		t.Errorf("the step list is %d bytes, cap %d", len(list), siblingListCapBytes)
	}
}

// TestPass2PromptWorstCaseSize pins the largest prompt one pass can produce
// with the default caps, so a change that lets it grow shows up here.
func TestPass2PromptWorstCaseSize(t *testing.T) {
	phase := node(t, "phase-001", strings.Repeat("p", 200), strings.Repeat("d", 500), strings.Repeat("o", 1000))
	task := node(t, "phase-001.task-001", strings.Repeat("t", 200), strings.Repeat("d", 500), strings.Repeat("o", 1000))
	var steps []plantree.Node
	for i := 1; i <= 100; i++ {
		steps = append(steps, node(t, fmt.Sprintf("phase-001.task-001.step-%03d", i), strings.Repeat("s", 200), strings.Repeat("d", 500), ""))
	}
	chunks := []Chunk{{Index: 0, Text: strings.Repeat("brief text line\n", 2000)}}
	excerpts := Excerpts(chunks, []int{0}, defaultBriefBytes)
	overview := FitOverview(strings.Repeat("o", 20000), OverviewCapBytes)
	p := Pass2Prompt(strings.Repeat("n", 100), overview, phase, task, steps, steps[:defaultStepsPerPass], excerpts)
	t.Logf("worst-case pass-2 prompt: %d bytes", len(p))
	if len(p) > 26000 {
		t.Errorf("worst-case pass-2 prompt is %d bytes, want at most 26000", len(p))
	}
}
