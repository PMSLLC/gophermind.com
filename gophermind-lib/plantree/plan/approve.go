package plan

import (
	"errors"
	"fmt"
	"strings"

	"gophermind/gophermind-lib/plantree"
)

// ErrNotApprovable is returned when the plan is not ready to be approved. The
// wrapped message names what is in the way.
var ErrNotApprovable = errors.New("plan: the plan cannot be approved yet")

// Approval is what one Approve call did.
type Approval struct {
	// Steps is the number of steps this call moved to approved and reviewed.
	Steps int
	// Already is the number that were already there, which is what a rerun
	// after a partial approval reports for the part that was done.
	Already int
}

// Approve marks the whole plan approved: every step moves to stage approved
// and status reviewed, and the structural nodes derive theirs (Summarize
// reports the root reviewed once every step is). In this version the owner
// approves the whole plan at once; per-task approval is a later layer.
//
// It refuses, with ErrNotApprovable, while anything is outstanding: a
// question is open, a step waits to be re-planned, a task has no steps, or
// NextActions offers anything other than exactly one approve action. The
// message names the first thing in the way, so the owner reads what to do
// rather than that something is wrong.
//
// It is idempotent and crash-safe. A step already approved and reviewed is
// counted and left alone, so approving twice changes nothing; and a run
// interrupted part way leaves a tree whose only outstanding action is still
// approve, so simply calling it again finishes the job.
func Approve(repo *plantree.Repo) (Approval, error) {
	// The same lock RunPass1 and RunPass2 take, so a pass running in another
	// process yields a clean ErrRunBusy instead of a half-written approval.
	release, err := AcquireRun(repo)
	if err != nil {
		return Approval{}, err
	}
	defer release()
	if err := Approvable(repo); err != nil {
		return Approval{}, err
	}
	var steps []plantree.Node
	if err := repo.Walk(func(n plantree.Node) error {
		if n.Kind() == plantree.KindStep {
			steps = append(steps, n)
		}
		return nil
	}); err != nil {
		return Approval{}, err
	}
	var out Approval
	for _, s := range steps {
		// Re-read: Walk's snapshot may be older than a step this same loop
		// already wrote, and Update needs the current revision.
		cur, err := repo.Get(s.ID)
		if err != nil {
			return out, err
		}
		if cur.Planning.Stage == plantree.StageApproved && cur.Status == plantree.StatusReviewed {
			out.Already++
			continue
		}
		if _, err := repo.Update(cur.ID, cur.NodeRevision, func(n *plantree.Node) error {
			n.Planning.Stage = plantree.StageApproved
			n.Status = plantree.StatusReviewed
			return nil
		}); err != nil {
			return out, fmt.Errorf("plan: approving %s: %w", cur.ID, err)
		}
		out.Steps++
	}
	return out, nil
}

// Approvable reports why the plan cannot be approved, or nil when it can. It
// is what Approve checks, exported so a user interface can offer approval
// only when it would be accepted, and say why when it would not.
func Approvable(repo *plantree.Repo) error {
	if _, err := repo.Get(plantree.RootID); err != nil {
		return err
	}
	// Dangling depends_on and cycles are refused before anything is written.
	if err := repo.Verify(); err != nil {
		return err
	}
	open, err := OpenQuestions(repo)
	if err != nil {
		return err
	}
	if len(open) > 0 {
		return fmt.Errorf("%w: %d question(s) are still open, first %s (%s); run /questions",
			ErrNotApprovable, len(open), open[0].ID, oneLine(open[0].Question))
	}
	waiting, err := NeedsReplan(repo)
	if err != nil {
		return err
	}
	if waiting > 0 {
		return fmt.Errorf("%w: %d step(s) wait to be planned again after a changed answer; run /questions to finish that pass",
			ErrNotApprovable, waiting)
	}
	empty, err := EmptyTasks(repo)
	if err != nil {
		return err
	}
	if len(empty) > 0 {
		return fmt.Errorf("%w: %d task(s) have no steps and nothing can decompose them yet: %s",
			ErrNotApprovable, len(empty), strings.Join(clipIDs(empty, 5), ", "))
	}
	// Approve writes status reviewed, which would regress a step that has
	// started or finished.
	var executing []string
	if err := repo.Walk(func(n plantree.Node) error {
		if n.Kind() == plantree.KindStep && (n.Status == plantree.StatusInProgress || n.Status == plantree.StatusCompleted) {
			executing = append(executing, n.ID)
		}
		return nil
	}); err != nil {
		return err
	}
	if len(executing) > 0 {
		return fmt.Errorf("%w: %d step(s) already started or finished and cannot be approved again: %s",
			ErrNotApprovable, len(executing), strings.Join(clipIDs(executing, 5), ", "))
	}
	// An approved plan has nothing left for NextActions to offer, so it would
	// fail the check below. Approve is idempotent, so say yes instead.
	sum, err := repo.Summarize(plantree.RootID)
	if err != nil {
		return err
	}
	if sum.Leaves > 0 && sum.Status == plantree.StatusReviewed {
		return nil
	}
	actions, err := NextActions(repo)
	if err != nil {
		return err
	}
	if len(actions.Blocked) > 0 {
		x := actions.Blocked[0]
		return fmt.Errorf("%w: %s is %s: %s", ErrNotApprovable, x.NodeID, x.Kind, oneLine(x.Reason))
	}
	if len(actions.Runnable) == 0 {
		return fmt.Errorf("%w: the plan has no step to approve", ErrNotApprovable)
	}
	switch {
	case len(actions.Runnable) > 1 || actions.Runnable[0].Kind != plantree.ActionApprove:
		x := actions.Runnable[0]
		return fmt.Errorf("%w: %d action(s) are still outstanding, first %s on %s: %s",
			ErrNotApprovable, len(actions.Runnable), x.Kind, x.NodeID, oneLine(x.Reason))
	}
	return nil
}

// clipIDs shortens a list of ids for an error message.
func clipIDs(ids []string, n int) []string {
	if len(ids) <= n {
		return ids
	}
	return append(append([]string{}, ids[:n]...), fmt.Sprintf("and %d more", len(ids)-n))
}
