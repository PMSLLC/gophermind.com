// Package ledger records every model call: who was asked, who answered, how
// long it took, and how it went. It stores sizes and SHA-256 hashes of the
// prompt and reply, never their text.
package ledger

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"

	"gophermind/gophermind-lib/briefv2/db"
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

type SQLite struct{ db *sql.DB }

var _ Ledger = (*SQLite)(nil)

func NewSQLite(d *sql.DB) *SQLite { return &SQLite{db: d} }

func (s *SQLite) Record(ctx context.Context, c *Call) error {
	if c.RunID == "" || c.Stage == "" || c.Provider == "" || c.Outcome == "" {
		return errors.New("ledger: run, stage, provider and outcome are required")
	}
	if c.At.IsZero() {
		c.At = time.Now()
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO calls (run_id, at, stage, task_type, node_class, node_id, revision, scope, tier, chain_pos, provider, model_requested, model_served,
		   prompt_tokens, completion_tokens, prompt_bytes, prompt_sha256, response_bytes, response_sha256, duration_ms,
		   outcome, error_kind, retry_after_s)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.RunID, db.TS(c.At), c.Stage, c.TaskType, c.NodeClass, c.NodeID, c.Revision, c.Scope, c.Tier, c.ChainPos, c.Provider, c.ModelRequested, c.ModelServed,
		c.PromptTokens, c.CompletionTokens, c.PromptBytes, c.PromptSHA256, c.ResponseBytes, c.ResponseSHA256, c.DurationMS,
		string(c.Outcome), c.ErrorKind, c.RetryAfterS)
	if err != nil {
		return fmt.Errorf("ledger: %w", err)
	}
	c.ID, _ = res.LastInsertId()
	return nil
}

func (s *SQLite) Amend(ctx context.Context, runID string, id int64, o Outcome, errorKind string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE calls SET outcome = ?, error_kind = ? WHERE id = ? AND run_id = ?`, string(o), errorKind, id, runID)
	if err != nil {
		return fmt.Errorf("ledger: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("ledger: no call with id %d", id)
	}
	return nil
}

func (s *SQLite) List(ctx context.Context, runID string, f Filter) ([]Call, error) {
	q := `SELECT id, run_id, at, stage, task_type, node_class, node_id, revision, scope, tier, chain_pos, provider, model_requested, model_served,
	        prompt_tokens, completion_tokens, prompt_bytes, prompt_sha256, response_bytes, response_sha256, duration_ms,
	        outcome, error_kind, retry_after_s
	      FROM calls WHERE run_id = ?`
	args := []any{runID}
	add := func(col, val string) {
		if val != "" {
			q += ` AND ` + col + ` = ?`
			args = append(args, val)
		}
	}
	add("stage", f.Stage)
	add("task_type", f.TaskType)
	add("node_id", f.NodeID)
	add("provider", f.Provider)
	add("outcome", string(f.Outcome))
	q += ` ORDER BY at, id`
	rs, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("ledger: %w", err)
	}
	defer rs.Close()
	var out []Call
	for rs.Next() {
		var c Call
		var at, outcome string
		if err := rs.Scan(&c.ID, &c.RunID, &at, &c.Stage, &c.TaskType, &c.NodeClass, &c.NodeID, &c.Revision, &c.Scope, &c.Tier, &c.ChainPos, &c.Provider,
			&c.ModelRequested, &c.ModelServed, &c.PromptTokens, &c.CompletionTokens, &c.PromptBytes, &c.PromptSHA256,
			&c.ResponseBytes, &c.ResponseSHA256, &c.DurationMS, &outcome, &c.ErrorKind, &c.RetryAfterS); err != nil {
			return nil, fmt.Errorf("ledger: %w", err)
		}
		c.At, _ = db.ParseTS(at)
		c.Outcome = Outcome(outcome)
		out = append(out, c)
	}
	return out, rs.Err()
}

func (s *SQLite) Summary(ctx context.Context, runID string) ([]ModelSummary, error) {
	rs, err := s.db.QueryContext(ctx,
		`SELECT task_type, node_class, provider, CASE WHEN model_served = '' THEN model_requested ELSE model_served END AS m, outcome,
		        COUNT(*), SUM(prompt_tokens), SUM(completion_tokens), SUM(duration_ms)
		 FROM calls WHERE run_id = ? GROUP BY task_type, node_class, provider, m, outcome`, runID)
	if err != nil {
		return nil, fmt.Errorf("ledger: %w", err)
	}
	defer rs.Close()
	byKey := map[string]*ModelSummary{}
	for rs.Next() {
		var task, class, prov, model, outcome string
		var n int
		var pt, ct, ms int64
		if err := rs.Scan(&task, &class, &prov, &model, &outcome, &n, &pt, &ct, &ms); err != nil {
			return nil, fmt.Errorf("ledger: %w", err)
		}
		k := task + "\x00" + class + "\x00" + prov + "\x00" + model
		m := byKey[k]
		if m == nil {
			m = &ModelSummary{TaskType: task, NodeClass: class, Provider: prov, Model: model, Outcomes: map[Outcome]int{}}
			byKey[k] = m
		}
		m.Calls += n
		m.PromptTokens += pt
		m.CompletionTokens += ct
		m.TotalMS += ms
		m.Outcomes[Outcome(outcome)] += n
	}
	if err := rs.Err(); err != nil {
		return nil, err
	}
	out := make([]ModelSummary, 0, len(byKey))
	for _, m := range byKey {
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.TaskType != b.TaskType {
			return a.TaskType < b.TaskType
		}
		if a.NodeClass != b.NodeClass {
			return a.NodeClass < b.NodeClass
		}
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		return a.Model < b.Model
	})
	return out, nil
}
