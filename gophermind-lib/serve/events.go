package serve

import "gophermind/gophermind-lib/agent"

// This file pins the JSON payload shape of every SSE event type the server
// emits, as named Go structs with JSON tags — the contract gophermind-server
// and gophermind-osx both build against (see .planning/tasks/01-03.json).
// Event names are the SSE "event:" line value; each struct is the "data:"
// line's JSON payload, except where noted as plain text.

// TokenEvent ("token"): one streamed completion token. Payload is the raw
// token text, not JSON — no struct.

// AssistantEvent ("assistant"): a complete assistant message. Payload is the
// raw message text, not JSON — no struct.

// DoneEvent ("done"): the turn finished successfully. Payload is empty — no
// struct.

// ErrorEvent ("error"): the turn failed. Payload is a plain-text message
// ("error: <detail>"), not JSON — no struct.

// ToolCallEvent ("tool_call") reports one tool invocation the model made.
type ToolCallEvent struct {
	Name string `json:"name"`
	Args string `json:"args"`
}

// ToolResultEvent ("tool_result") reports one tool call's result.
type ToolResultEvent struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

// UsageEvent ("usage") reports running per-session token totals. Payload is
// agent.UsageSnapshot directly — see gophermind-lib/agent/usage.go.
type UsageEvent = agent.UsageSnapshot

// ApprovalNeededEvent ("approval-needed") reports a gated tool call blocked
// on remote approval.
type ApprovalNeededEvent struct {
	ApprovalID string `json:"approval_id"`
	Tool       string `json:"tool"`
	Args       string `json:"args"`
}

// ModelSwitchedEvent ("model-switched") reports the active model changed
// mid-session (e.g. a fallback wave promoting to the next configured model).
//
// Not yet emitted by any code path — this type pins the intended contract
// shape per .planning/tasks/01-03.json's SSE event list, but wiring an
// actual emission point (in the model-fallback/cycling logic) is unbuilt.
// A caller depending on this event firing needs that follow-up work first.
type ModelSwitchedEvent struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason"`
}

// The pipeline stream (GET /pipeline/events) has its own event vocabulary:
// node-status, node-attempt, node-result, wave-changed, run-status and
// run-report. Its payload types live in gophermind-lib/runview (events.go
// would otherwise import the run folder reader).
