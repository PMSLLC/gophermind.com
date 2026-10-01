package executor

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"syscall"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/report"
)

// runFlags are set only by the package tests, through run. afterStart is the
// one test seam: it is called with the started run (Wave 0 done) before any
// wave, so a test can replace or wrap what the run calls.
type runFlags struct {
	skipAcceptance bool
	afterStart     func(*runCtx)
}

// Run is the executor: it builds the approved plan and returns the run Report.
// The error is only for a fault of the harness that stopped the run before it
// could say anything about the plan (the repository cannot be opened, the
// sandbox is refused, the settings are invalid, the approval does not match).
// Every other outcome, failed, escalated, interrupted or blocked, is a Report
// with a status and an exit code, also written to <run>/report.json.
func Run(ctx context.Context, o Options) (Report, error) { return run(ctx, o, runFlags{}) }

func run(ctx context.Context, o Options, f runFlags) (Report, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	started := o.Now()
	// A signal during start (Wave 0, a resume's checks) cancels the start too.
	// This registration overlaps runContext's own until the run context exists,
	// so there is no gap in which a signal would kill the process.
	sctx, stopSig := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSig()
	rc, err := startRun(sctx, o)
	var stop *stopError
	if err != nil {
		se, isStop := stopOf(err)
		switch {
		case rc == nil:
			return Report{}, err
		case isStop:
			stop = se
		case sctx.Err() != nil:
			stop = interruptStop("")
			if ctx.Err() == nil {
				stop = interruptStop("signal")
			}
		default:
			rc.close()
			return Report{}, err
		}
	}
	defer rc.close()
	if f.afterStart != nil {
		f.afterStart(rc)
	}

	rctx, cause, release := rc.runContext(ctx)
	defer release()
	stopSig()

	var fault error
	if stop == nil {
		stop, fault = rc.runWaves(rctx, cause)
	}
	if fault == nil && (stop == nil || stop.Reason != "plan_changed") {
		if se := rc.planIntact(); se != nil {
			stop = se
		}
	}

	var fin finishResult
	switch {
	case fault != nil:
		fin = finishResult{Status: "failed", Reason: "harness_fault"}
	case stop != nil:
		fin = finishResult{Status: stop.Status, Reason: stop.Reason, Failures: stopFailure(stop)}
	default:
		fin, fault = rc.finish(rctx, f)
		if fault != nil {
			fin = finishResult{Status: "failed", Reason: "harness_fault"}
		}
	}
	// An interruption in any stage (waves, acceptance, landing) is named by what
	// ended the run context: the limit, a signal, or the caller.
	if fin.Status == "interrupted" && fin.Reason == "cancelled" && cause() != "" {
		fin.Reason = cause()
	}
	if stop == nil || stop.Reason != "repo_moved" { // a refused resume must not accept the moved branch
		rc.recordTip()
	}

	rc.emit("sandbox", "", fmt.Sprintf("%s; sandbox-exec %s", rc.sandboxLabel(), rc.rep.sandboxExec))
	rep, err := rc.buildReport(rctx, fin, started)
	if err != nil {
		return Report{}, err
	}
	if err := report.Write(rc.o.RunDir, rep, report.WriteOptions{RepoRoot: rc.o.Repo}); err != nil {
		return Report{}, err
	}
	if fault != nil {
		return rep, fault
	}
	return rep, nil
}

// runWaves builds every wave in order. A done context ends the loop as an
// interruption (the claims were released by the leaf loop). The error is a
// harness fault.
func (rc *runCtx) runWaves(ctx context.Context, cause func() string) (*stopError, error) {
	for _, w := range rc.waves() {
		if ctx.Err() != nil {
			return interruptStop(cause()), nil
		}
		stop, err := rc.runWave(ctx, w)
		if err != nil {
			if ctx.Err() != nil {
				return interruptStop(cause()), nil
			}
			return nil, err
		}
		if stop != nil {
			if stop.Status == "interrupted" && stop.Reason == "cancelled" && cause() != "" {
				stop = interruptStop(cause())
			}
			return stop, nil
		}
	}
	if ctx.Err() != nil {
		return interruptStop(cause()), nil
	}
	return nil, nil
}

// baseBranch is the branch the run lands on.
func (rc *runCtx) baseBranch() string {
	if b := rc.plan.Brief.Front.BaseBranch; b != "" {
		return b
	}
	return "main"
}

// planIntact compares the plan files with what was hashed at load, and the
// approval with the plan as it stands. A plan file that changed while the run
// was going stops the run failed with reason plan_changed; the check runs
// before every commit and at the end.
func (rc *runCtx) planIntact() *stopError {
	bad := &stopError{Status: "failed", Reason: "plan_changed", Message: "executor: a plan file changed while the run was in progress"}
	if err := planner.VerifyApproval(rc.o.RunDir); err != nil {
		return bad
	}
	for key, want := range rc.plan.Hashes {
		rel := strings.TrimPrefix(key, "tree/")
		raw, err := readPlanFile(rc.o.RunDir, rel)
		if err != nil || hashHex(raw) != want {
			return bad
		}
	}
	return nil
}

// unfinished names every leaf that is not verified (spec goal 1: no unfinished
// leaf goes unnamed). A failed or escalated leaf gives "<id>: <reason>" with a
// class, "skipped by human" or the last attempt's class. Any other leaf whose
// direct dependency (in id order) is not verified gives "<id>: blocked by
// <dep>" and is counted as blocked; a leaf whose dependencies are all verified
// and whose work began (claimed, in progress, reopened) gives "<id>:
// interrupted: <reason>"; one the run never reached gives "<id>: not_run:
// <reason>". No line holds output text.
func (rc *runCtx) unfinished(rows []blackboard.Row, reason string) (blocked []string, failures []string) {
	status := map[string]blackboard.Status{}
	rowOf := map[string]blackboard.Row{}
	for _, r := range rows {
		status[r.NodeID] = r.Status
		rowOf[r.NodeID] = r
	}
	if reason == "" {
		reason = "run_ended"
	}
	repMu.Lock()
	reasons := make(map[string]string, len(rc.rep.reasons))
	for k, v := range rc.rep.reasons {
		reasons[k] = v
	}
	repMu.Unlock()
	for _, l := range rc.plan.Leaves {
		st, has := status[l.ID]
		if has && st == blackboard.StatusVerified {
			continue
		}
		switch st {
		case blackboard.StatusFailed, blackboard.StatusEscalated:
			r := reasons[l.ID]
			if r == "" {
				r = string(st)
			}
			failures = append(failures, l.ID+": "+r)
			continue
		}
		deps := append([]string(nil), l.DependsOn...)
		sort.Strings(deps)
		var blocker string
		for _, d := range deps {
			if rc.plan.Leaf(d) != nil && status[d] != blackboard.StatusVerified {
				blocker = d
				break
			}
		}
		if blocker != "" {
			blocked = append(blocked, l.ID)
			failures = append(failures, l.ID+": blocked by "+blocker)
			continue
		}
		if touched(rowOf[l.ID]) {
			failures = append(failures, l.ID+": interrupted: "+reason)
			continue
		}
		failures = append(failures, l.ID+": not_run: "+reason)
	}
	return blocked, failures
}

// touched reports that work on a leaf began: it was claimed, is being revised,
// or was reopened (a ready row with attempts or a raised revision).
func touched(r blackboard.Row) bool {
	switch r.Status {
	case blackboard.StatusClaimed, blackboard.StatusInProgress, blackboard.StatusNeedsRevision:
		return true
	case blackboard.StatusReady:
		return r.Revision > 0 || len(r.Attempts) > 0
	}
	return false
}

var safeVersion = regexp.MustCompile(`^[A-Za-z0-9._+~-]{1,64}$`)

// environment is what the run ran on, as the report states it: the sandbox
// setting and whether sandbox-exec was found (recorded in preflight), the
// binary's version and commit, the Go runtime.
func (rc *runCtx) environment() []string {
	version, commit := "unknown", "unknown"
	if bi, ok := debug.ReadBuildInfo(); ok {
		if safeVersion.MatchString(bi.Main.Version) {
			version = bi.Main.Version
		}
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" && safeVersion.MatchString(s.Value) {
				commit = s.Value
			}
		}
	}
	return []string{
		fmt.Sprintf("sandbox: %s (sandbox-exec %s)", rc.sandboxLabel(), rc.rep.sandboxExec),
		fmt.Sprintf("binary: %s commit %s", version, commit),
		fmt.Sprintf("go: %s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH),
	}
}

// buildReport is the one place a Report is made. It reads the ledger, the
// blackboard and the executor's own state files, never a reply, a prompt or
// command output. A run that says verified while a leaf is not verified is
// refused here too: the status becomes failed.
func (rc *runCtx) buildReport(ctx context.Context, fin finishResult, started time.Time) (Report, error) {
	wc := context.WithoutCancel(ctx)
	runID := rc.plan.RunID
	calls, err := rc.o.Ledger.List(wc, runID, ledger.Filter{})
	if err != nil {
		return Report{}, fmt.Errorf("executor: reading the ledger for the report failed")
	}
	rows, err := rc.o.Board.List(wc, runID, blackboard.Filter{})
	if err != nil {
		return Report{}, fmt.Errorf("executor: reading the blackboard for the report failed")
	}
	escalations, err := LoadEscalations(rc.o.RunDir)
	if err != nil {
		return Report{}, err
	}
	// The planner shares the blackboard and keeps a row per tree node: the
	// report counts the plan's leaves.
	var leafRows []blackboard.Row
	for _, r := range rows {
		if rc.plan.Leaf(r.NodeID) != nil {
			leafRows = append(leafRows, r)
		}
	}
	rows = leafRows
	blocked, failures := rc.unfinished(rows, fin.Reason)
	status, reason := fin.Status, fin.Reason
	if status == "verified" && len(failures) > 0 {
		status, reason = "failed", "leaves_not_verified"
	}
	failures = append(failures, fin.Failures...)

	ignored, ignoredTotal, ignoredCut, err := planner.IgnoredDuplicates(rc.o.RunDir)
	if err != nil {
		return Report{}, fmt.Errorf("executor: reading the planner's ignored duplicates failed")
	}
	ledgerErrors := 0
	if rc.o.LedgerErrors != nil {
		ledgerErrors = rc.o.LedgerErrors()
	}
	leafIDs := make([]string, 0, len(rc.plan.Leaves))
	deps := map[string][]string{}
	for _, l := range rc.plan.Leaves {
		leafIDs = append(leafIDs, l.ID)
		deps[l.ID] = append([]string(nil), l.DependsOn...)
	}
	repMu.Lock()
	weak, repairs := rc.rep.weak, rc.rep.repairs
	repMu.Unlock()

	return report.Build(report.Input{
		RunID: runID, StartedAt: started, FinishedAt: rc.o.Now(),
		Status: status, StopReason: reason, Resumed: rc.state.Resumed,
		Sandbox: rc.sandboxLabel(), Environment: rc.environment(),
		RepoBrief: rc.plan.BriefRepo, RepoUsed: rc.o.Repo,
		Calls: calls, Rows: rows, Blocked: blocked,
		Requirements: rc.plan.Requirements, Coverage: rc.plan.Coverage,
		AcceptancePassed: fin.Acc.Passed, AcceptanceTotal: fin.Acc.Total,
		ConstraintsPassed: fin.Acc.ConstraintsPassed, ConstraintsTotal: fin.Acc.ConstraintsTotal,
		Waves: len(rc.waves()), Escalations: escalations,
		WeakTests: weak, Repairs: repairs, LedgerErrors: ledgerErrors,
		Failures: failures, Landing: fin.Landing,
		IgnoredDuplicates: ignored, IgnoredDuplicatesTotal: ignoredTotal, IgnoredDuplicatesTruncated: ignoredCut,
		PlanLeaves: leafIDs, LeafDeps: deps,
	})
}
