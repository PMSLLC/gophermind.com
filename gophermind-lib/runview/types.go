// Package runview reads a run folder (.gophermind/<run-id>/ inside a repo) and
// follows it while it changes. It is the one place that knows the folder's
// layout for readers: plan nodes, node runtime files, _state/events.jsonl,
// _state/calls.jsonl, report.json, attempts/. It owns the wire types of the
// pipeline contract (schema_version 1) that the server serves and the desktop
// app decodes. It never writes to the run folder.
package runview

import (
	"encoding/json"
	"errors"

	"gophermind/gophermind-lib/build/report"
)

// SchemaVersion is the pipeline contract version. Every JSON body carries it
// (except the final report, which keeps the report's own version), and every
// response carries it in the header X-Gophermind-Pipeline-Schema.
const SchemaVersion = 1

// Run statuses (RunSummary.Status).
const (
	StatusPlanning     = "planning"
	StatusWaiting      = "waiting"
	StatusPlanned      = "planned"
	StatusBuilding     = "building"
	StatusVerified     = "verified"
	StatusFailed       = "failed"
	StatusEscalated    = "escalated"
	StatusInterrupted  = "interrupted"
	StatusHarnessFault = "harness_fault"
)

// Event types on the stream (GET /pipeline/events).
const (
	EventNodeStatus  = "node-status"
	EventNodeAttempt = "node-attempt"
	EventNodeResult  = "node-result"
	EventWaveChanged = "wave-changed"
	EventRunStatus   = "run-status"
	EventRunReport   = "run-report"
)

var (
	// ErrBadID is returned for a run id, node id, attempt number or artifact
	// name that does not match its pattern.
	ErrBadID = errors.New("runview: invalid id")
	// ErrNotFound is returned when the run, node or file does not exist, or is
	// refused because it (or a parent) is a symbolic link.
	ErrNotFound = errors.New("runview: not found")
)

// Counts summarizes the function nodes of a run (the leaves the executor builds).
type Counts struct {
	Nodes    int            `json:"nodes"`
	ByStatus map[string]int `json:"by_status"`
}

// CoverageCount is how many of the brief's requirements the plan covers.
type CoverageCount struct {
	Covered int `json:"covered"`
	Total   int `json:"total"`
}

// RunSummary is one run in a list and the header of a state response.
type RunSummary struct {
	RunID      string        `json:"run_id"`
	Title      string        `json:"title"`
	RepoName   string        `json:"repo_name"` // the folder name, never a path
	Status     string        `json:"status"`
	StopReason string        `json:"stop_reason"`
	PlannedAt  string        `json:"planned_at"`
	StartedAt  string        `json:"started_at"`
	UpdatedAt  string        `json:"updated_at"`
	Waves      int           `json:"waves"`
	Counts     Counts        `json:"counts"`
	Coverage   CoverageCount `json:"coverage"`
}

// Attempt is one model attempt at a node. NodeSummary.LastAttempt and the
// node-attempt event carry the same shape.
type Attempt struct {
	Model         string   `json:"model"`
	Provider      string   `json:"provider"`
	Verdict       string   `json:"verdict"`
	FailureReason string   `json:"failure_reason"`
	DurationMS    int64    `json:"duration_ms"`
	StartedAt     string   `json:"started_at"`
	Order         int      `json:"order"`
	Revision      int      `json:"revision"`
	TestsPassed   int      `json:"tests_passed"`
	TestsTotal    int      `json:"tests_total"`
	FailedTests   []string `json:"failed_tests"`
}

// NodeResult is what a verified node landed.
type NodeResult struct {
	Commit       string   `json:"commit"`
	FilesChanged []string `json:"files_changed"`
}

// TestCount is how many tests a node declares.
type TestCount struct {
	Total int `json:"total"`
}

// NodeSummary is one node in the list view. The groups and the embedded
// decisions are not here; they come from GET /pipeline/node.
type NodeSummary struct {
	ID             string      `json:"id"`
	Kind           string      `json:"kind"`
	Parent         string      `json:"parent"`
	Title          string      `json:"title"`
	Status         string      `json:"status"`
	Wave           int         `json:"wave"`
	ModelTier      string      `json:"model_tier"`
	NodeClass      string      `json:"node_class"`
	DependsOn      []string    `json:"depends_on"`
	RequirementIDs []string    `json:"requirement_ids"`
	Revision       int         `json:"revision"`
	ClaimedBy      string      `json:"claimed_by"`
	AttemptCount   int         `json:"attempt_count"`
	LastAttempt    *Attempt    `json:"last_attempt"`
	Result         *NodeResult `json:"result"`
	Tests          TestCount   `json:"tests"`
}

// RuntimeDoc is a node's runtime file (status, claim, attempts, result).
type RuntimeDoc struct {
	Status      string      `json:"status"`
	Revision    int         `json:"revision"`
	ClaimedBy   string      `json:"claimed_by"`
	HeartbeatAt string      `json:"heartbeat_at"`
	UpdatedAt   string      `json:"updated_at"`
	Attempts    []Attempt   `json:"attempts"`
	Result      *NodeResult `json:"result"`
}

// ArtifactEntry lists the saved files of one attempt folder (attempts/<node>/<n>/).
type ArtifactEntry struct {
	Attempt int      `json:"attempt"`
	Files   []string `json:"files"`
}

// NodeBody is GET /pipeline/node: the full plan node as stored (all groups and
// its embedded decisions), its runtime file (nil before the executor started),
// and its saved attempt artifacts.
type NodeBody struct {
	SchemaVersion int             `json:"schema_version"`
	Node          json.RawMessage `json:"node"`
	Runtime       *RuntimeDoc     `json:"runtime"`
	Artifacts     []ArtifactEntry `json:"artifacts"`
}

// DecisionsBody is GET /pipeline/decisions: the root node's records followed
// by the build's revision decisions.
type DecisionsBody struct {
	SchemaVersion int               `json:"schema_version"`
	Decisions     []json.RawMessage `json:"decisions"`
}

// RunsBody is GET /pipeline/runs, newest first.
type RunsBody struct {
	SchemaVersion int          `json:"schema_version"`
	Runs          []RunSummary `json:"runs"`
}

// StateBody is GET /pipeline/state. Run is null when the repo has no run.
// EventSeq is the line count of _state/events.jsonl read before the nodes, so
// a client resumes the stream at exactly this snapshot with since=EventSeq.
type StateBody struct {
	SchemaVersion int           `json:"schema_version"`
	GeneratedAt   string        `json:"generated_at"`
	EventSeq      int64         `json:"event_seq"`
	Run           *RunSummary   `json:"run"`
	Nodes         []NodeSummary `json:"nodes"`
	Warnings      []string      `json:"warnings,omitempty"`
}

// ReportBody is the shape of GET /pipeline/report. The final body is the
// executor's report.json (no complete key); the running body is built from the
// call log and runtime files and carries "complete": false.
type ReportBody struct {
	report.Report
	Complete *bool `json:"complete,omitempty"`
}

// IsComplete reports whether the report is final: an absent key means complete.
func (b ReportBody) IsComplete() bool { return b.Complete == nil || *b.Complete }

// Event is one event of the stream. Seq is the 1-based line number of
// _state/events.jsonl (the SSE id). Data is the JSON payload.
type Event struct {
	Seq  int64
	Type string
	Data json.RawMessage
}

// NodeStatusData is the payload of node-status. A status line and a revision
// line of the event log both map to it: Status and Revision are the node's
// values as they are now.
type NodeStatusData struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Wave     int    `json:"wave"`
	Revision int    `json:"revision"`
}

// NodeAttemptData is the payload of node-attempt.
type NodeAttemptData struct {
	ID      string  `json:"id"`
	Attempt Attempt `json:"attempt"`
}

// NodeResultData is the payload of node-result.
type NodeResultData struct {
	ID     string     `json:"id"`
	Result NodeResult `json:"result"`
}

// WaveChangedData is the payload of wave-changed; State is "started" or "finished".
type WaveChangedData struct {
	Wave  int    `json:"wave"`
	State string `json:"state"`
}

// RunStatusData is the payload of run-status.
type RunStatusData struct {
	Status     string `json:"status"`
	StopReason string `json:"stop_reason"`
}
