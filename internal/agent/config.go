package agent

import (
	"strings"

	"gophermind/internal/safety"
)

// AgentConfig is a snapshot of the agent's user-facing configuration. It exists
// so the TUI's /config wizard can pre-fill the current values and report what
// changed after the user edits them.
type AgentConfig struct {
	BaseURL      string
	Model        string
	ApprovalMode string // "ask" or "auto"
	MaxIter      int
}

// Config returns the agent's current user-facing configuration.
func (a *Agent) Config() AgentConfig {
	return AgentConfig{
		BaseURL:      a.llm.BaseURL,
		Model:        a.llm.Model,
		ApprovalMode: a.approvalMode,
		MaxIter:      a.maxIter,
	}
}

// SetBaseURL points the agent at a different OpenAI-compatible endpoint for
// subsequent requests. The trailing slash is trimmed to match client setup.
func (a *Agent) SetBaseURL(u string) {
	a.llm.BaseURL = strings.TrimRight(strings.TrimSpace(u), "/")
}

// SetModel changes the model used for subsequent requests.
func (a *Agent) SetModel(m string) { a.llm.Model = strings.TrimSpace(m) }

// SetAPIKey updates the bearer token sent on subsequent requests.
func (a *Agent) SetAPIKey(k string) { a.llm.APIKey = strings.TrimSpace(k) }

// SetMaxIter sets the per-turn tool-iteration budget. A value < 1 is ignored so
// a stray zero from a wizard never disables the loop.
func (a *Agent) SetMaxIter(n int) {
	if n > 0 {
		a.maxIter = n
	}
}

// SetApprovalMode records the approval mode ("ask"/"auto"). Switching to "auto"
// takes effect immediately for subsequent tool calls; switching back to "ask"
// is recorded for display and the saved config, but the interactive prompt is
// only re-installed on the next launch (the TUI owns that closure), so callers
// should surface that when reporting the change.
func (a *Agent) SetApprovalMode(mode string) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	a.approvalMode = mode
	if mode == "auto" {
		a.approve = safety.Auto
	}
}
