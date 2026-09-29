// Package blackboard defines the runtime-state store GopherMind uses to coordinate
// workers. Implement this interface exactly; the executor, resume logic, run report,
// and live view all program against it.
//
// The blackboard holds RUNTIME state only. Node definitions live in the file tree
// (.gophermind/<run>/...). A row is keyed by (RunID, NodeID).
//
// Reference backend: SQLite via modernc.org/sqlite (pure Go, no cgo), one file per
// harness at ~/.gophermind/blackboard.db, WAL mode. Claim() must be a single
// UPDATE ... WHERE status='ready' so it is atomic without application locks.
package blackboard

import (
	"context"
	"time"
)

// Status is the runtime state of a node. Transitions are enforced by SetStatus.
type Status string

const (
	StatusPending       Status = "pending"        // created, dependencies not yet verified
	StatusReady         Status = "ready"          // all depends_on verified; claimable
	StatusClaimed       Status = "claimed"        // a worker owns it, no model call yet
	StatusInProgress    Status = "in_progress"    // model attempts under way
	StatusVerified      Status = "verified"       // tests passed, landed
	StatusFailed        Status = "failed"         // terminal failure not eligible for revision (e.g. critical network)
	StatusNeedsRevision Status = "needs_revision" // chain exhausted or contract changed; planner must rewrite
	StatusEscalated     Status = "escalated"      // max_revisions exceeded; waiting on a human
)

// Allowed transitions. SetStatus rejects anything else with ErrBadTransition.
//
//	pending        -> ready, needs_revision
//	ready          -> claimed, needs_revision
//	claimed        -> in_progress, ready (release)
//	in_progress    -> verified, failed, needs_revision, ready (release on crash)
//	needs_revision -> ready (after planner rewrite), escalated
//	escalated      -> ready (after human edits), failed (human abandons)
//	verified       -> needs_revision (only when an upstream contract changed)
//	failed         -> (terminal)

type Verdict string

const (
	VerdictPass  Verdict = "pass"
	VerdictFail  Verdict = "fail"  // tests ran and some failed; counts against the chain
	VerdictError Verdict = "error" // could not get a usable answer (429, timeout, malformed); does NOT count against the chain
)

type Claim struct {
	Worker    string
	ClaimedAt time.Time
}

type Attempt struct {
	Model         string
	Provider      string
	Revision      int
	Order         int // 1-based position in the fallback chain for this revision
	StartedAt     time.Time
	DurationMS    int64
	Verdict       Verdict
	TestsPassed   int
	TestsTotal    int
	FailedTests   []string
	FailureReason string // human-readable; for VerdictError use a prefix: rate_limited:, timeout:, malformed:, context_too_long:
}

type Result struct {
	FilesChanged []string
	Commit       string
}

// Row is the full runtime record for one node.
type Row struct {
	RunID       string
	NodeID      string
	Status      Status
	Revision    int
	Claim       *Claim
	HeartbeatAt time.Time
	Attempts    []Attempt
	Result      *Result
	UpdatedAt   time.Time
}

type Filter struct {
	Statuses []Status // empty means all
	Wave     *int     // nil means all
}

type EventKind string

const (
	EventStatus  EventKind = "status"
	EventAttempt EventKind = "attempt"
	EventResult  EventKind = "result"
)

type Event struct {
	Kind EventKind
	Row  Row
	At   time.Time
}

// Blackboard is the coordination store. All methods are safe for concurrent use
// from multiple goroutines and, for the SQLite backend, multiple processes.
type Blackboard interface {
	// InitRun creates a pending row for every node ID. Idempotent: existing rows are left alone,
	// which is what --resume relies on.
	InitRun(ctx context.Context, runID string, nodeIDs []string, waves map[string]int) error

	Get(ctx context.Context, runID, nodeID string) (Row, error)
	List(ctx context.Context, runID string, f Filter) ([]Row, error)

	// Claim atomically moves a node from ready to claimed for worker. Returns false, nil if
	// another worker won or the node is not ready. Never returns an error for a lost race.
	Claim(ctx context.Context, runID, nodeID, worker string) (bool, error)

	// Heartbeat refreshes HeartbeatAt for a claim. Resume treats a claim whose heartbeat is
	// older than stale_claim_seconds as abandoned and releases it.
	Heartbeat(ctx context.Context, runID, nodeID, worker string) error

	// Release returns a claimed or in_progress node to ready. Only the claiming worker may call it.
	Release(ctx context.Context, runID, nodeID, worker string) error

	// SetStatus is a compare-and-set: it fails with ErrBadTransition if the row is not
	// currently in from, or the from->to transition is not allowed.
	SetStatus(ctx context.Context, runID, nodeID string, from, to Status) error

	// SetRevision records that the planner rewrote the node definition.
	SetRevision(ctx context.Context, runID, nodeID string, revision int) error

	AppendAttempt(ctx context.Context, runID, nodeID string, a Attempt) error
	SetResult(ctx context.Context, runID, nodeID string, r Result) error

	// ReleaseStale returns every claimed/in_progress row whose heartbeat is older than olderThan
	// to ready, and reports their IDs. Called once at the start of --resume.
	ReleaseStale(ctx context.Context, runID string, olderThan time.Duration) ([]string, error)

	// Watch streams events for a run until ctx is cancelled. Implementations may poll; the live
	// view tolerates up to 2s latency. The channel is closed when ctx is done.
	Watch(ctx context.Context, runID string) (<-chan Event, error)

	Close() error
}

var (
	ErrNotFound      = Err("blackboard: row not found")
	ErrBadTransition = Err("blackboard: transition not allowed")
	ErrNotOwner      = Err("blackboard: worker does not hold the claim")
)

type Err string

func (e Err) Error() string { return string(e) }
