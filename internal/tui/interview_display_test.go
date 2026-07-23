package tui

import (
	"strings"
	"testing"
)

const rawStep = `{"question":"What are the core features?","why":"To understand scope.","done":false}`

// interviewModel returns a model mid-interview with an in-flight interview turn.
func interviewModel(t *testing.T) model {
	t.Helper()
	m := testModel(t)
	m.projName = "build"
	m.proj = projInterview
	m.projTurn = true
	return m
}

// TestInterviewJSONIsNotShownToTheUser: the JSON is a wire format between
// gophermind and the model. Only the rendered question belongs in the
// transcript.
func TestInterviewJSONIsNotShownToTheUser(t *testing.T) {
	m := interviewModel(t)

	// The reply streams in as tokens, then completes.
	updated, _ := m.Update(tokenMsg(rawStep))
	m = updated.(model)
	updated, _ = m.Update(doneMsg{answer: rawStep})
	m = updated.(model)

	if strings.Contains(m.content, `"question"`) {
		t.Errorf("raw JSON leaked into the transcript:\n%s", m.content)
	}
	if strings.Contains(m.content, `"done":false`) {
		t.Errorf("raw JSON leaked into the transcript:\n%s", m.content)
	}
	if !strings.Contains(m.content, "What are the core features?") {
		t.Errorf("the parsed question is missing:\n%s", m.content)
	}
}

// TestInterviewStreamBufferStaysEmpty: suppressing only at commit time would
// still flash the JSON in the live view while it streams.
func TestInterviewStreamBufferStaysEmpty(t *testing.T) {
	m := interviewModel(t)

	updated, _ := m.Update(tokenMsg(`{"question":"Who are`))
	m = updated.(model)

	if m.stream != "" {
		t.Errorf("stream buffer = %q, want empty during an interview turn", m.stream)
	}
	if strings.Contains(m.View(), `"question"`) {
		t.Error("partial JSON is visible in the live view")
	}
}

// TestNonInterviewTurnsStillStream guards the blast radius: ordinary turns, and
// the /project generation turn, must still show their prose.
func TestNonInterviewTurnsStillStream(t *testing.T) {
	m := testModel(t) // ordinary turn: no project flow active
	updated, _ := m.Update(tokenMsg("hello "))
	m = updated.(model)
	updated, _ = m.Update(tokenMsg("world"))
	m = updated.(model)

	if m.stream != "hello world" {
		t.Errorf("stream = %q, want the streamed prose", m.stream)
	}

	gen := testModel(t)
	gen.proj = projGenerating
	gen.projTurn = true
	updated, _ = gen.Update(tokenMsg("writing the plan"))
	gen = updated.(model)
	if gen.stream != "writing the plan" {
		t.Errorf("generation stream = %q, want the prose shown", gen.stream)
	}
}

// TestUnparseableInterviewReplyIsStillShown: when the model does not return
// JSON, the fallback path surfaces the raw reply so the user is never left
// staring at a blank screen.
func TestUnparseableInterviewReplyIsStillShown(t *testing.T) {
	m := interviewModel(t)
	m.projParseRetry = true // the reformat retry has already been spent

	updated, _ := m.Update(doneMsg{answer: "I have no idea what you mean."})
	m = updated.(model)

	if !strings.Contains(m.content, "I have no idea what you mean.") {
		t.Errorf("unparseable reply was swallowed:\n%s", m.content)
	}
}
