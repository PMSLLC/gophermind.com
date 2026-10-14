package executor

import (
	"context"
	"errors"
	"fmt"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/packer"
)

// landResult is how a landing attempt ended.
type landResult int

const (
	landPassed      landResult = iota // committed through finishPass
	landConflict                      // a charged failed attempt: the ladder goes on in the main tree
	landInterrupted                   // the context ended: the candidate stays for the next run
)

// privatePass is a pass in a private tree (spec section 6): the reply is saved
// as the leaf's candidate, the stub is put back in the tree, and the ladder
// stops. No attempt is recorded: the blackboard sees the pass at landing.
func (lr *leafRun) privatePass(ctx context.Context, e ladderEntry, reply packer.Reply) (entryEnd, bool, error) {
	c := candidate{Revision: lr.rev, Entry: e.Name, Tier: string(e.Tier), Pos: e.Pos, SHA256: reply.SHA256, Source: string(reply.Source)}
	if err := saveCandidate(lr.rc.o.RunDir, lr.l.ID, c); err != nil {
		return endFailed, true, err
	}
	lr.rc.emit("leaf_candidate", lr.l.ID, "passed in its tree; waiting for its turn")
	if err := lr.swap.Fail(); err != nil {
		return endFailed, true, fmt.Errorf("executor: the stub of leaf %s could not be put back in its tree", lr.l.ID)
	}
	lr.cand = &c
	lr.art = attemptArt{}
	return endFailed, true, nil
}

// toMain moves a leaf from its private tree to the main tree once its turn
// came: the swap, the checker and the declared names are the main tree's from
// here on (the names now include what earlier leaves of the level landed). The
// private tree is removed by the job when the leaf ends. In the main tree it
// does nothing.
func (lr *leafRun) toMain(ctx context.Context) error {
	if !lr.wt.Private {
		return nil
	}
	if lr.turn == nil {
		return errors.New("executor: a leaf in a private tree has no turn")
	}
	if err := lr.turn.wait(ctx); err != nil {
		return err
	}
	main := lr.rc.mainTree()
	declared, err := declaredNames(main.Repo, lr.l)
	if err != nil {
		return err
	}
	swap := NewSwap(main.Repo, lr.l, main.Git, lr.stub)
	if lr.rc.diffOnly {
		swap.NoCommit(lr.rc.o.RunDir)
	}
	lr.wt, lr.swap, lr.expect.Declared = main, swap, declared
	return nil
}

// land replays a saved candidate in the main tree (spec section 7.2): the
// reply gate again (ruling 9), then the same check a model reply gets (ruling
// C4): the stub swap, gofmt, build, vet, the named test, the stray-file check
// and the critical-host streak. A pass commits through finishPass exactly as a
// serial pass does, and finishPass says leaf_landed. Any charged failure is a
// landing conflict (section 7.3): the attempt is recorded with its real class,
// the stub is back, the candidate is removed, a land_conflict event is emitted
// and the failure is fed to the next attempt. A context that ended keeps the
// candidate for the next run.
func (lr *leafRun) land(ctx context.Context, c candidate) (landResult, error) {
	rc, l := lr.rc, lr.l
	e := c.entry()
	started := rc.o.Now()
	src := []byte(c.Source)
	conflict := func(err error) (landResult, error) {
		if rerr := removeCandidate(rc.o.RunDir, l.ID); rerr != nil && err == nil {
			err = rerr
		}
		rc.noteConflict()
		rc.emit("land_conflict", l.ID, lr.failClass)
		return landConflict, err
	}
	if class, note := lr.gate(src); class != "" {
		// Nothing of a refused candidate is stored: no source, no output.
		lr.art = attemptArt{}
		if err := lr.record(ctx, e, started, blackboard.VerdictFail, class, failureReason(class, nil), nil, c.SHA256); err != nil {
			return landConflict, err
		}
		lr.prev = failureText(note)
		return conflict(nil)
	}
	lr.art = attemptArt{source: src}
	charged := lr.charged
	lr.landing = true
	end, _, err := lr.check(ctx, e, started, packer.Reply{Source: src, SHA256: c.SHA256})
	lr.landing = false
	switch {
	case lr.passed:
		return landPassed, err
	case lr.charged > charged:
		return conflict(err) // a terminal critical streak still ends the leaf after the bookkeeping
	case err != nil:
		return landConflict, err // a harness fault: the candidate stays
	case end == endInterrupted:
		return landInterrupted, nil // the stub is restored by runLeaf; the candidate stays in _state
	}
	return landConflict, fmt.Errorf("executor: the landing of leaf %s ended without a verdict", l.ID)
}

// landCandidate is the leaf's turn: take the main tree, read the saved
// candidate again (it may have changed while the leaf waited, ruling C2) and
// replay it. done is true when the leaf ended (verified, or interrupted with
// its candidate kept); false when there was no candidate to land, or after a
// conflict, when the ladder continues in the main tree from its first entry
// (ruling 8). A candidate of an earlier revision is dropped with a warning
// (ruling 10).
func (lr *leafRun) landCandidate(ctx context.Context) (leafOutcome, bool, error) {
	rc, l := lr.rc, lr.l
	lr.cand = nil
	if err := lr.toMain(ctx); err != nil {
		if ctx.Err() != nil || errors.Is(err, errLandingFault) {
			return leafOutcome{Interrupted: true}, true, nil
		}
		return leafOutcome{}, true, err
	}
	c, found, err := readCandidate(rc.o.RunDir, l.ID)
	if err != nil {
		return leafOutcome{}, true, err
	}
	if !found {
		return leafOutcome{}, false, nil
	}
	if c.Revision != lr.rev {
		rc.emit("warning", l.ID, "a saved candidate of an earlier revision was dropped")
		return leafOutcome{}, false, removeCandidate(rc.o.RunDir, l.ID)
	}
	res, err := lr.land(ctx, c)
	switch {
	case err != nil:
		return leafOutcome{}, true, err
	case res == landPassed:
		return leafOutcome{Status: blackboard.StatusVerified}, true, nil
	case res == landInterrupted:
		return leafOutcome{Interrupted: true}, true, nil
	}
	return leafOutcome{}, false, nil
}

// resumeCandidate lands a candidate an earlier process saved (spec section 11
// step 3), with no model call. It runs in the main tree. done is false when
// there was none, when it was stale, or after a landing conflict.
func (lr *leafRun) resumeCandidate(ctx context.Context) (leafOutcome, bool, error) {
	return lr.landCandidate(ctx)
}

// noteConflict counts one landing conflict for the report.
func (rc *runCtx) noteConflict() {
	repMu.Lock()
	rc.rep.conflicts++
	repMu.Unlock()
}
