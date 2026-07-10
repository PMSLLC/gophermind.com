package agent

import (
	"context"
	"testing"

	"gophermind/internal/llm"
)

func TestParseVerdict(t *testing.T) {
	// satisfied
	ok, fb := parseVerdict(llm.Message{ToolCalls: []llm.ToolCall{{
		Function: llm.FunctionCall{Name: verifierToolName, Arguments: `{"satisfied":true}`},
	}}})
	if !ok || fb != "" {
		t.Errorf("satisfied verdict = (%v,%q)", ok, fb)
	}
	// not satisfied with feedback
	ok, fb = parseVerdict(llm.Message{ToolCalls: []llm.ToolCall{{
		Function: llm.FunctionCall{Name: verifierToolName, Arguments: `{"satisfied":false,"feedback":"missing tests"}`},
	}}})
	if ok || fb != "missing tests" {
		t.Errorf("unsatisfied verdict = (%v,%q)", ok, fb)
	}
	// no tool call -> lenient accept (don't block on an uncooperative verifier)
	if ok, _ := parseVerdict(llm.Message{Content: "looks fine"}); !ok {
		t.Error("no tool call should default to accept")
	}
}

func TestSendWithVerificationCorrects(t *testing.T) {
	sp := &scriptedProvider{responses: []string{
		finalResp("first attempt"), // Send #1
		toolCallResp("v1", verifierToolName, `{"satisfied":false,"feedback":"add error handling"}`), // verifier
		finalResp("corrected attempt"), // correction Send
	}}
	a := newTestAgent(t, sp, t.TempDir())

	var sawFlag bool
	a.onEvent = func(e Event) {
		if e.Type == "assistant" && contains(e.Text, "Verifier") {
			sawFlag = true
		}
	}

	out, err := a.SendWithVerification(context.Background(), "do the task")
	if err != nil {
		t.Fatal(err)
	}
	if out != "corrected attempt" {
		t.Errorf("final = %q, want corrected attempt", out)
	}
	if !sawFlag {
		t.Error("verifier feedback event not emitted")
	}
}

func TestSendWithVerificationAcceptsGoodAnswer(t *testing.T) {
	sp := &scriptedProvider{responses: []string{
		finalResp("great answer"),                                  // Send #1
		toolCallResp("v1", verifierToolName, `{"satisfied":true}`), // verifier accepts
		finalResp("SHOULD NOT BE REACHED"),                         // would be a correction round
	}}
	a := newTestAgent(t, sp, t.TempDir())

	out, err := a.SendWithVerification(context.Background(), "do the task")
	if err != nil {
		t.Fatal(err)
	}
	if out != "great answer" {
		t.Errorf("final = %q, want great answer (no correction round)", out)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
