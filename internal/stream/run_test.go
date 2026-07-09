package stream

import (
	"context"
	"strings"
	"testing"

	"gophermind/internal/agent"
)

// fakeSession simulates an agent: on Send it emits one tool round-trip through
// the encoder (mimicking the agent's onEvent → Encoder.Handle wiring) and
// returns a canned answer.
type fakeSession struct {
	enc     *Encoder
	answer  string
	inputs  []string
}

func (f *fakeSession) Send(_ context.Context, input string) (string, error) {
	f.inputs = append(f.inputs, input)
	_ = f.enc.Handle(agent.Event{Type: "tool_call", Name: "read_file", Text: `{"path":"x"}`})
	_ = f.enc.Handle(agent.Event{Type: "tool_result", Name: "read_file", Text: "data"})
	return f.answer, nil
}

func TestRunTextEmitsInitToolAndResult(t *testing.T) {
	var b strings.Builder
	enc := NewEncoder(&b, "s")
	sess := &fakeSession{enc: enc, answer: "done"}
	err := Run(context.Background(), enc, sess, Options{
		InputFormat: "text", Prompt: "hello", Model: "qwen", Tools: []string{"read_file"}, Cwd: "/r",
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := decodeLines(t, b.String())
	types := make([]string, len(lines))
	for i, m := range lines {
		types[i] = m["type"].(string)
	}
	want := []string{"system", "assistant", "user", "result"}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Errorf("line types = %v, want %v", types, want)
	}
	if sess.inputs[0] != "hello" {
		t.Errorf("prompt not forwarded: %v", sess.inputs)
	}
	if lines[3]["result"] != "done" {
		t.Errorf("result text = %v", lines[3]["result"])
	}
}

func TestRunStreamJSONInputOneTurnPerLine(t *testing.T) {
	var b strings.Builder
	enc := NewEncoder(&b, "s")
	sess := &fakeSession{enc: enc, answer: "ok"}
	in := `{"type":"user","message":{"role":"user","content":"first"}}` + "\n" +
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"second"}]}}` + "\n"
	err := Run(context.Background(), enc, sess, Options{InputFormat: "stream-json", In: strings.NewReader(in)})
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.inputs) != 2 || sess.inputs[0] != "first" || sess.inputs[1] != "second" {
		t.Fatalf("inputs = %v, want [first second] (string and block-array forms)", sess.inputs)
	}
	// Two result lines, one per turn.
	results := 0
	for _, m := range decodeLines(t, b.String()) {
		if m["type"] == "result" {
			results++
		}
	}
	if results != 2 {
		t.Errorf("result lines = %d, want 2", results)
	}
}
