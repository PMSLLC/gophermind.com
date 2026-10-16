// Package ledger records every model call: who was asked, who answered, how
// long it took, and how it went. It stores sizes and SHA-256 hashes of the
// prompt and reply, never their text.
package ledger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

type Outcome string

const (
	OutcomeOK           Outcome = "ok"
	OutcomeRateLimited  Outcome = "rate_limited"
	OutcomeTimeout      Outcome = "timeout"
	OutcomeMalformed    Outcome = "malformed"
	OutcomeAuth         Outcome = "auth"
	OutcomeTooLong      Outcome = "too_long"
	OutcomeModelMissing Outcome = "model_missing"
	OutcomeError        Outcome = "error"
)

// Call is one model attempt.
type Call struct {
	ID       int64
	RunID    string
	At       time.Time
	Stage    string // clarify, contract:<pass>, decompose:<component>, coverage, coverage_fill, testwrite:<node>, revise:<node>, implement:<node>
	NodeID   string
	Revision int
	Scope    string // brief | component | node
	Tier     string
	ChainPos int

	// TaskType is the kind of work (clarify, contract, decompose, coverage,
	// testwrite, revise, implement) and NodeClass, for a call about one leaf,
	// the class of function (pure, validation, handler, client, storage,
	// concurrency, wiring, other). Together they let models be compared by the
	// kind of work they were given.
	TaskType  string
	NodeClass string

	Provider       string
	ModelRequested string
	ModelServed    string

	PromptTokens     int
	CompletionTokens int
	PromptBytes      int
	PromptSHA256     string
	ResponseBytes    int
	ResponseSHA256   string
	DurationMS       int64

	Outcome     Outcome
	ErrorKind   string
	RetryAfterS int
}

type Filter struct {
	Stage    string
	TaskType string
	NodeID   string
	Provider string
	Outcome  Outcome
}

// ModelSummary aggregates a run's calls per task type, node class, provider
// and model served.
type ModelSummary struct {
	TaskType         string
	NodeClass        string
	Provider         string
	Model            string
	Calls            int
	PromptTokens     int64
	CompletionTokens int64
	TotalMS          int64
	Outcomes         map[Outcome]int
}

type Ledger interface {
	// Record stores c and sets c.ID.
	Record(ctx context.Context, c *Call) error
	// Amend changes a stored row's outcome, for a reply that arrived fine but
	// failed the stage's parser.
	Amend(ctx context.Context, runID string, id int64, o Outcome, errorKind string) error
	List(ctx context.Context, runID string, f Filter) ([]Call, error)
	Summary(ctx context.Context, runID string) ([]ModelSummary, error)
}

// Digest returns the byte length and hex SHA-256 of b.
func Digest(b []byte) (int, string) {
	sum := sha256.Sum256(b)
	return len(b), hex.EncodeToString(sum[:])
}
