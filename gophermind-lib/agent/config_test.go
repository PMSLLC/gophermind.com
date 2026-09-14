package agent

import (
	"testing"

	"gophermind/gophermind-lib/llm"
)

// newConfigAgent builds a bare agent with a client, bypassing New so the config
// accessors can be tested without a live endpoint.
func newConfigAgent() *Agent {
	return &Agent{
		llm:     &llm.Client{BaseURL: "http://old", Model: "old-model", APIKey: "old-key"},
		maxIter: 10,
	}
}

func TestConfigReflectsSetters(t *testing.T) {
	a := newConfigAgent()
	a.approvalMode = "ask"

	got := a.Config()
	if got.BaseURL != "http://old" || got.Model != "old-model" || got.MaxIter != 10 || got.ApprovalMode != "ask" {
		t.Fatalf("initial Config wrong: %+v", got)
	}

	a.SetBaseURL("http://new/") // trailing slash trimmed
	a.SetModel("new-model")
	a.SetMaxIter(42)
	if c := a.Config(); c.BaseURL != "http://new" || c.Model != "new-model" || c.MaxIter != 42 {
		t.Errorf("after setters, Config = %+v", c)
	}
}

func TestSetMaxIterIgnoresNonPositive(t *testing.T) {
	a := newConfigAgent()
	a.SetMaxIter(0)
	a.SetMaxIter(-5)
	if a.Config().MaxIter != 10 {
		t.Errorf("MaxIter should stay 10, got %d", a.Config().MaxIter)
	}
}

func TestSetApprovalMode(t *testing.T) {
	a := newConfigAgent()
	a.approve = nil

	a.SetApprovalMode("AUTO") // normalized to lower
	if a.Config().ApprovalMode != "auto" {
		t.Errorf("ApprovalMode = %q, want auto", a.Config().ApprovalMode)
	}
	if a.approve == nil {
		t.Error("switching to auto should install an auto-approval func")
	}

	a.SetApprovalMode("ask")
	if a.Config().ApprovalMode != "ask" {
		t.Errorf("ApprovalMode = %q, want ask", a.Config().ApprovalMode)
	}
}

func TestSetAPIKey(t *testing.T) {
	a := newConfigAgent()
	a.SetAPIKey("  new-key  ")
	if a.llm.APIKey != "new-key" {
		t.Errorf("APIKey = %q, want trimmed new-key", a.llm.APIKey)
	}
}

// SetChatPath/SetModelsPath must round-trip through Config, and critically
// must apply an empty value (unlike SetBaseURL/SetModel, which ignore
// blanks): an empty ChatPath/ModelsPath is the meaningful "use the client
// default" state, not "leave unset". This is what lets the TUI /config
// wizard clear a stale path override when switching from a profile that
// needs one (e.g. openai) to one that does not (e.g. local-llama) on the
// live client, not just in the saved config file.
func TestSetChatPathAndModelsPathReflectInConfigIncludingClear(t *testing.T) {
	a := newConfigAgent()
	if got := a.Config(); got.ChatPath != "" || got.ModelsPath != "" {
		t.Fatalf("initial ChatPath/ModelsPath should be empty, got %+v", got)
	}

	a.SetChatPath("/chat/completions")
	a.SetModelsPath("/models")
	if got := a.Config(); got.ChatPath != "/chat/completions" || got.ModelsPath != "/models" {
		t.Fatalf("after setting, Config = %+v", got)
	}

	a.SetChatPath("")
	a.SetModelsPath("")
	if got := a.Config(); got.ChatPath != "" || got.ModelsPath != "" {
		t.Errorf("after clearing, Config = %+v, want both empty", got)
	}
}
