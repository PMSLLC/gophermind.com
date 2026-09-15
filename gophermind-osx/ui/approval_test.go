package ui

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// recordingApprove returns an ApproveFunc that records every call and
// (optionally) fails on demand, for testing ApproveFunc-failure paths.
func recordingApprove(fail bool) (ApproveFunc, *[]struct {
	SessionID, ID string
	Approved      bool
}) {
	var calls []struct {
		SessionID, ID string
		Approved      bool
	}
	fn := func(ctx context.Context, sessionID, approvalID string, approved bool) error {
		calls = append(calls, struct {
			SessionID, ID string
			Approved      bool
		}{sessionID, approvalID, approved})
		if fail {
			return errors.New("boom")
		}
		return nil
	}
	return fn, &calls
}

// TestApprovalTracker_AddCreatesPending covers "Approval card appears
// inline in transcript when approval-needed event received" at the model
// level, plus "Card shows: tool name, pretty-printed args."
func TestApprovalTracker_AddCreatesPending(t *testing.T) {
	fn, _ := recordingApprove(false)
	tr := NewApprovalTracker(fn)

	a := tr.Add("sess-1", "appr-1", "run_shell", `{
  "command": "echo hi"
}`)
	if a.Status != ApprovalPending {
		t.Errorf("Status = %v, want Pending", a.Status)
	}
	if a.Tool != "run_shell" {
		t.Errorf("Tool = %q", a.Tool)
	}
	if len(tr.Pending()) != 1 {
		t.Errorf("Pending() = %+v, want 1 entry", tr.Pending())
	}
}

// TestApprovalTracker_ResolveApprovedCallsApproveFunc covers "client.Approve()
// is called with correct session_id, tool_call_id, decision" for the
// approve path.
func TestApprovalTracker_ResolveApprovedCallsApproveFunc(t *testing.T) {
	fn, calls := recordingApprove(false)
	tr := NewApprovalTracker(fn)
	tr.Add("sess-1", "appr-1", "run_shell", "{}")

	if err := tr.Resolve(context.Background(), "appr-1", true); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("ApproveFunc called %d times, want 1", len(*calls))
	}
	got := (*calls)[0]
	if got.SessionID != "sess-1" || got.ID != "appr-1" || !got.Approved {
		t.Errorf("call = %+v", got)
	}

	all := tr.All()
	if len(all) != 1 || all[0].Status != ApprovalApproved {
		t.Errorf("All() = %+v, want Status Approved", all)
	}
	if len(tr.Pending()) != 0 {
		t.Error("resolved approval should no longer be Pending")
	}
}

func TestApprovalTracker_ResolveDeniedCallsApproveFunc(t *testing.T) {
	fn, calls := recordingApprove(false)
	tr := NewApprovalTracker(fn)
	tr.Add("sess-1", "appr-1", "run_shell", "{}")

	if err := tr.Resolve(context.Background(), "appr-1", false); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := (*calls)[0]; got.Approved {
		t.Error("ApproveFunc called with approved=true, want false")
	}
	if tr.All()[0].Status != ApprovalDenied {
		t.Errorf("Status = %v, want Denied", tr.All()[0].Status)
	}
}

// TestApprovalTracker_ResolveTwiceReturnsAlreadyResolved covers the
// "already-resolved" resolution state directly, and that a second
// resolution does not call ApproveFunc again (no double-decision sent to
// the server).
func TestApprovalTracker_ResolveTwiceReturnsAlreadyResolved(t *testing.T) {
	fn, calls := recordingApprove(false)
	tr := NewApprovalTracker(fn)
	tr.Add("sess-1", "appr-1", "run_shell", "{}")

	if err := tr.Resolve(context.Background(), "appr-1", true); err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	err := tr.Resolve(context.Background(), "appr-1", false)
	if !errors.Is(err, ErrAlreadyResolved) {
		t.Errorf("second Resolve error = %v, want ErrAlreadyResolved", err)
	}
	if len(*calls) != 1 {
		t.Errorf("ApproveFunc called %d times, want exactly 1 (no re-send)", len(*calls))
	}
	// The first (approved) decision must stick -- a losing second call
	// must not flip it to denied.
	if tr.All()[0].Status != ApprovalApproved {
		t.Errorf("Status = %v, want it to remain Approved", tr.All()[0].Status)
	}
}

func TestApprovalTracker_ResolveUnknownIDErrors(t *testing.T) {
	fn, _ := recordingApprove(false)
	tr := NewApprovalTracker(fn)
	if err := tr.Resolve(context.Background(), "never-added", true); err == nil {
		t.Fatal("expected an error for an unknown approval id")
	}
}

// TestApprovalTracker_ResolveApproveFuncFailureLeavesStatusPending covers
// the transport-failure path: if the server call fails, the local status
// must not silently flip to approved/denied.
func TestApprovalTracker_ResolveApproveFuncFailureLeavesStatusPending(t *testing.T) {
	fn, _ := recordingApprove(true)
	tr := NewApprovalTracker(fn)
	tr.Add("sess-1", "appr-1", "run_shell", "{}")

	if err := tr.Resolve(context.Background(), "appr-1", true); err == nil {
		t.Fatal("expected the ApproveFunc failure to propagate")
	}
	if tr.All()[0].Status != ApprovalPending {
		t.Errorf("Status = %v, want it to remain Pending after a failed resolve", tr.All()[0].Status)
	}
}

// TestApprovalTracker_CheckTimeoutsAutoDeniesPastDeadline covers "5-min
// timeout: ... auto-deny at 5:00".
func TestApprovalTracker_CheckTimeoutsAutoDeniesPastDeadline(t *testing.T) {
	fn, calls := recordingApprove(false)
	tr := NewApprovalTracker(fn)
	fixedStart := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tr.now = func() time.Time { return fixedStart }
	tr.Add("sess-1", "appr-1", "run_shell", "{}")

	// Not yet due.
	tr.now = func() time.Time { return fixedStart.Add(4 * time.Minute) }
	if timedOut := tr.CheckTimeouts(context.Background()); len(timedOut) != 0 {
		t.Errorf("CheckTimeouts at 4:00 = %+v, want none yet", timedOut)
	}

	// Past the 5-minute deadline.
	tr.now = func() time.Time { return fixedStart.Add(5*time.Minute + time.Second) }
	timedOut := tr.CheckTimeouts(context.Background())
	if len(timedOut) != 1 || timedOut[0].ID != "appr-1" {
		t.Fatalf("CheckTimeouts past deadline = %+v, want appr-1 timed out", timedOut)
	}
	if tr.All()[0].Status != ApprovalTimedOut {
		t.Errorf("Status = %v, want TimedOut", tr.All()[0].Status)
	}
	if len(*calls) != 1 || (*calls)[0].Approved {
		t.Errorf("calls = %+v, want one auto-deny (approved=false)", *calls)
	}

	// Already timed out: a second sweep must not re-fire it.
	timedOut2 := tr.CheckTimeouts(context.Background())
	if len(timedOut2) != 0 {
		t.Errorf("second CheckTimeouts = %+v, want none (already resolved)", timedOut2)
	}
}

// TestApproval_IsWarning covers "warning at 4:30" of the 5:00 timeout.
func TestApproval_IsWarning(t *testing.T) {
	created := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	a := &Approval{Status: ApprovalPending, CreatedAt: created}

	cases := []struct {
		elapsed time.Duration
		want    bool
	}{
		{4 * time.Minute, false},                // well before warning
		{4*time.Minute + 29*time.Second, false}, // just before 4:30
		{4*time.Minute + 30*time.Second, true},  // exactly at 4:30
		{4*time.Minute + 45*time.Second, true},  // inside the window
		{5 * time.Minute, false},                // at/after timeout: not "warning", it's due
		{6 * time.Minute, false},
	}
	for _, c := range cases {
		got := a.IsWarning(created.Add(c.elapsed))
		if got != c.want {
			t.Errorf("IsWarning at +%v = %v, want %v", c.elapsed, got, c.want)
		}
	}
}

// TestApprovalTracker_OnChangeFires covers the redraw-trigger contract the
// widget layer depends on.
func TestApprovalTracker_OnChangeFires(t *testing.T) {
	fn, _ := recordingApprove(false)
	tr := NewApprovalTracker(fn)
	var calls atomic.Int32
	tr.OnChange(func() { calls.Add(1) })

	tr.Add("s", "a1", "t", "{}")
	tr.Resolve(context.Background(), "a1", true)

	if got := calls.Load(); got != 2 {
		t.Errorf("OnChange called %d times, want 2 (Add + Resolve)", got)
	}
}

func TestYNDecision(t *testing.T) {
	cases := []struct {
		key     rune
		approve bool
		ok      bool
	}{
		{'y', true, true},
		{'Y', true, true},
		{'n', false, true},
		{'N', false, true},
		{'x', false, false},
		{' ', false, false},
	}
	for _, c := range cases {
		approve, ok := YNDecision(c.key)
		if approve != c.approve || ok != c.ok {
			t.Errorf("YNDecision(%q) = (%v, %v), want (%v, %v)", c.key, approve, ok, c.approve, c.ok)
		}
	}
}

func TestApprovalStatus_String(t *testing.T) {
	for s, want := range map[ApprovalStatus]string{
		ApprovalPending:   "pending",
		ApprovalApproved:  "approved",
		ApprovalDenied:    "denied",
		ApprovalTimedOut:  "denied (timed out)",
		ApprovalStatus(9): "unknown",
	} {
		if got := s.String(); got != want {
			t.Errorf("Status(%d).String() = %q, want %q", s, got, want)
		}
	}
}
