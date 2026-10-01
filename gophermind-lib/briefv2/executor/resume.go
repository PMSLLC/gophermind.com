package executor

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
)

// resumeMovedMessage is the fixed text of a resume refused because the work
// branch is no longer where this run left it.
const resumeMovedMessage = "executor: the repository moved since this run stopped (the work branch no longer contains the commits this run made); nothing was changed"

// resumeBaseMovedMessage is the fixed text of a resume refused early because
// landing could no longer be a fast-forward.
const resumeBaseMovedMessage = "executor: the base branch moved since this run started, so the work branch can no longer be fast-forwarded onto it; nothing was changed"

// maxDirtShown is how many foreign paths a stop names.
const maxDirtShown = 5

// tipNow is the work branch tip, or "" when there is none to record
// (diff_only makes no branch and no commit). Git being unable to say is an error.
func (rc *runCtx) tipNow() (string, error) {
	if rc.diffOnly || rc.state.Branch == "" {
		return "", nil
	}
	head, err := rc.git.Head()
	if err != nil {
		return "", errors.New("executor: the work branch tip could not be read")
	}
	return head, nil
}

// recordTip saves the work branch tip in the state file. It runs after every
// commit the run makes and when the run ends, so a later resume can prove the
// branch only grew by this run's own commits. A tip that cannot be read or
// saved is a harness fault: a silent skip would let a later resume accept
// foreign commits.
func (rc *runCtx) recordTip() error {
	tip, err := rc.tipNow()
	if err != nil || tip == "" {
		return err
	}
	stateMu.Lock()
	defer stateMu.Unlock()
	if rc.state.Tip == tip {
		return nil
	}
	prev := rc.state.Tip
	rc.state.Tip = tip
	if err := rc.state.Save(rc.o.RunDir); err != nil {
		rc.state.Tip = prev
		return err
	}
	return nil
}

// resume is spec 10 steps 1 to 4 for a run whose _state/executor.json exists
// and whose Wave 0 is done (a run that stopped inside Wave 0 is re-entered by
// retryWave0). A plan change and a live claim are harness faults (plain
// errors, nothing is modified); a moved repository and foreign dirt are stops.
// Nothing here calls a model: steps 5 and 6 are the leaf loop's, which finds
// every released leaf ready and runs its checks on the file already on disk
// before any call.
func (rc *runCtx) resume(ctx context.Context) error {
	if !reflect.DeepEqual(rc.state.PlanHashes, rc.plan.Hashes) {
		return errors.New("executor: plan files changed since the run started")
	}
	runID := rc.plan.RunID
	ids := make([]string, 0, len(rc.plan.Leaves))
	waves := map[string]int{}
	for _, l := range rc.plan.Leaves {
		ids = append(ids, l.ID)
		waves[l.ID] = l.Wave
	}
	if err := rc.o.Board.InitRun(ctx, runID, ids, waves); err != nil {
		return errors.New("executor: the blackboard could not be initialised")
	}
	released, err := rc.o.Board.ReleaseStale(ctx, runID, time.Duration(rc.cfg.Executor.StaleClaimSeconds)*time.Second)
	if err != nil {
		return errors.New("executor: releasing stale claims failed")
	}
	rows, err := rc.o.Board.List(ctx, runID, blackboard.Filter{})
	if err != nil {
		return errors.New("executor: the blackboard could not be read")
	}
	var live []string
	verified, past := 0, false
	for _, r := range rows {
		if rc.plan.Leaf(r.NodeID) == nil {
			continue // the planner's rows for the tree's other nodes
		}
		switch r.Status {
		case blackboard.StatusClaimed, blackboard.StatusInProgress:
			live = append(live, r.NodeID) // still held after the sweep: its heartbeat is fresh
		case blackboard.StatusVerified:
			verified++
		}
		if r.Status != blackboard.StatusPending {
			past = true
		}
	}
	if len(live) > 0 {
		sort.Strings(live)
		// A killed worker's claim goes stale after stale_claim_seconds: say how
		// long to wait (ids and seconds only), then refuse.
		stale := time.Duration(rc.cfg.Executor.StaleClaimSeconds) * time.Second
		var wait time.Duration
		for _, r := range rows {
			if inList(live, r.NodeID) {
				if left := stale - rc.o.Now().Sub(r.HeartbeatAt); left > wait {
					wait = left
				}
			}
		}
		rc.emit("resume_blocked", "", fmt.Sprintf("claims held by a live heartbeat: %s; wait up to %d s for them to go stale", strings.Join(live, ", "), int(wait.Seconds())+1))
		return fmt.Errorf("executor: node %s is held by a live worker", live[0])
	}
	listed := "none"
	if len(released) > 0 {
		listed = strings.Join(released, ", ")
	}
	rc.emit("resume", "", fmt.Sprintf("released claims: %s; %d of %d leaves verified", listed, verified, len(rc.plan.Leaves)))

	if !rc.diffOnly && rc.state.Branch != "" {
		if err := rc.git.Start(rc.baseBranch(), rc.state.Branch, nil); err != nil {
			return errors.New("executor: the work branch of this run could not be checked out")
		}
		if rc.state.Tip != "" {
			ok, err := rc.git.IsAncestor(rc.state.Tip)
			if err != nil || !ok {
				return &stopError{Status: "failed", Reason: "repo_moved", Message: resumeMovedMessage}
			}
		}
		// Everything after the recorded tip (everything after the base when none
		// was recorded) must be this run's own commit: a kill between a commit
		// and the tip record leaves such commits, a person's commit is foreign.
		n, err := rc.git.ForeignCommits(rc.state.Tip)
		if err != nil {
			return errors.New("executor: the commits of the work branch could not be read")
		}
		if n > 0 {
			return &stopError{Status: "failed", Reason: "repo_moved",
				Message: fmt.Sprintf("executor: the work branch holds %d commit(s) this run did not make; nothing was changed", n)}
		}
		// A base that moved makes the final fast-forward impossible: refuse now,
		// not after the whole build.
		if ok, err := rc.git.CanFastForward(); err != nil {
			return errors.New("executor: the base branch could not be compared with the work branch")
		} else if !ok {
			return &stopError{Status: "failed", Reason: "base_moved", Message: resumeBaseMovedMessage}
		}
	}
	// A diff_only repair that was cut off leaves its candidate on disk and the
	// verified file under _state: put the verified file back first.
	if err := restorePriors(rc.o.Repo, rc.o.RunDir, rc.plan.Leaves); err != nil {
		return err
	}
	dirty, err := rc.git.Dirty()
	if err != nil {
		return errors.New("executor: the repository state could not be read")
	}
	if foreign := rc.foreignDirt(dirty, released); len(foreign) > 0 {
		shown := foreign
		if len(shown) > maxDirtShown {
			shown = shown[:maxDirtShown]
		}
		return &stopError{Status: "failed", Reason: "foreign_dirt",
			Message: fmt.Sprintf("executor: the working tree holds %d path(s) that belong to no released leaf, so nothing was changed: %s", len(foreign), strings.Join(shown, ", "))}
	}

	if past && !rc.state.Resumed {
		rc.state.Resumed = true
		return rc.state.Save(rc.o.RunDir)
	}
	return nil
}

// foreignDirt returns the dirty paths that belong to no released leaf (not its
// contract file, not its stub) and, while Wave 0 is not done, not one of Wave
// 0's paths. With diff_only nothing is ever committed, so the declared files
// of every leaf and Wave 0's own are the result and are never foreign. The
// result is sorted; nothing is stashed, reset or discarded by this check.
func (rc *runCtx) foreignDirt(dirty []string, released []string) []string {
	own := map[string]bool{}
	mine := func(l *Leaf) {
		own[l.File] = true
		own[l.StubFile] = true
	}
	for _, id := range released {
		if l := rc.plan.Leaf(id); l != nil {
			mine(l)
		}
	}
	if rc.diffOnly {
		for _, l := range rc.plan.Leaves {
			mine(l)
		}
	}
	if rc.diffOnly || !rc.state.Wave0Done {
		for _, p := range rc.wave0Paths() {
			own[p] = true
		}
	}
	var out []string
	for _, p := range dirty {
		if !own[p] {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}
