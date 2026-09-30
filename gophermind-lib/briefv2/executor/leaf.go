package executor

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/packer"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/report"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/runner"
	"gophermind/gophermind-lib/briefv2/settings"
)

// The previous-failure sentences the model gets when the failure has no
// runner output to show. They are fixed text: nothing of the refused reply is
// ever put into one.
const (
	noteForbidden = "The previous reply was refused before anything was written: it used a file header, a diff, several code fences or another form that is not allowed. Reply with exactly one Go source file in one code fence."
	noteMalformed = "The previous file did not parse or does not satisfy the contract: the package, the exact signature and the allowed declarations. Reply with exactly one Go source file in one code fence."
	noteStray     = "The previous reply was refused: something other than the target file was written while its tests ran. Write only the target file."
	noteCritical  = "The previous reply was refused: a request to a critical network host failed while its tests ran."
)

// noteImports is the sentence for a reply that imports a package the policy
// does not allow. It lists what is allowed and never the offending import.
func noteImports(p packer.ImportPolicy) string {
	parts := []string{"the standard library (without process, syscall, unsafe, plugin and debug packages)"}
	if p.Module != "" {
		parts = append(parts, p.Module+"/...")
	}
	for _, d := range p.Deps {
		parts = append(parts, d+"/...")
	}
	return "The previous reply was refused: one of its imports is not allowed. Allowed imports: " + strings.Join(parts, ", ") + "."
}

// leafIn is what a caller hands runLeaf.
type leafIn struct {
	Previous packer.Failure // seeds the previous-failure text (repair rounds pass integration failure lines, Task 12b)
	Repair   int            // 0 for a normal run; n >= 1 for repair round n (Swap.Reopen, PassRepair)
}

// leafOutcome is how one leaf ended.
type leafOutcome struct {
	Status      blackboard.Status // verified | failed | escalated
	Reason      string            // class, or "skipped by human"
	Interrupted bool              // context done or every entry cooling: claim released, no attempt charged
}

// revResult is how one revision of the ladder ended.
type revResult int

const (
	revVerified        revResult = iota
	revExhausted                 // every entry tried and failed, or was abandoned for a reason the revision cannot cure
	revContractProblem           // the model answered CONTRACT_PROBLEM
	revTooLong                   // every entry was too small for the prompt floor
	revInterrupted               // the context ended, or every entry was cooling or failing with nothing charged
)

// entryEnd is how one entry of the ladder ended within a revision.
type entryEnd int

const (
	endFailed    entryEnd = iota // every attempt of the entry failed
	endIdentical                 // the model repeated its last reply
	endContract
	endTooLong
	endProvider  // the provider was cooling, timed out or failed
	endMalformed // malformed after the router's repair
	endPermanent // auth, missing model, no provider, privacy
	endInterrupted
)

// leafRun is the state of one leaf from claim to a terminal status.
type leafRun struct {
	rc     *runCtx
	l      *Leaf
	swap   *Swap
	in     leafIn
	expect packer.Expect
	worker string

	rev         int
	cp          map[int]int       // contract problems per revision
	lastSHA     map[string]string // entry name -> hash of the last reply on it (memory, else the blackboard)
	tooLong     map[string]bool   // entries whose floor does not fit, for the whole leaf
	history     []string          // historyLine of every attempt, for the revise prompt (Task 11b)
	prev        packer.Failure    // the previous failure text, memory only
	problem     string            // the last CONTRACT_PROBLEM sentence, memory only, never persisted
	charged     int               // VerdictFail attempts of the current revision
	unavailable string            // why a revision ended Interrupted with nothing charged: provider_unavailable or a configuration class
	permClass   string            // the first permanent provider-level class seen
	streakSince time.Time         // proxy failures before this time belong to an earlier attempt

	reopened bool // the leaf was already committed and failed its check: the fix commits as a repair
	passed   bool // the real file is committed: the stub must not be restored
	final    bool // a terminal status was set: the claim must not be released
}

var (
	repMu   sync.Mutex // guards rc.rep.reasons and _state/leaf-results.json
	stateMu sync.Mutex // guards the read-modify-write of rc.state by leaf loops
)

// maxRevisions is the node's budget, else the settings default, plus the
// revisions a human granted.
func (rc *runCtx) maxRevisions(l *Leaf) int {
	n := rc.cfg.Defaults.MaxRevisions
	if l.MaxRevisions >= 0 {
		n = l.MaxRevisions
	}
	stateMu.Lock()
	defer stateMu.Unlock()
	return n + rc.state.ExtraRevisions[l.ID]
}

// contextBudget is the prompt budget of a leaf: the node's, else the brief's,
// else the settings default.
func (rc *runCtx) contextBudget(l *Leaf) int {
	if l.MaxContextTokens > 0 {
		return l.MaxContextTokens
	}
	if b := rc.plan.Brief.Front.Budget; b != nil && b.MaxContextTokens > 0 {
		return b.MaxContextTokens
	}
	return rc.cfg.Defaults.MaxContextTokens
}

func (rc *runCtx) heartbeatEvery() time.Duration {
	if rc.heartbeat > 0 {
		return rc.heartbeat
	}
	return time.Duration(rc.cfg.Executor.HeartbeatSeconds) * time.Second
}

// declaredNames is every package-level name the other files of the leaf's
// package declare: the contract types, the stubs of the other leaves, the
// leaf tests and whatever the other leaves already committed. A helper in the
// reply may not reuse one (packer.Expect.Declared). The leaf's own file and
// stub are left out. The result is never nil: nil would refuse every helper.
func declaredNames(repo string, l *Leaf) ([]string, error) {
	dir := filepath.Join(repo, filepath.FromSlash(l.Dir))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("executor: leaf %s: its package directory is not readable", l.ID)
	}
	own := map[string]bool{path0(l.File): true, path0(l.StubFile): true}
	names := map[string]bool{}
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".go") || own[n] || !e.Type().IsRegular() {
			continue
		}
		src, err := readNoFollow(filepath.Join(dir, n))
		if err != nil {
			return nil, fmt.Errorf("executor: leaf %s: %s is not readable", l.ID, n)
		}
		f, err := parser.ParseFile(token.NewFileSet(), n, src, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("executor: leaf %s: %s does not parse", l.ID, n)
		}
		if f.Name.Name != l.Package {
			continue // another package (an external test package has its own scope)
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					names[d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.TypeSpec:
						names[s.Name.Name] = true
					case *ast.ValueSpec:
						for _, id := range s.Names {
							names[id.Name] = true
						}
					}
				}
			}
		}
	}
	out := make([]string, 0, len(names))
	for n := range names {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

// path0 is the last element of a slash path.
func path0(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// newLeafRun builds the per-leaf state: the reply expectation (with the names
// the package already declares), the swap over the leaf's files and the
// history of attempts already on the blackboard. A signature that does not
// parse is a plan defect and comes back as a *stopError. Nothing is claimed.
func (rc *runCtx) newLeafRun(ctx context.Context, l *Leaf, in leafIn) (*leafRun, error) {
	exp, err := packer.NewExpect(l.Package, l.Signature)
	if err != nil {
		return nil, &stopError{Status: "failed", Reason: "contract_signature",
			Message: fmt.Sprintf("executor: leaf %s: the contract signature does not parse as a function", l.ID)}
	}
	if exp.Declared, err = declaredNames(rc.o.Repo, l); err != nil {
		return nil, err
	}
	stub, err := stubFor(rc.plan.Contracts, rc.policy, l)
	if err != nil {
		return nil, err
	}
	swap := NewSwap(rc.o.Repo, l, rc.git, stub)
	if in.Repair > 0 {
		swap.Reopen()
	}
	lr := &leafRun{
		rc: rc, l: l, swap: swap, in: in, expect: exp, worker: "executor:" + l.ID,
		cp: map[int]int{}, lastSHA: map[string]string{}, tooLong: map[string]bool{}, prev: in.Previous,
	}
	row, err := rc.o.Board.Get(ctx, rc.plan.RunID, l.ID)
	if err != nil {
		return nil, fmt.Errorf("executor: reading the row of %s failed", l.ID)
	}
	lr.rev = row.Revision
	for i, a := range row.Attempts {
		lr.history = append(lr.history, historyLine(i+1, a))
	}
	return lr, nil
}

// runLeaf takes a ready leaf to a terminal status. It expects a `ready` row.
// A stop error ends the run; a plain error is a harness fault and leaves the
// leaf ready again. Every exit either sets a terminal status with a reason
// (verified, failed, escalated) or releases the claim, and the stub is back
// on disk for every leaf that is not verified.
func (rc *runCtx) runLeaf(ctx context.Context, l *Leaf, in leafIn) (out leafOutcome, err error) {
	if ctx.Err() != nil {
		return leafOutcome{Interrupted: true}, nil
	}
	runID := rc.plan.RunID
	worker := "executor:" + l.ID
	ok, cerr := rc.o.Board.Claim(ctx, runID, l.ID, worker)
	if cerr != nil {
		if ctx.Err() != nil {
			return leafOutcome{Interrupted: true}, nil
		}
		return leafOutcome{}, fmt.Errorf("executor: claiming %s failed", l.ID)
	}
	if !ok {
		return leafOutcome{}, fmt.Errorf("executor: leaf %s could not be claimed: another worker owns it or it is not ready", l.ID)
	}

	lr := &leafRun{rc: rc, l: l, worker: worker} // swap is set once the run is built
	hbStop := func() {}
	defer func() {
		hbStop()
		if lr.swap != nil && !lr.passed {
			if ferr := lr.swap.Fail(); ferr != nil && lr.swap.entered && err == nil {
				err = fmt.Errorf("executor: the stub of leaf %s could not be restored", l.ID)
			}
		}
		if !lr.final {
			rctx := context.WithoutCancel(ctx)
			if rerr := rc.o.Board.Release(rctx, runID, l.ID, worker); rerr != nil {
				rc.emit("warning", l.ID, "releasing the claim failed")
			}
		}
	}()

	if serr := rc.o.Board.SetStatus(ctx, runID, l.ID, blackboard.StatusClaimed, blackboard.StatusInProgress); serr != nil {
		if ctx.Err() != nil {
			return leafOutcome{Interrupted: true}, nil
		}
		return leafOutcome{}, fmt.Errorf("executor: starting %s failed", l.ID)
	}
	hbStop = rc.startHeartbeat(ctx, l.ID, worker)
	rc.emit("leaf_started", l.ID, "")

	built, berr := rc.newLeafRun(ctx, l, in)
	if berr != nil {
		return lr.exit(ctx, leafOutcome{}, berr)
	}
	lr = built

	if in.Repair == 0 {
		done, o, aerr := lr.adoptOnDisk(ctx)
		if aerr != nil {
			return lr.exit(ctx, leafOutcome{}, aerr)
		}
		if done {
			return o, nil
		}
		if ctx.Err() != nil {
			return leafOutcome{Interrupted: true}, nil
		}
	}

	for {
		res, rerr := lr.oneRevision(ctx)
		if rerr != nil {
			return lr.exit(ctx, leafOutcome{}, rerr)
		}
		switch res {
		case revVerified:
			return leafOutcome{Status: blackboard.StatusVerified}, nil
		case revInterrupted:
			return leafOutcome{Interrupted: true, Reason: lr.unavailable}, nil
		}
		o, done, aerr := lr.afterRevision(ctx, res)
		if aerr != nil || done {
			return lr.exit(ctx, o, aerr)
		}
	}
}

// startHeartbeat refreshes the claim every interval until the returned
// function is called, which also waits for the goroutine to end.
func (rc *runCtx) startHeartbeat(ctx context.Context, id, worker string) (stop func()) {
	hb, cancel := context.WithCancel(context.WithoutCancel(ctx))
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(rc.heartbeatEvery())
		defer t.Stop()
		for {
			select {
			case <-hb.Done():
				return
			case <-t.C:
				_ = rc.o.Board.Heartbeat(hb, rc.plan.RunID, id, worker)
			}
		}
	}()
	return func() { cancel(); wg.Wait() }
}

// exit turns what ended the loop into runLeaf's result. A leaf whose outcome
// is failed or escalated and whose row is still in progress gets its terminal
// status here, so no exit leaves a leaf silently non-terminal. A stop error
// that says failed fails the leaf; every other error leaves it to be released.
func (lr *leafRun) exit(ctx context.Context, out leafOutcome, err error) (leafOutcome, error) {
	var se *stopError
	if err != nil {
		if errors.As(err, &se) && se.Status == "failed" {
			return lr.fail(ctx, se.Reason, err)
		}
		return leafOutcome{}, err
	}
	switch out.Status {
	case blackboard.StatusFailed:
		return lr.fail(ctx, out.Reason, nil)
	case blackboard.StatusEscalated:
		return lr.escalate(ctx, out)
	}
	if out.Interrupted {
		return out, nil
	}
	lr.final = lr.final || out.Status == blackboard.StatusVerified
	return out, nil
}

// inProgress reports that the row still says in_progress.
func (lr *leafRun) inProgress(ctx context.Context) bool {
	row, err := lr.rc.o.Board.Get(ctx, lr.rc.plan.RunID, lr.l.ID)
	return err == nil && row.Status == blackboard.StatusInProgress
}

// fail sets the leaf failed with a reason: status, recorded reason, event.
func (lr *leafRun) fail(ctx context.Context, reason string, cause error) (leafOutcome, error) {
	rc, l := lr.rc, lr.l
	if lr.swap != nil && !lr.passed {
		if ferr := lr.swap.Fail(); ferr != nil && lr.swap.entered {
			return leafOutcome{}, fmt.Errorf("executor: the stub of leaf %s could not be restored", l.ID)
		}
	}
	if lr.inProgress(ctx) {
		if serr := rc.o.Board.SetStatus(context.WithoutCancel(ctx), rc.plan.RunID, l.ID, blackboard.StatusInProgress, blackboard.StatusFailed); serr != nil {
			return leafOutcome{}, fmt.Errorf("executor: marking %s failed did not work", l.ID)
		}
	}
	lr.final = true
	if rerr := rc.setResult(l.ID, blackboard.StatusFailed, reason); rerr != nil {
		return leafOutcome{}, rerr
	}
	rc.emit("leaf_failed", l.ID, reason)
	return leafOutcome{Status: blackboard.StatusFailed, Reason: reason}, cause
}

// escalate makes sure an escalated leaf is escalated on the blackboard.
func (lr *leafRun) escalate(ctx context.Context, out leafOutcome) (leafOutcome, error) {
	rc, l := lr.rc, lr.l
	if lr.inProgress(ctx) {
		wc := context.WithoutCancel(ctx)
		for _, to := range []blackboard.Status{blackboard.StatusNeedsRevision, blackboard.StatusEscalated} {
			from := blackboard.StatusInProgress
			if to == blackboard.StatusEscalated {
				from = blackboard.StatusNeedsRevision
			}
			if serr := rc.o.Board.SetStatus(wc, rc.plan.RunID, l.ID, from, to); serr != nil {
				return leafOutcome{}, fmt.Errorf("executor: escalating %s did not work", l.ID)
			}
		}
	}
	lr.final = true
	if rerr := rc.setResult(l.ID, blackboard.StatusEscalated, out.Reason); rerr != nil {
		return leafOutcome{}, rerr
	}
	rc.emit("leaf_escalated", l.ID, out.Reason)
	return out, nil
}

// afterRevision decides what follows a revision that did not verify (rungs 5
// to 7). Too long for every entry escalates at once; a contract problem goes
// to the revise rung once per revision and escalates the second time; an
// ordinary exhaustion goes to the revise rung while the revision allowance is
// not used up, else escalates with the class of the last attempt. It returns
// done == false when the ladder is to run again (after a revise, or after a
// person chose retry).
func (lr *leafRun) afterRevision(ctx context.Context, res revResult) (out leafOutcome, done bool, err error) {
	rc, l := lr.rc, lr.l
	if res == revInterrupted {
		return leafOutcome{Interrupted: true, Reason: lr.unavailable}, true, nil
	}
	reason, doRevise := "", false
	switch res {
	case revTooLong:
		reason = ClassContextTooLong
	case revContractProblem:
		if lr.cp[lr.rev] >= 2 || lr.rev >= rc.maxRevisions(l) {
			reason = ClassContractProblem
		} else {
			doRevise = true
		}
	default:
		if lr.rev < rc.maxRevisions(l) {
			doRevise = true
		} else {
			reason = lr.lastClass(ctx)
		}
	}
	if doRevise {
		cp, rerr := lr.revise(ctx)
		var rs *reviseStop
		switch {
		case errors.As(rerr, &rs) && rs.Interrupted:
			return leafOutcome{Interrupted: true, Reason: rs.Reason}, true, nil
		case errors.As(rerr, &rs):
			reason = rs.Reason
		case rerr != nil:
			return leafOutcome{}, true, rerr
		case cp:
			reason = ClassContractProblem
		default:
			return leafOutcome{}, false, nil
		}
	}
	return lr.toHuman(ctx, reason)
}

// lastClass is the class of the last attempt of the leaf: the text of its
// failure reason before the first colon.
func (lr *leafRun) lastClass(ctx context.Context) string {
	row, err := lr.rc.o.Board.Get(ctx, lr.rc.plan.RunID, lr.l.ID)
	if err != nil || len(row.Attempts) == 0 {
		return "ladder_exhausted"
	}
	class, _, _ := strings.Cut(row.Attempts[len(row.Attempts)-1].FailureReason, ":")
	if class == "" {
		return "ladder_exhausted"
	}
	return class
}

// toHuman is rung 7 for a leaf in progress: escalate, then act on the answer.
// retry claims the leaf again and the ladder restarts; skip ends it failed;
// stop and a missing or waiting gate end the run with the leaf escalated.
func (lr *leafRun) toHuman(ctx context.Context, reason string) (leafOutcome, bool, error) {
	rc, l := lr.rc, lr.l
	res, err := rc.escalate(ctx, l, blackboard.StatusInProgress, reason, lr.history)
	if err != nil {
		var se *stopError
		switch {
		case errors.As(err, &se):
			lr.final = true // escalated is the row's final status for this run: nothing to release
			return leafOutcome{}, true, err
		case ctx.Err() != nil:
			lr.final = true
			return leafOutcome{Interrupted: true}, true, nil
		}
		return leafOutcome{}, true, err
	}
	switch res.Action {
	case human.ActionSkip:
		lr.final = true
		return leafOutcome{Status: blackboard.StatusFailed, Reason: reasonSkipped}, true, nil
	case human.ActionRetry:
		lr.cp[lr.rev] = 0
		ok, cerr := rc.o.Board.Claim(ctx, rc.plan.RunID, l.ID, lr.worker)
		if cerr != nil || !ok {
			return leafOutcome{}, true, fmt.Errorf("executor: leaf %s could not be claimed again after the retry", l.ID)
		}
		if serr := rc.o.Board.SetStatus(ctx, rc.plan.RunID, l.ID, blackboard.StatusClaimed, blackboard.StatusInProgress); serr != nil {
			return leafOutcome{}, true, fmt.Errorf("executor: restarting %s failed", l.ID)
		}
		return leafOutcome{}, false, nil
	}
	lr.final = true
	return leafOutcome{}, true, humanStop(l.ID)
}

// adoptOnDisk is spec 7.2 and 10 step 5: a real file already on disk is
// checked before any model call. A pass ends the leaf verified (a commit is
// made unless the file is already committed). A failure seeds the previous
// failure with the runner's output, in memory, at no model cost. done is true
// when the leaf ended here.
func (lr *leafRun) adoptOnDisk(ctx context.Context) (done bool, out leafOutcome, err error) {
	rc, l := lr.rc, lr.l
	if err := lr.swap.Normalize(); err != nil {
		return false, out, fmt.Errorf("executor: leaf %s: its files could not be put in order", l.ID)
	}
	st, err := lr.swap.State()
	if err != nil {
		return false, out, err
	}
	if st != SwapRealOnly {
		return false, out, nil
	}
	dirty, err := rc.git.Dirty()
	if err != nil {
		return false, out, errors.New("executor: the repository state could not be read")
	}
	committed, hash := false, ""
	if !inList(dirty, l.File) {
		h, found, err := rc.git.LeafCommit(l.ID)
		if err != nil {
			return false, out, fmt.Errorf("executor: looking for the commit of %s failed", l.ID)
		}
		committed, hash = found, h
	}
	src, err := readNoFollow(filepath.Join(rc.o.Repo, filepath.FromSlash(l.File)))
	if err != nil {
		return false, out, fmt.Errorf("executor: the file of leaf %s is not readable", l.ID)
	}
	if committed {
		lr.swap.Reopen() // Fail restores the committed content
		lr.reopened = true
	}
	// Enter the file's own bytes so the swap owns it: a failed check then
	// restores the stub (or the committed file) through the one code path.
	if err := lr.swap.Enter(src); err != nil {
		return false, out, fmt.Errorf("executor: leaf %s: its file on disk could not be taken over", l.ID)
	}
	// The file is held to the gate a model reply passes before anything runs it.
	if class, note := lr.gate(src); class != "" {
		rc.emit("adopt_refused", l.ID, class)
		lr.prev = failureText(note)
		if err := lr.swap.Fail(); err != nil {
			return false, out, fmt.Errorf("executor: the stub of leaf %s could not be restored", l.ID)
		}
		return false, out, nil
	}
	snap, err := TakeSnapshot(rc.o.Repo, rc.git)
	if err != nil {
		return false, out, err
	}
	start := time.Now()
	v := settle(rc.checkLeaf(ctx, l))
	stray, err := lr.cleanStray(snap)
	if err != nil {
		return false, out, err
	}
	if v.Faulted() {
		if ctx.Err() != nil {
			return false, leafOutcome{Interrupted: true}, nil
		}
		return false, out, fmt.Errorf("executor: the check of leaf %s could not run: %w", l.ID, v.Err)
	}
	crit := rc.criticalSince(l.ID, start)
	switch {
	case len(stray) > 0:
		lr.prev = failureText(noteStray)
	case len(crit) > 0:
		lr.prev = failureText(noteCritical)
	case !v.Pass():
		lr.prev = failureOf(v, rc.secretValues())
	default:
		if err := lr.finishPass(ctx, committed, hash); err != nil {
			return false, out, err
		}
		return true, leafOutcome{Status: blackboard.StatusVerified}, nil
	}
	if err := lr.swap.Fail(); err != nil {
		return false, out, fmt.Errorf("executor: the stub of leaf %s could not be restored", l.ID)
	}
	return false, out, nil
}

// gate runs on a file's bytes what a reply passes before it is written: the
// reply gate (parse, package, signature, declarations, process control), the
// import policy and the source scan. It returns the failure class and the
// fixed sentence for the next prompt, or "" when the file passes.
func (lr *leafRun) gate(src []byte) (class, note string) {
	r, err := packer.ParseReply(string(src), lr.expect)
	switch {
	case err != nil:
		return runner.ClassMalformed, noteMalformed
	case r.ContractProblem != "" || r.Forbidden != "":
		return ClassForbiddenWrite, noteForbidden
	case len(lr.rc.plan.Policy().Check(r.Imports)) > 0:
		return ClassImportNotAllowed, noteImports(lr.rc.plan.Policy())
	}
	if findings, serr := ScanGoSource(lr.l.File, src); serr != nil || len(findings) > 0 {
		return ClassForbiddenWrite, noteForbidden
	}
	return "", ""
}

func inList(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// failureText is a previous failure that is one fixed sentence.
func failureText(s string) packer.Failure { return packer.Failure{Lines: []string{s}} }

// cleanStray removes every file the check wrote that is not the leaf's own
// file or stub (ignored files included) and returns what it removed. The
// declared files are the only ones a leaf may change.
func (lr *leafRun) cleanStray(snap Snapshot) ([]string, error) {
	rc, l := lr.rc, lr.l
	stray, err := snap.Stray(rc.o.Repo, rc.git, l.File, l.StubFile)
	if err != nil {
		return nil, err
	}
	if len(stray) > 0 {
		if err := Revert(rc.git, stray); err != nil {
			return nil, fmt.Errorf("executor: removing the files a check wrote outside leaf %s failed", l.ID)
		}
	}
	return stray, nil
}

// criticalSince is the hosts of critical-host failures of the leaf since t.
// With no proxy (an offline run, or a test that built none) there is no
// evidence to read: it returns nothing, and the attempt is judged by the check
// alone.
func (rc *runCtx) criticalSince(id string, t time.Time) []string {
	if rc.prox == nil {
		return nil
	}
	var hosts []string
	seen := map[string]bool{}
	for _, f := range rc.prox.Failures(id, t) {
		if !seen[f.Host] {
			seen[f.Host] = true
			hosts = append(hosts, f.Host)
		}
	}
	sort.Strings(hosts)
	return hosts
}

// finishPass is the done definition met: commit (unless adopted), record the
// result and set verified. Once the commit exists the stub must never be
// restored, so passed is set before anything that can still fail.
func (lr *leafRun) finishPass(ctx context.Context, committed bool, hash string) error {
	rc, l := lr.rc, lr.l
	if !committed {
		var err error
		if lr.in.Repair > 0 || lr.reopened {
			round := lr.in.Repair
			if round < 1 {
				round = 1
			}
			hash, err = lr.swap.PassRepair(round)
		} else {
			hash, err = lr.swap.Pass(l.Title)
		}
		if err != nil {
			return fmt.Errorf("executor: committing leaf %s failed", l.ID)
		}
	}
	lr.passed = true
	wc := context.WithoutCancel(ctx)
	if err := rc.o.Board.SetResult(wc, rc.plan.RunID, l.ID, blackboard.Result{FilesChanged: []string{l.File, l.StubFile}, Commit: hash}); err != nil {
		return fmt.Errorf("executor: recording the result of %s failed", l.ID)
	}
	if err := rc.o.Board.SetStatus(wc, rc.plan.RunID, l.ID, blackboard.StatusInProgress, blackboard.StatusVerified); err != nil {
		return fmt.Errorf("executor: marking %s verified failed", l.ID)
	}
	lr.final = true
	if err := rc.setResult(l.ID, blackboard.StatusVerified, ""); err != nil {
		return err
	}
	rc.emit("leaf_verified", l.ID, "")
	return nil
}

// previousSHA is the hash of the last reply of this leaf on the entry: from
// memory, else from the blackboard's last attempt of the same provider and
// model that has one. Empty when there is none. A blackboard that cannot be
// read is an error: guessing "no previous reply" would lose the detection.
func (lr *leafRun) previousSHA(ctx context.Context, e ladderEntry) (string, error) {
	if s, ok := lr.lastSHA[e.Name]; ok {
		return s, nil
	}
	prov, model, _ := settings.SplitEntry(e.Name)
	row, err := lr.rc.o.Board.Get(ctx, lr.rc.plan.RunID, lr.l.ID)
	if err != nil {
		return "", fmt.Errorf("executor: reading the attempts of %s failed", lr.l.ID)
	}
	sha := ""
	for i := len(row.Attempts) - 1; i >= 0; i-- {
		a := row.Attempts[i]
		if a.Provider == prov && a.Model == model && a.ReplySHA256 != "" {
			sha = a.ReplySHA256
			break
		}
	}
	lr.lastSHA[e.Name] = sha
	return sha, nil
}

// noteEscalation records the move to a later model of the ladder, when the
// model is about to be called: once per model per leaf, however many
// revisions follow, and never for a model that was skipped.
func (lr *leafRun) noteEscalation(e ladderEntry) error {
	if e.Pos <= 1 {
		return nil
	}
	list, err := LoadEscalations(lr.rc.o.RunDir)
	if err != nil {
		return err
	}
	for _, x := range list {
		if x.Kind == "model" && x.NodeID == lr.l.ID && x.Model == e.Name {
			return nil
		}
	}
	return AppendEscalation(lr.rc.o.RunDir, report.Escalation{Kind: "model", TaskType: "implement", Model: e.Name, NodeID: lr.l.ID})
}

// oneRevision is rungs 1 to 4 for revision lr.rev: for each entry in order
// the initial attempt and then up to executor.fix_attempts fixes, each with
// the previous failure; then the next model; then, past the leaf's own tier,
// the strong chain.
func (lr *leafRun) oneRevision(ctx context.Context) (revResult, error) {
	rc := lr.rc
	entries := rc.ladderEntries(lr.l)
	lr.charged = 0
	lr.unavailable = ""
	var tooLong, providerN, permanentN int
	for _, e := range entries {
		if lr.tooLong[e.Name] {
			tooLong++
			continue
		}
		end, err := lr.runEntry(ctx, e)
		if err != nil {
			return revExhausted, err
		}
		switch end {
		case endInterrupted:
			return revInterrupted, nil
		case endContract:
			return revContractProblem, nil
		case endTooLong:
			tooLong++
		case endProvider:
			providerN++
		case endPermanent:
			permanentN++
		default:
			if lr.passed {
				return revVerified, nil
			}
		}
	}
	switch {
	case lr.passed:
		return revVerified, nil
	case lr.charged > 0:
		return revExhausted, nil
	case len(entries) > 0 && tooLong == len(entries):
		return revTooLong, nil
	case providerN > 0:
		// Nothing was charged and a provider was cooling or failing: no model
		// was at fault, and the leaf can be tried again on a later run.
		lr.unavailable = "provider_unavailable"
		return revInterrupted, nil
	case permanentN > 0:
		// A configuration fault (credentials, a missing model, privacy): the
		// true class is reported, and it is not the leaf's failure.
		lr.unavailable = lr.permClass
		return revInterrupted, nil
	}
	return revExhausted, nil
}

// runEntry is the initial attempt and the fix attempts of one entry.
func (lr *leafRun) runEntry(ctx context.Context, e ladderEntry) (entryEnd, error) {
	for k := 0; k <= lr.rc.cfg.Executor.FixAttempts; k++ {
		end, done, err := lr.attempt(ctx, e, k)
		if err != nil {
			return end, err
		}
		if lr.passed {
			return endFailed, nil
		}
		if done {
			return end, nil
		}
	}
	return endFailed, nil
}

// record appends one attempt to the blackboard. It holds the model, the
// verdict, counts, the failure reason and the reply hash: never text.
func (lr *leafRun) record(ctx context.Context, e ladderEntry, started time.Time, verdict blackboard.Verdict, class, reason string, v *runner.Verdict, sha string) error {
	rc := lr.rc
	prov, model, _ := settings.SplitEntry(e.Name)
	a := blackboard.Attempt{
		Model: model, Provider: prov, Revision: lr.rev, Order: e.Pos, StartedAt: started,
		DurationMS: rc.o.Now().Sub(started).Milliseconds(), Verdict: verdict, FailureReason: reason, ReplySHA256: sha,
	}
	if v != nil && v.Events > 0 {
		a.TestsTotal = 1
		if v.Pass() {
			a.TestsPassed = 1
		}
		a.FailedTests = v.Names
	}
	if err := rc.o.Board.AppendAttempt(context.WithoutCancel(ctx), rc.plan.RunID, lr.l.ID, a); err != nil {
		return fmt.Errorf("executor: recording an attempt of %s failed", lr.l.ID)
	}
	lr.history = append(lr.history, historyLine(len(lr.history)+1, a))
	if sha != "" {
		lr.lastSHA[e.Name] = sha
	}
	if verdict == blackboard.VerdictFail {
		lr.charged++
	}
	rc.emit("attempt", lr.l.ID, fmt.Sprintf("entry %s %s %s", e.Name, verdict, class))
	return nil
}

// tooLongEnd records an entry that cannot hold the prompt: no provider call,
// an error attempt, and the entry is skipped for the rest of the leaf.
func (lr *leafRun) tooLongEnd(ctx context.Context, e ladderEntry, started time.Time) (entryEnd, bool, error) {
	lr.tooLong[e.Name] = true
	reason := failureReason(ClassContextTooLong, []string{e.Name})
	if err := lr.record(ctx, e, started, blackboard.VerdictError, ClassContextTooLong, reason, nil, ""); err != nil {
		return endTooLong, true, err
	}
	return endTooLong, true, nil
}

// attempt is one model call and everything that follows from its reply. done
// is true when the entry is finished (whatever the reason); false means the
// attempt failed, consumed a fix and the entry may try again.
func (lr *leafRun) attempt(ctx context.Context, e ladderEntry, k int) (end entryEnd, done bool, err error) {
	rc, l := lr.rc, lr.l
	if ctx.Err() != nil {
		return endInterrupted, true, nil
	}
	started := rc.o.Now()

	packed, tooLong, err := lr.pack(e)
	if err != nil {
		return endFailed, true, err
	}
	if tooLong {
		return lr.tooLongEnd(ctx, e, started)
	}
	max, _ := packer.MaxTokens(e.ContextTokens, packed.Tokens)
	req := provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: packer.SystemPrefix + "implement:" + l.ID},
			{Role: provider.RoleUser, Content: packed.Text},
		},
		MaxTokens: max, Temperature: fixTemperature(k),
	}
	if grown := e.ContextTokens - packed.Tokens; e.ContextTokens > 0 && grown > max {
		if grown > 16384 {
			grown = 16384
		}
		req.MaxGrownTokens = grown
	}
	info := router.CallInfo{
		RunID: rc.plan.RunID, Stage: "implement:" + l.ID, NodeID: l.ID, Tier: e.Tier, Scope: router.ScopeNode,
		Revision: lr.rev, TaskType: "implement", NodeClass: l.Class, Only: e.Name,
	}
	var reply packer.Reply
	lastSHA := "" // hash of the last reply the parse saw, good or not
	parse := func(text string) error {
		r, perr := packer.ParseReply(text, lr.expect)
		reply, lastSHA = r, r.SHA256
		return perr
	}
	if err := lr.noteEscalation(e); err != nil {
		return endFailed, true, err
	}
	if _, cerr := rc.o.Caller.CallParsed(ctx, info, req, parse); cerr != nil {
		return lr.providerError(ctx, e, started, cerr, lastSHA)
	}

	// A repeat of the previous reply on this entry, whatever the gate would say
	// of it, is identical: hashed first so a refused reply cannot burn every fix.
	if reply.ContractProblem == "" {
		if same, err := lr.identical(ctx, e, started, reply.SHA256); err != nil || same {
			return endIdentical, true, err
		}
	}
	switch {
	case reply.ContractProblem != "":
		lr.problem = reply.ContractProblem
		lr.cp[lr.rev]++
		if err := lr.record(ctx, e, started, blackboard.VerdictFail, ClassContractProblem, failureReason(ClassContractProblem, nil), nil, reply.SHA256); err != nil {
			return endContract, true, err
		}
		return endContract, true, nil
	case reply.Forbidden != "":
		return lr.refused(ctx, e, started, reply, ClassForbiddenWrite, failureText(noteForbidden))
	}
	if bad := rc.plan.Policy().Check(reply.Imports); len(bad) > 0 {
		return lr.refused(ctx, e, started, reply, ClassImportNotAllowed, failureText(noteImports(rc.plan.Policy())))
	}
	if findings, serr := ScanGoSource(l.File, reply.Source); serr != nil || len(findings) > 0 {
		return lr.refused(ctx, e, started, reply, ClassForbiddenWrite, failureText(noteForbidden))
	}
	return lr.check(ctx, e, started, reply)
}

// identical reports whether sha repeats the last reply of this leaf on the
// entry. When it does, the attempt is recorded as a failed identical_reply.
func (lr *leafRun) identical(ctx context.Context, e ladderEntry, started time.Time, sha string) (bool, error) {
	prev, err := lr.previousSHA(ctx, e)
	if err != nil {
		return false, err
	}
	if prev == "" || prev != sha {
		return false, nil
	}
	err = lr.record(ctx, e, started, blackboard.VerdictFail, ClassIdentical, failureReason(ClassIdentical, nil), nil, sha)
	return true, err
}

// refused records an attempt whose reply was refused before being written.
// It consumes a fix attempt.
func (lr *leafRun) refused(ctx context.Context, e ladderEntry, started time.Time, reply packer.Reply, class string, next packer.Failure) (entryEnd, bool, error) {
	if err := lr.record(ctx, e, started, blackboard.VerdictFail, class, failureReason(class, nil), nil, reply.SHA256); err != nil {
		return endFailed, true, err
	}
	lr.prev = next
	return endFailed, false, nil
}

// pack builds the prompt of one attempt. tooLong is true when even the
// entry's whole window cannot hold the floor of the prompt.
func (lr *leafRun) pack(e ladderEntry) (p packer.Packed, tooLong bool, err error) {
	rc, l := lr.rc, lr.l
	testSrc, err := lr.testSource()
	if err != nil {
		return p, false, err
	}
	notes, err := LoadNotes(rc.o.RunDir)
	if err != nil {
		return p, false, err
	}
	in := packer.Inputs{Failure: lr.prev, Notes: notes[l.ID], Budget: rc.contextBudget(l), Secrets: rc.secretValues()}
	view := rc.plan.View(l, testSrc)
	p, err = packer.Pack(view, rc.plan.Contracts, in)
	var floor *packer.ErrFloorOverBudget
	if errors.As(err, &floor) && e.ContextTokens-512 > in.Budget {
		in.Budget = e.ContextTokens - 512
		p, err = packer.Pack(view, rc.plan.Contracts, in)
	}
	if errors.As(err, &floor) {
		return p, true, nil
	}
	if err != nil {
		return p, false, fmt.Errorf("executor: leaf %s: the prompt could not be built: %w", l.ID, err)
	}
	if _, ok := packer.MaxTokens(e.ContextTokens, p.Tokens); !ok {
		return p, true, nil
	}
	return p, false, nil
}

// testSource reads the leaf's test file and proves it is the file the planner
// recorded: a test edited between planning and running stops the run.
func (lr *leafRun) testSource() (string, error) {
	rc, l := lr.rc, lr.l
	raw, err := readNoFollow(filepath.Join(rc.o.Repo, filepath.FromSlash(l.TestFile)))
	if err != nil {
		return "", fmt.Errorf("executor: the test file of leaf %s is not readable", l.ID)
	}
	if hashHex(raw) != l.TestSHA256 {
		return "", &stopError{Status: "failed", Reason: "test_file_changed",
			Message: fmt.Sprintf("executor: the test file of leaf %s changed since it was planned", l.ID)}
	}
	return string(raw), nil
}

// providerError is what a failed router call means for the entry.
func (lr *leafRun) providerError(ctx context.Context, e ladderEntry, started time.Time, cerr error, lastSHA string) (entryEnd, bool, error) {
	var ce *router.ChainExhausted
	if ctx.Err() != nil || errors.Is(cerr, context.Canceled) {
		return endInterrupted, true, nil
	}
	if !errors.As(cerr, &ce) {
		return endFailed, true, fmt.Errorf("executor: the model call for leaf %s failed: %w", lr.l.ID, cerr)
	}
	class, end := ClassTimeout, endProvider
	switch {
	case ce.ParseErr != nil:
		// Every reply of the entry was unusable. The last one is hashed; one
		// that repeats the entry's previous reply is an identical reply.
		if same, err := lr.identical(ctx, e, started, lastSHA); err != nil || same {
			return endIdentical, true, err
		}
		class, end = runner.ClassMalformed, endMalformed
	case len(ce.Reasons) > 0:
		switch kind := ce.Reasons[0].Kind; kind {
		case router.ReasonTooLong:
			return lr.tooLongEnd(ctx, e, started)
		case router.ReasonCooldown:
			class = ClassRateLimited
		case router.ReasonFailed:
			class = failedClass(ce.Reasons[0].Detail)
		default:
			class, end = kind, endPermanent
			if lr.permClass == "" {
				lr.permClass = kind
			}
		}
	default:
		class, end = "no_model", endPermanent
	}
	sha := ""
	if end == endMalformed {
		sha = lastSHA
	}
	if err := lr.record(ctx, e, started, blackboard.VerdictError, class, failureReason(class, nil), nil, sha); err != nil {
		return end, true, err
	}
	return end, true, nil
}

// failedClass names why the router's "failed" entry failed, from the fixed
// detail text the router gives it.
func failedClass(detail string) string {
	switch {
	case strings.HasPrefix(detail, "timed out"):
		return ClassTimeout
	case strings.HasPrefix(detail, "reply truncated"):
		return "truncated"
	case strings.HasPrefix(detail, "empty reply"):
		return "empty_reply"
	}
	return "provider_failed"
}

// check writes the reply, runs the leaf check, looks for strays and critical
// network failures, and decides the attempt: a pass is committed, anything
// else puts the stub back and feeds the failure to the next attempt.
func (lr *leafRun) check(ctx context.Context, e ladderEntry, started time.Time, reply packer.Reply) (entryEnd, bool, error) {
	rc, l := lr.rc, lr.l
	snap, err := TakeSnapshot(rc.o.Repo, rc.git)
	if err != nil {
		return endFailed, true, err
	}
	lr.streakSince = time.Now()
	if err := lr.swap.Enter(reply.Source); err != nil {
		return endFailed, true, fmt.Errorf("executor: writing the file of leaf %s failed", l.ID)
	}
	v := settle(rc.checkLeaf(ctx, l))
	stray, err := lr.cleanStray(snap)
	if err != nil {
		return endFailed, true, err
	}
	if v.Faulted() {
		if ctx.Err() != nil || v.Class == runner.ClassCancelled {
			return endInterrupted, true, nil
		}
		return endFailed, true, fmt.Errorf("executor: the check of leaf %s could not run: %w", l.ID, v.Err)
	}
	for _, w := range rc.warningsSince(l.ID, lr.streakSince) {
		rc.emit("warning", l.ID, w)
	}
	crit := rc.criticalSince(l.ID, lr.streakSince)
	terminal, err := rc.observeCritical(l.ID, len(crit) > 0)
	if err != nil {
		return endFailed, true, err
	}

	var (
		class, reason string
		next          packer.Failure
	)
	switch {
	case len(stray) > 0:
		class, next = ClassForbiddenWrite, failureText(noteStray)
		reason = failureReason(class, nil)
	case len(crit) > 0:
		class, next = ClassNetworkCritical, failureText(noteCritical)
		reason = failureReason(class, crit)
	case !v.Pass():
		class, next = v.Class, failureOf(v, rc.secretValues())
		reason = v.Reason()
	default:
		if err := lr.finishPass(ctx, false, ""); err != nil {
			return endFailed, true, err
		}
		if err := lr.record(ctx, e, started, blackboard.VerdictPass, "pass", "", &v, reply.SHA256); err != nil {
			return endFailed, true, err
		}
		return endFailed, true, nil
	}
	if err := lr.record(ctx, e, started, blackboard.VerdictFail, class, reason, &v, reply.SHA256); err != nil {
		return endFailed, true, err
	}
	if err := lr.swap.Fail(); err != nil {
		return endFailed, true, fmt.Errorf("executor: the stub of leaf %s could not be restored", l.ID)
	}
	lr.prev = next
	if terminal {
		return endFailed, true, &stopError{Status: "failed", Reason: ClassNetworkCritical,
			Message: fmt.Sprintf("executor: leaf %s failed three attempts in a row on a critical network host", l.ID)}
	}
	return endFailed, false, nil
}

// warningsSince is one line per failed request to a non-critical host.
func (rc *runCtx) warningsSince(id string, t time.Time) []string {
	if rc.prox == nil {
		return nil
	}
	var out []string
	for _, w := range rc.prox.Warnings(id, t) {
		out = append(out, fmt.Sprintf("a request to %s failed (%s)", w.Host, w.Kind))
	}
	return out
}

// observeCritical counts one checked attempt toward the critical streak of the
// node (S10) and saves it. It returns true on the third in a row. An attempt
// that never reached a check says nothing about the network and is not counted.
func (rc *runCtx) observeCritical(id string, failed bool) (bool, error) {
	terminal := rc.streak.Observe(id, failed)
	stateMu.Lock()
	defer stateMu.Unlock()
	cur := rc.streak.Export()
	if !failed && rc.state.CriticalStreak[id] == 0 {
		return terminal, nil
	}
	rc.state.CriticalStreak = cur
	if err := rc.state.Save(rc.o.RunDir); err != nil {
		return terminal, err
	}
	return terminal, nil
}
