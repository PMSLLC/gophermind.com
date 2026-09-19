package plan

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

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
	p := Pass2Prompt(Pass2Input{Project: "demo", Overview: "an overview", Excerpts: "[part 1 of 1]\nthe brief text", Phase: phase, Task: task, Siblings: []plantree.Node{st1, st2}, Batch: []plantree.Node{st2}})
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
	p := Pass2Prompt(Pass2Input{Project: "demo", Overview: "", Excerpts: "", Phase: phase, Task: task, Siblings: []plantree.Node{st}, Batch: []plantree.Node{st}})
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
	p := Pass2Prompt(Pass2Input{Project: "demo", Overview: "", Excerpts: "", Phase: phase, Task: task, Siblings: steps, Batch: steps[:1]})
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
	p := Pass2Prompt(Pass2Input{Project: strings.Repeat("n", 100), Overview: overview, Facts: strings.Repeat("f", FactsCapBytes), Excerpts: excerpts, Phase: phase, Task: task, Siblings: steps, Batch: steps[:defaultStepsPerPass]})
	t.Logf("worst-case pass-2 prompt: %d bytes", len(p))
	if len(p) > 26000 {
		t.Errorf("worst-case pass-2 prompt is %d bytes, want at most 26000", len(p))
	}
}

func TestPass2PromptWorstCaseSizeWithMultibyteText(t *testing.T) {
	r := func(n int) string { return strings.Repeat("\U0001D11E", n) }
	phase := node(t, "phase-001", r(200), r(500), r(1000))
	task := node(t, "phase-001.task-001", r(200), r(500), r(1000))
	var steps []plantree.Node
	for i := 1; i <= 100; i++ {
		steps = append(steps, node(t, fmt.Sprintf("phase-001.task-001.step-%03d", i), r(200), r(500), ""))
	}
	chunks := []Chunk{{Index: 0, Text: strings.Repeat("brief text line\n", 2000)}}
	excerpts := Excerpts(chunks, []int{0}, defaultBriefBytes)
	overview := FitOverview(strings.Repeat("o", 20000), OverviewCapBytes)
	p := Pass2Prompt(Pass2Input{Project: r(100), Overview: overview, Facts: r(FactsCapBytes / 4), Excerpts: excerpts, Phase: phase, Task: task, Siblings: steps, Batch: steps[:defaultStepsPerPass]})
	t.Logf("multibyte worst-case pass-2 prompt: %d bytes", len(p))
	if len(p) > 26000 {
		t.Errorf("multibyte worst-case pass-2 prompt is %d bytes, want at most 26000", len(p))
	}
	if !utf8.ValidString(p) {
		t.Error("the prompt is not valid UTF-8")
	}
}

func TestPass2PromptTellsTheModelWhatTheParserEnforces(t *testing.T) {
	phase := node(t, "phase-001", "P", "d", "")
	task := node(t, "phase-001.task-001", "T", "d", "")
	st := node(t, s1, "S", "d", "")
	p := Pass2Prompt(Pass2Input{Project: "demo", Overview: "", Excerpts: "", Phase: phase, Task: task, Siblings: []plantree.Node{st}, Batch: []plantree.Node{st}})
	for _, want := range []string{"2000 characters", "300 characters", "never put an empty string", "Do not depend on a step marked on hold"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
	if strings.Contains(p, `[""]`) {
		t.Error("the shape example must not contain an empty string")
	}
	found := false
	for _, line := range strings.Split(p, "\n") {
		if strings.HasPrefix(line, `{"steps"`) {
			found = true
			if !json.Valid([]byte(line)) {
				t.Errorf("the shape line is not valid JSON: %s", line)
			}
		}
	}
	if !found {
		t.Error("no shape line found")
	}
}

func TestPass2PromptTagsEachSiblingByStageAndHold(t *testing.T) {
	phase := node(t, "phase-001", "P", "d", "")
	task := node(t, "phase-001.task-001", "T", "d", "")
	skipped := node(t, "phase-001.task-001.step-001", "One", "d", "")
	skipped.Status = plantree.StatusSkipped
	drafted := node(t, "phase-001.task-001.step-002", "Two", "d", "")
	drafted.Planning.Stage = plantree.StageDrafted
	skel := node(t, "phase-001.task-001.step-003", "Three", "d", "")
	p := Pass2Prompt(Pass2Input{Project: "demo", Overview: "", Excerpts: "", Phase: phase, Task: task, Siblings: []plantree.Node{skipped, drafted, skel}, Batch: []plantree.Node{skel}})
	for _, want := range []string{
		"step-001: One [on hold: skipped]", "step-002: Two [specified]", "step-003: Three [to specify]",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
}

func TestPass2PromptShowsFactsOrSaysNone(t *testing.T) {
	phase := node(t, "phase-001", "P", "d", "")
	task := node(t, "phase-001.task-001", "T", "d", "")
	st := node(t, s1, "S", "d", "")
	base := Pass2Input{Project: "demo", Phase: phase, Task: task, Siblings: []plantree.Node{st}, Batch: []plantree.Node{st}}

	without := Pass2Prompt(base)
	if !strings.Contains(without, "Repository facts") || !strings.Contains(without, "(not provided)") {
		t.Error("with no facts the prompt must say so")
	}
	if !strings.Contains(without, "rather than guessing") {
		t.Error("the prompt must tell the model not to guess a test command")
	}

	base.Facts = "Go 1.25. Test: go test ./..."
	with := Pass2Prompt(base)
	if !strings.Contains(with, "Go 1.25. Test: go test ./...") || strings.Contains(with, "(not provided)") {
		t.Error("the facts must appear and replace the placeholder")
	}

	base.Facts = strings.Repeat("f", 10*FactsCapBytes)
	if got := Pass2Prompt(base); len(got) > len(with)+FactsCapBytes+200 {
		t.Errorf("oversize facts must be cut to FactsCapBytes, prompt grew to %d", len(got))
	}
}
