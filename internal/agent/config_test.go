package agent

import (
	"testing"

	"gophermind/internal/llm"
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
