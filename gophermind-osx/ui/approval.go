package ui

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ApprovalStatus is a pending tool-call approval's resolution state.
type ApprovalStatus int

const (
	ApprovalPending ApprovalStatus = iota
	ApprovalApproved
	ApprovalDenied
	ApprovalTimedOut
)

func (s ApprovalStatus) String() string {
	switch s {
	case ApprovalPending:
		return "pending"
	case ApprovalApproved:
		return "approved"
	case ApprovalDenied:
		return "denied"
	case ApprovalTimedOut:
		return "denied (timed out)"
	default:
		return "unknown"
	}
}

// ApprovalTimeout is how long a pending approval waits before
// ApprovalTracker.CheckTimeouts auto-denies it.
const ApprovalTimeout = 5 * time.Minute

// ApprovalWarningAt is how long before ApprovalTimeout a pending approval
// enters its warning window (Approval.IsWarning becomes true) -- "warning
// at 4:30" of a 5:00 timeout.
const ApprovalWarningAt = 30 * time.Second

// Approval is one gated tool call awaiting (or having received) a decision.
type Approval struct {
	ID        string
	SessionID string
	Tool      string
	Args      string // pretty-printed JSON
	Status    ApprovalStatus
	CreatedAt time.Time
}

// TimeRemaining returns how long until this approval times out, relative
// to now. Negative once past ApprovalTimeout.
func (a *Approval) TimeRemaining(now time.Time) time.Duration {
	return ApprovalTimeout - now.Sub(a.CreatedAt)
}

// IsWarning reports whether this approval is inside its warning window
// (TimeRemaining <= ApprovalWarningAt) but not yet timed out.
func (a *Approval) IsWarning(now time.Time) bool {
	remaining := a.TimeRemaining(now)
	return remaining <= ApprovalWarningAt && remaining > 0
}

// ApproveFunc resolves an approval against the backend -- normally
// (*client.Client).Approve, injected rather than imported directly so
// this package (and its tests) don't need a real or fake HTTP server just
// to exercise the tracker's state machine.
type ApproveFunc func(ctx context.Context, sessionID, approvalID string, approved bool) error

// ApprovalTracker holds every approval card the transcript currently
// shows or has shown, resolves them (calling ApproveFunc), and handles
// the 5-minute timeout.
type ApprovalTracker struct {
	mu        sync.Mutex
	approvals map[string]*Approval
	order     []string // insertion order
	approveFn ApproveFunc
	onChange  func()
	now       func() time.Time
}

// NewApprovalTracker returns a tracker that resolves approvals via
// approveFn.
func NewApprovalTracker(approveFn ApproveFunc) *ApprovalTracker {
	return &ApprovalTracker{
		approvals: make(map[string]*Approval),
		approveFn: approveFn,
		now:       time.Now,
	}
}

// SetApproveFunc replaces the tracker's ApproveFunc, so a connection
// established after construction can wire client.Approve without
// rebuilding the tracker (and its existing cards).
func (t *ApprovalTracker) SetApproveFunc(fn ApproveFunc) {
	t.mu.Lock()
	t.approveFn = fn
	t.mu.Unlock()
}

// OnChange registers f to be called after every state change (Add,
// Resolve, a timeout). Same non-blocking, non-reentrant contract as
// Transcript.OnChange.
func (t *ApprovalTracker) OnChange(f func()) {
	t.mu.Lock()
	t.onChange = f
	t.mu.Unlock()
}

func (t *ApprovalTracker) notify() {
	if t.onChange != nil {
		t.onChange()
	}
}

// Add records a new pending approval, covering "Approval card appears
// inline in transcript when approval-needed event received" at the model
// level.
func (t *ApprovalTracker) Add(sessionID, approvalID, tool, prettyArgs string) *Approval {
	a := &Approval{ID: approvalID, SessionID: sessionID, Tool: tool, Args: prettyArgs, Status: ApprovalPending, CreatedAt: t.now()}
	t.mu.Lock()
	t.approvals[approvalID] = a
	t.order = append(t.order, approvalID)
	t.mu.Unlock()
	t.notify()
	return a
}

// ErrAlreadyResolved is returned by Resolve when id has already been
// decided (approved, denied, or timed out) -- the "already-resolved"
// resolution state the acceptance criteria names, distinct from a
// not-found id or a transport error.
var ErrAlreadyResolved = fmt.Errorf("approval already resolved")

// Resolve records approved/denied for id and calls ApproveFunc. Returns
// ErrAlreadyResolved (without calling ApproveFunc again) if id was already
// decided -- covers a duplicate click or a Y/N shortcut racing a button
// click. Returns an error naming the unknown id if it was never added.
func (t *ApprovalTracker) Resolve(ctx context.Context, id string, approved bool) error {
	t.mu.Lock()
	a, ok := t.approvals[id]
	if !ok {
		t.mu.Unlock()
		return fmt.Errorf("approval %q not found", id)
	}
	if a.Status != ApprovalPending {
		t.mu.Unlock()
		return ErrAlreadyResolved
	}
	sessionID, approveFn := a.SessionID, t.approveFn
	t.mu.Unlock()

	if err := approveFn(ctx, sessionID, id, approved); err != nil {
		return err
	}

	t.mu.Lock()
	if approved {
		a.Status = ApprovalApproved
	} else {
		a.Status = ApprovalDenied
	}
	t.mu.Unlock()
	t.notify()
	return nil
}

// CheckTimeouts auto-denies (calling ApproveFunc with approved=false)
// every pending approval whose TimeRemaining has reached zero, and
// returns the ones it just timed out. Meant to be called periodically
// (e.g. from a UI timer) -- covers "5-min timeout: ... auto-deny at
// 5:00". A per-approval ApproveFunc failure is not fatal to the sweep: it
// logs nowhere (this package has no logger) but leaves that approval
// pending for the next sweep to retry, rather than losing track of it.
func (t *ApprovalTracker) CheckTimeouts(ctx context.Context) []*Approval {
	now := t.now()
	t.mu.Lock()
	var due []*Approval
	for _, id := range t.order {
		a := t.approvals[id]
		if a.Status == ApprovalPending && a.TimeRemaining(now) <= 0 {
			due = append(due, a)
		}
	}
	t.mu.Unlock()

	var timedOut []*Approval
	for _, a := range due {
		if err := t.approveFn(ctx, a.SessionID, a.ID, false); err != nil {
			continue
		}
		t.mu.Lock()
		a.Status = ApprovalTimedOut
		t.mu.Unlock()
		timedOut = append(timedOut, a)
	}
	if len(timedOut) > 0 {
		t.notify()
	}
	return timedOut
}

// Pending returns every currently-pending approval, oldest first --
// what the persistent approval bar summarizes.
func (t *ApprovalTracker) Pending() []*Approval {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []*Approval
	for _, id := range t.order {
		if a := t.approvals[id]; a.Status == ApprovalPending {
			out = append(out, a)
		}
	}
	return out
}

// All returns every approval this tracker has ever recorded, in the order
// they were added, regardless of status -- what the transcript's inline
// cards render (each shows its own current Status).
func (t *ApprovalTracker) All() []*Approval {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]*Approval, 0, len(t.order))
	for _, id := range t.order {
		out = append(out, t.approvals[id])
	}
	return out
}

// YNDecision maps a key rune to an approve/deny decision for the Y/N
// keyboard shortcut, case-insensitively. ok is false for any other key.
func YNDecision(key rune) (approve bool, ok bool) {
	switch key {
	case 'y', 'Y':
		return true, true
	case 'n', 'N':
		return false, true
	default:
		return false, false
	}
}

// ApproveLatest resolves the most recently added pending approval as
// approved (the Y key). No-op if nothing is pending.
func (t *ApprovalTracker) ApproveLatest() {
	pending := t.Pending()
	if len(pending) == 0 {
		return
	}
	latest := pending[len(pending)-1]
	_ = t.Resolve(context.Background(), latest.ID, true)
}

// DenyLatest resolves the most recently added pending approval as denied
// (the N key). No-op if nothing is pending.
func (t *ApprovalTracker) DenyLatest() {
	pending := t.Pending()
	if len(pending) == 0 {
		return
	}
	latest := pending[len(pending)-1]
	_ = t.Resolve(context.Background(), latest.ID, false)
}
