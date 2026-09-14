package tui

import (
	"strings"
	"testing"
)

// TestParseInterviewStepBareObject covers the happy path: the model returns
// exactly the object it was asked for.
func TestParseInterviewStepBareObject(t *testing.T) {
	step, err := parseInterviewStep(`{"question":"What are we building?","why":"scope","done":false}`)
	if err != nil {
		t.Fatalf("parseInterviewStep: %v", err)
	}
	if step.Question != "What are we building?" {
		t.Errorf("Question = %q", step.Question)
	}
	if step.Done {
		t.Error("Done = true, want false")
	}
}

// TestParseInterviewStepWrappedInProse is the case that matters for weak local
// models: they narrate around the JSON instead of returning it bare.
func TestParseInterviewStepWrappedInProse(t *testing.T) {
	reply := "Sure! Here is my next question:\n\n```json\n" +
		`{"question":"Who are the users?","why":"audience","done":false}` +
		"\n```\n\nLet me know."
	step, err := parseInterviewStep(reply)
	if err != nil {
		t.Fatalf("parseInterviewStep: %v", err)
	}
	if step.Question != "Who are the users?" {
		t.Errorf("Question = %q", step.Question)
	}
}

// TestParseInterviewStepNestedBraces makes sure the extractor balances braces
// rather than stopping at the first "}".
func TestParseInterviewStepNestedBraces(t *testing.T) {
	reply := `noise {"question":"Pick one: {a, b}","why":"x","done":false} trailing`
	step, err := parseInterviewStep(reply)
	if err != nil {
		t.Fatalf("parseInterviewStep: %v", err)
	}
	if !strings.Contains(step.Question, "{a, b}") {
		t.Errorf("Question = %q, want the nested braces preserved", step.Question)
	}
}

// TestParseInterviewStepDone recognizes the terminating object.
func TestParseInterviewStepDone(t *testing.T) {
	step, err := parseInterviewStep(`{"done":true}`)
	if err != nil {
		t.Fatalf("parseInterviewStep: %v", err)
	}
	if !step.Done {
		t.Error("Done = false, want true")
	}
}

// TestParseInterviewStepRejectsGarbage: no JSON at all is an error, so the
// caller can retry once before falling back.
func TestParseInterviewStepRejectsGarbage(t *testing.T) {
	if _, err := parseInterviewStep("I have no idea what you mean."); err == nil {
		t.Error("expected an error for a reply containing no JSON object")
	}
}

// TestParseInterviewStepRejectsEmptyQuestion: an object that is neither done
// nor carrying a question would stall the interview.
func TestParseInterviewStepRejectsEmptyQuestion(t *testing.T) {
	if _, err := parseInterviewStep(`{"question":"   ","done":false}`); err == nil {
		t.Error("expected an error for a blank question with done=false")
	}
}

// TestInterviewTranscriptAccumulates: answers must survive across rounds, since
// they are what the generation step is built from.
func TestInterviewTranscriptAccumulates(t *testing.T) {
	var tr interviewTranscript
	tr.add("What are we building?", "A CLI todo app.")
	tr.add("Who are the users?", "Just me.")

	out := tr.String()
	for _, want := range []string{"What are we building?", "A CLI todo app.", "Who are the users?", "Just me."} {
		if !strings.Contains(out, want) {
			t.Errorf("transcript missing %q:\n%s", want, out)
		}
	}
	if tr.count() != 2 {
		t.Errorf("count = %d, want 2", tr.count())
	}
	// Order matters: the spec should read in the order asked.
	if strings.Index(out, "What are we building?") > strings.Index(out, "Who are the users?") {
		t.Error("transcript is out of order")
	}
}

// TestInterviewStepPromptDemandsOneQuestion guards the instruction itself: the
// prompt must ask for a single question and the JSON shape, and must not carry
// the old "a FEW at a time" wording.
func TestInterviewStepPromptDemandsOneQuestion(t *testing.T) {
	p := interviewStepPrompt("myproj", interviewTranscript{}, "")
	low := strings.ToLower(p)
	if strings.Contains(low, "few at a time") {
		t.Error("prompt still asks for a few questions at a time")
	}
	if !strings.Contains(low, "exactly one") {
		t.Error("prompt does not demand exactly one question")
	}
	for _, key := range []string{"question", "done"} {
		if !strings.Contains(p, key) {
			t.Errorf("prompt does not describe the %q field", key)
		}
	}
	if !strings.Contains(p, "myproj") {
		t.Error("prompt does not name the project")
	}
}

// TestInterviewStepPromptIncludesPriorAnswers: later questions must be informed
// by what was already answered, or the model repeats itself.
func TestInterviewStepPromptIncludesPriorAnswers(t *testing.T) {
	var tr interviewTranscript
	tr.add("What are we building?", "A CLI todo app.")

	p := interviewStepPrompt("myproj", tr, "")
	if !strings.Contains(p, "A CLI todo app.") {
		t.Errorf("prompt omits prior answers:\n%s", p)
	}
}

// TestInterviewAsksForTestCommand: the executor gate needs a test command, so
// the interview must require it before finishing.
func TestInterviewAsksForTestCommand(t *testing.T) {
	p := interviewStepPrompt("myproj", interviewTranscript{}, "")
	if !strings.Contains(strings.ToLower(p), "test command") {
		t.Errorf("prompt never requires the test command:\n%s", p)
	}
}

// TestInterviewPromptCarriesContext: the digest must reach the model, or
// nothing can be prefilled.
func TestInterviewPromptCarriesContext(t *testing.T) {
	p := interviewStepPrompt("myproj", interviewTranscript{}, "### Existing spec\nCTX-MARKER")
	if !strings.Contains(p, "CTX-MARKER") {
		t.Error("repository context missing from the prompt")
	}
	if !strings.Contains(p, "suggested") {
		t.Error("prompt does not ask for a suggested answer")
	}
}

// TestInterviewPromptOmitsEmptyContext keeps a fresh repo's prompt clean.
func TestInterviewPromptOmitsEmptyContext(t *testing.T) {
	p := interviewStepPrompt("myproj", interviewTranscript{}, "")
	if strings.Contains(p, "already records about itself") {
		t.Error("context heading emitted with no context")
	}
}

// TestParseInterviewStepReadsSuggested covers the new field.
func TestParseInterviewStepReadsSuggested(t *testing.T) {
	step, err := parseInterviewStep(`{"question":"Test command?","suggested":"go test ./...","done":false}`)
	if err != nil {
		t.Fatalf("parseInterviewStep: %v", err)
	}
	if step.Suggested != "go test ./..." {
		t.Errorf("Suggested = %q", step.Suggested)
	}
}
