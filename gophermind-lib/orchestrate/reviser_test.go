package orchestrate

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/llm"
	"gophermind/gophermind-lib/phaseflow"
)

// This file covers LLMReviser (pipeline piece 4, task 3): the prompt must
// carry every attempt's model and reason, not just the latest one, and the
// model is asked what they have in common - the difference between a
// revision that addresses the shared pattern and a generic "try again". No
// test here makes a real network call: every server is an httptest.Server
// on loopback, the same pattern internal/llm's own tests use to stub the
// chat-completions endpoint.

func reviseTask() phaseflow.Task {
	return phaseflow.Task{
		ID:                 "01-01",
		Title:              "Trim trailing whitespace",
		Description:        "Write a function that trims trailing whitespace from each line.",
		AcceptanceCriteria: []string{"trailing spaces and tabs are removed", "leading whitespace is preserved"},
	}
}

func reviseAttempts() []phaseflow.Attempt {
	return []phaseflow.Attempt{
		{Model: "model-a", Verdict: "fail", Reason: "did not handle a trailing tab, only trailing spaces"},
		{Model: "model-b", Verdict: "fail", Reason: "stripped leading whitespace too, which the test forbids"},
		{Model: "model-c", Verdict: "fail", Reason: "left a trailing tab on the last line of the file"},
	}
}

// chatRequestBody is the minimal shape reviser_test.go needs from the wire
// request: which messages went out and to which model, so a test can assert
// the attempt history actually reached the prompt.
type chatRequestBody struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

// captureRequest returns an httptest server that stores the last request
// body it received (for prompt assertions) and answers with respBody.
func captureRequest(t *testing.T, respBody string) (*httptest.Server, *chatRequestBody) {
	t.Helper()
	var got chatRequestBody
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &got)
		w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func toolCallResponse(argsJSON string) string {
	args, _ := json.Marshal(argsJSON) // encode as a JSON string literal, escaping quotes inside it
	return `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"` +
		reviseToolName + `","arguments":` + string(args) + `}}]},"finish_reason":"tool_calls"}]}`
}

// TestRevisePromptCarriesEveryAttempt: the prompt sent to the model must
// contain every attempt's model and reason, not just the most recent one -
// that is what makes noticing a shared root cause possible at all.
func TestRevisePromptCarriesEveryAttempt(t *testing.T) {
	argsJSON := `{"note":"all three missed some form of trailing whitespace handling","deliverable":"","test":[]}`
	srv, got := captureRequest(t, toolCallResponse(argsJSON))

	client := llm.New(srv.URL, "", "weak-default", 5*time.Second, false)
	r := NewLLMReviser(client, "strongest-model")

	if _, err := r.Revise(context.Background(), reviseTask(), reviseAttempts()); err != nil {
		t.Fatalf("Revise: %v", err)
	}

	if got.Model != "strongest-model" {
		t.Errorf("request model = %q, want %q (the strongest configured model, not the client's default)", got.Model, "strongest-model")
	}

	var userMsg string
	for _, m := range got.Messages {
		if m.Role == "user" {
			userMsg = m.Content
		}
	}
	if userMsg == "" {
		t.Fatal("no user message sent")
	}

	for _, a := range reviseAttempts() {
		if !strings.Contains(userMsg, a.Model) {
			t.Errorf("prompt missing attempt model %q\nprompt:\n%s", a.Model, userMsg)
		}
		if !strings.Contains(userMsg, a.Reason) {
			t.Errorf("prompt missing attempt reason %q\nprompt:\n%s", a.Reason, userMsg)
		}
	}
	if !strings.Contains(strings.ToLower(userMsg), "have in common") {
		t.Errorf("prompt does not ask what the failures have in common:\n%s", userMsg)
	}
}

// TestReviseClientModelRestoredAfterCall: Revise must not leave the shared
// Client permanently pointed at the strong model - a caller reusing the same
// Client for ordinary task execution must see its own model afterward.
func TestReviseClientModelRestoredAfterCall(t *testing.T) {
	argsJSON := `{"note":"n","deliverable":"d","test":[]}`
	srv, _ := captureRequest(t, toolCallResponse(argsJSON))

	client := llm.New(srv.URL, "", "weak-default", 5*time.Second, false)
	r := NewLLMReviser(client, "strongest-model")

	if _, err := r.Revise(context.Background(), reviseTask(), reviseAttempts()); err != nil {
		t.Fatalf("Revise: %v", err)
	}
	if client.Model != "weak-default" {
		t.Errorf("client.Model after Revise = %q, want restored to %q", client.Model, "weak-default")
	}
}

// TestReviseWellFormedResponseParses: a well-formed tool-call response
// parses into a Revision with Note, Deliverable and Test carried through.
func TestReviseWellFormedResponseParses(t *testing.T) {
	argsJSON := `{"note":"all three missed trailing-whitespace edge cases","deliverable":"trim trailing spaces AND tabs, never leading whitespace","test":["trailing spaces removed","trailing tabs removed","leading whitespace preserved"]}`
	srv, _ := captureRequest(t, toolCallResponse(argsJSON))

	client := llm.New(srv.URL, "", "m", 5*time.Second, false)
	r := NewLLMReviser(client, "m")

	rev, err := r.Revise(context.Background(), reviseTask(), reviseAttempts())
	if err != nil {
		t.Fatalf("Revise: %v", err)
	}
	if rev.Note != "all three missed trailing-whitespace edge cases" {
		t.Errorf("Note = %q", rev.Note)
	}
	if rev.Deliverable != "trim trailing spaces AND tabs, never leading whitespace" {
		t.Errorf("Deliverable = %q", rev.Deliverable)
	}
	if len(rev.Test) != 3 {
		t.Fatalf("Test = %+v, want 3 entries", rev.Test)
	}
}

// TestReviseMalformedResponseIsError: a response with no matching tool call
// (the model answered in plain text instead) must be an error, never a
// silently empty Revision - an empty Revision would be indistinguishable
// from the model genuinely finding nothing to change, and
// phaseflow.ApplyRevision would then reject it as a no-op, masking that the
// reviser never actually answered.
func TestReviseMalformedResponseIsError(t *testing.T) {
	srv, _ := captureRequest(t, `{"choices":[{"message":{"role":"assistant","content":"I think the task is fine as-is."}}]}`)

	client := llm.New(srv.URL, "", "m", 5*time.Second, false)
	r := NewLLMReviser(client, "m")

	_, err := r.Revise(context.Background(), reviseTask(), reviseAttempts())
	if err == nil {
		t.Fatal("Revise with no tool call in the response: want an error, got nil")
	}
}

// TestReviseUnparseableToolArgumentsIsError: a tool call with invalid JSON
// arguments must also be an error, not a zero-value Revision.
func TestReviseUnparseableToolArgumentsIsError(t *testing.T) {
	srv, _ := captureRequest(t, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"`+
		reviseToolName+`","arguments":"{not valid json"}}]},"finish_reason":"tool_calls"}]}`)

	client := llm.New(srv.URL, "", "m", 5*time.Second, false)
	r := NewLLMReviser(client, "m")

	_, err := r.Revise(context.Background(), reviseTask(), reviseAttempts())
	if err == nil {
		t.Fatal("Revise with unparseable tool arguments: want an error, got nil")
	}
}

// TestReviseNilClientIsError: a Reviser with no client must fail clearly
// rather than nil-dereferencing.
func TestReviseNilClientIsError(t *testing.T) {
	r := NewLLMReviser(nil, "m")
	if _, err := r.Revise(context.Background(), reviseTask(), reviseAttempts()); err == nil {
		t.Fatal("Revise with a nil client: want an error, got nil")
	}
}
