package plantree

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// SchemaVersion is written into every node. It matches the v4 spec so later
// layers extend these documents rather than replace them.
const SchemaVersion = 4

// Status is a leaf's lifecycle state. Structural nodes derive theirs and
// store an empty status.
type Status string

const (
	StatusUntouched     Status = "untouched"
	StatusReviewed      Status = "reviewed"
	StatusInProgress    Status = "in_progress"
	StatusNeedsRevision Status = "needs_revision"
	StatusCompleted     Status = "completed"
	StatusBlocked       Status = "blocked"
	StatusDelayed       Status = "delayed"
	StatusSkipped       Status = "skipped"
	StatusFailed        Status = "failed"
	StatusEscalated     Status = "escalated"
)

var validStatuses = map[Status]bool{
	StatusUntouched: true, StatusReviewed: true, StatusInProgress: true,
	StatusNeedsRevision: true, StatusCompleted: true, StatusBlocked: true,
	StatusDelayed: true, StatusSkipped: true, StatusFailed: true, StatusEscalated: true,
}

// needsReason reports whether the status must carry a non-empty reason.
func (s Status) needsReason() bool {
	switch s {
	case StatusBlocked, StatusDelayed, StatusSkipped, StatusFailed, StatusEscalated, StatusNeedsRevision:
		return true
	}
	return false
}

// Stage is planning progress, separate from lifecycle status.
type Stage string

const (
	StageSkeleton            Stage = "skeleton"
	StageInspected           Stage = "inspected"
	StageDrafted             Stage = "drafted"
	StageAwaitingAnswers     Stage = "awaiting_answers"
	StageNeedsReconciliation Stage = "needs_reconciliation"
	StageApproved            Stage = "approved"
)

var validStages = map[Stage]bool{
	StageSkeleton: true, StageInspected: true, StageDrafted: true,
	StageAwaitingAnswers: true, StageNeedsReconciliation: true, StageApproved: true,
}

// Planning records planning progress for a node.
type Planning struct {
	Stage Stage `json:"stage"`
}

// Work is a step's generic specification.
type Work struct {
	Description        string   `json:"description"`
	TargetPaths        []string `json:"target_paths"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	TestCommand        []string `json:"test_command"`
}

// Node is one document in the tree. Its kind comes from its id.
type Node struct {
	SchemaVersion int      `json:"schema_version"`
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	NodeRevision  int      `json:"node_revision"`
	ContextDigest string   `json:"context_digest"`
	ParentRef     *string  `json:"parent_ref"`
	DependsOn     []string `json:"depends_on"`
	Status        Status   `json:"status"`
	Reason        string   `json:"reason"`
	Planning      Planning `json:"planning"`
	ResumeNote    string   `json:"resume_note"`
	Objective     string   `json:"objective"`
	Work          *Work    `json:"work"`
}

// Kind returns the node's level, or "" if its id is malformed.
func (n Node) Kind() Kind {
	k, _ := ParseID(n.ID)
	return k
}

// Validate checks a node's own fields. Cross-node rules (references
// resolving, cycles) belong to Repo.Verify.
func Validate(n Node) error {
	if n.SchemaVersion != SchemaVersion {
		return fmt.Errorf("plantree: %s: unsupported schema_version %d (want %d)", n.ID, n.SchemaVersion, SchemaVersion)
	}
	kind, err := ParseID(n.ID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(n.Title) == "" {
		return fmt.Errorf("plantree: %s: title is required", n.ID)
	}
	if n.NodeRevision < 1 {
		return fmt.Errorf("plantree: %s: node_revision must be at least 1", n.ID)
	}
	if strings.TrimSpace(n.ContextDigest) == "" {
		return fmt.Errorf("plantree: %s: context_digest is required", n.ID)
	}
	wantRef, err := ParentRef(n.ID)
	if err != nil {
		return err
	}
	switch {
	case wantRef == "" && n.ParentRef != nil:
		return fmt.Errorf("plantree: %s: parent_ref must be null at the root", n.ID)
	case wantRef != "" && (n.ParentRef == nil || *n.ParentRef != wantRef):
		return fmt.Errorf("plantree: %s: parent_ref must be %q", n.ID, wantRef)
	}
	seen := map[string]bool{}
	for _, d := range n.DependsOn {
		if _, err := ParseID(d); err != nil {
			return fmt.Errorf("plantree: %s: depends_on: %w", n.ID, err)
		}
		if d == n.ID {
			return fmt.Errorf("plantree: %s: depends on itself", n.ID)
		}
		if seen[d] {
			return fmt.Errorf("plantree: %s: duplicate dependency %s", n.ID, d)
		}
		seen[d] = true
	}
	if !validStages[n.Planning.Stage] {
		return fmt.Errorf("plantree: %s: unknown planning stage %q", n.ID, n.Planning.Stage)
	}
	if kind != KindStep {
		if n.Status != "" {
			return fmt.Errorf("plantree: %s: structural nodes derive their status; it must be empty", n.ID)
		}
		if n.Work != nil {
			return fmt.Errorf("plantree: %s: only steps carry work", n.ID)
		}
		return nil
	}
	if !validStatuses[n.Status] {
		return fmt.Errorf("plantree: %s: unknown status %q", n.ID, n.Status)
	}
	if n.Status.needsReason() && strings.TrimSpace(n.Reason) == "" {
		return fmt.Errorf("plantree: %s: status %s requires a reason", n.ID, n.Status)
	}
	if n.Planning.Stage == StageDrafted || n.Planning.Stage == StageApproved {
		return validateWork(n)
	}
	return nil
}

// validateWork requires a complete work spec once a step is drafted.
func validateWork(n Node) error {
	if n.Work == nil || strings.TrimSpace(n.Work.Description) == "" {
		return fmt.Errorf("plantree: %s: a %s step needs a work description", n.ID, n.Planning.Stage)
	}
	for _, c := range n.Work.AcceptanceCriteria {
		if strings.TrimSpace(c) != "" {
			return nil
		}
	}
	return fmt.Errorf("plantree: %s: a %s step needs at least one acceptance criterion", n.ID, n.Planning.Stage)
}

// Encode renders n as indented JSON with a trailing newline. Nil slices are
// written as empty arrays so readers never see null.
func Encode(n Node) ([]byte, error) {
	if n.DependsOn == nil {
		n.DependsOn = []string{}
	}
	if n.Work != nil {
		w := *n.Work
		if w.TargetPaths == nil {
			w.TargetPaths = []string{}
		}
		if w.AcceptanceCriteria == nil {
			w.AcceptanceCriteria = []string{}
		}
		if w.TestCommand == nil {
			w.TestCommand = []string{}
		}
		n.Work = &w
	}
	b, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Decode parses and validates a node. It rejects an unsupported schema
// version with an actionable message, unknown fields, and trailing data.
func Decode(b []byte) (Node, error) {
	var head struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return Node{}, fmt.Errorf("plantree: decode: %w", err)
	}
	if head.SchemaVersion != SchemaVersion {
		return Node{}, fmt.Errorf("plantree: unsupported schema_version %d (want %d)", head.SchemaVersion, SchemaVersion)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var n Node
	if err := dec.Decode(&n); err != nil {
		return Node{}, fmt.Errorf("plantree: decode: %w", err)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return Node{}, errors.New("plantree: decode: trailing data after the node")
	}
	if err := Validate(n); err != nil {
		return Node{}, err
	}
	return n, nil
}
