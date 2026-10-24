package executor

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"gophermind/gophermind-lib/build/packer"
	"gophermind/gophermind-lib/build/planner"
	"gophermind/gophermind-lib/build/revlog"
	"gophermind/gophermind-lib/build/runfs"
	"gophermind/gophermind-lib/build/settings"
)

const (
	maxProblemBytes = 300
	maxHistoryLines = 30
)

// rewriteMode says this leaf uses the planner's revise stage. A leaf in an
// integration repair round (running, or reopened and resumed) has a committed
// real file and no stub, so there is nothing to red-check a new test against:
// it keeps the notes rung.
func (lr *leafRun) rewriteMode() bool {
	return lr.rc.cfg.Revise.Mode == settings.ReviseModeRewrite && lr.in.Repair == 0 && !lr.reopened
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// failureLines renders a failure as the implement prompt does: the failing
// names, then the condensed output lines.
func failureLines(f packer.Failure) string {
	var parts []string
	if len(f.Names) > 0 {
		parts = append(parts, "Failing tests: "+strings.Join(f.Names, ", "))
	}
	return strings.Join(append(parts, f.Lines...), "\n")
}

// logRevise appends a flag or refused record to the node's revision log.
func (lr *leafRun) logRevise(r revlog.Record) error {
	dir, err := revlog.NodeDir(lr.rc.o.RunDir, lr.l.ID)
	if err != nil {
		return errPlanChanged()
	}
	r.At = runfs.TS(lr.rc.o.Now())
	return lr.rc.plan.chainWrite(func() error { return revlog.Append(dir, r) })
}

// afterRewrite resets the per-revision state of the leaf run: the ladder
// restarts at rung 2 from the revised node with no memory of the old failures.
// The names the package declares are read again: the new test may add helpers
// that a reply must not redeclare.
func (lr *leafRun) afterRewrite(ctx context.Context) error {
	row, err := lr.rc.o.Board.Get(ctx, lr.rc.plan.RunID, lr.l.ID)
	if err != nil {
		return fmt.Errorf("executor: reading the row of %s failed", lr.l.ID)
	}
	lr.cut = len(row.Attempts) // the row counter may be ahead of the node, so revision numbers cannot tell old attempts from new
	declared, err := declaredNames(lr.rc.o.Repo, lr.l)
	if err != nil {
		return err
	}
	lr.expect.Declared = declared
	lr.rev = lr.l.Revision
	lr.cp = map[int]int{}
	lr.lastSHA = map[string]string{}
	lr.tooLong = map[string]bool{}
	lr.prev = packer.Failure{}
	lr.problem = ""
	lr.charged = 0
	lr.refusals = nil
	lr.proposal = ""
	return nil
}

// rewrite is rung 5 in rewrite mode (spec section 4). It returns nil when the
// node was rewritten and the ladder is to run again, or a *reviseStop for a
// leaf that goes to the gate (a flag, or refusals used up) or is interrupted.
// A flag or an unusable reply consumes no revision.
func (lr *leafRun) rewrite(ctx context.Context, trigger string) error {
	rc, l := lr.rc, lr.l
	lr.refusals, lr.proposal = nil, ""
	for {
		testSrc, err := lr.testSource()
		if err != nil {
			return err
		}
		history := rc.scrubHistory(lr.history)
		if len(history) > maxHistoryLines {
			history = history[len(history)-maxHistoryLines:]
		}
		req := planner.ReviseRequest{
			RunID: rc.plan.RunID, RunDir: rc.o.RunDir, Repo: rc.o.Repo, NodeID: l.ID, Trigger: trigger,
			Revision: l.Revision, Remaining: rc.maxRevisions(l) - l.Revision,
			History: history, LastFailure: rc.scrubText(failureLines(lr.prev)),
			TestSource: testSrc, TestSHA256: l.TestSHA256, Refused: append([]string(nil), lr.refusals...), Secrets: rc.secretValues(),
		}
		if d, ok := rc.redOut.Load(l.ID); ok && len(lr.refusals) > 0 && lr.refusals[len(lr.refusals)-1] == planner.ClassRedBuild {
			req.RefusedDetail, _ = d.(string)
		}
		if trigger == planner.TriggerContractProblem {
			req.Problem = clip(rc.scrubText(lr.problem), maxProblemBytes)
		}
		rc.emit("revise_started", l.ID, trigger)
		prop, err := rc.rev.Revise(ctx, req)
		if err != nil {
			return lr.reviseFailure(ctx, err)
		}
		refusal := ""
		switch prop.Outcome {
		case planner.OutcomeRewritten:
			class, aerr := rc.applyRevision(ctx, l, prop, trigger)
			if aerr != nil {
				return aerr
			}
			if class == "" {
				return lr.afterRewrite(ctx)
			}
			refusal = class // applyRevision wrote the refused record and told the sink
		case planner.OutcomeFlagged:
			lr.proposal = prop.Paragraph
			if err := lr.logRevise(revlog.Record{Kind: revlog.KindFlag, Class: prop.Class, Trigger: trigger}); err != nil {
				return err
			}
			rc.emit("contract_flagged", l.ID, prop.Class)
			return &reviseStop{Reason: prop.Class}
		default:
			refusal = prop.Class
			if err := lr.logRevise(revlog.Record{Kind: revlog.KindRefused, N: l.Revision + 1, Class: refusal, Trigger: trigger}); err != nil {
				return err
			}
			rc.emit("revise_refused", l.ID, refusal)
		}
		lr.refusals = append(lr.refusals, refusal)
		if len(lr.refusals) >= rc.cfg.Revise.MaxRefusals {
			return &reviseStop{Reason: refusal}
		}
	}
}
