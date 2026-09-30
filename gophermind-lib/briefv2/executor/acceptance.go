package executor

// The acceptance stage (spec 9) and the one place a run's final status is
// decided (spec 7.1). Binaries are built once, outside the working tree's
// tracked files, and every root test of the brief's acceptance bullets (and of
// the constraints that have one) runs against them. The proof keeps each
// command's exit code and the SHA-256 of what it printed, never the output.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/gitland"
	"gophermind/gophermind-lib/briefv2/packer"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/report"
	"gophermind/gophermind-lib/briefv2/runner"
)

// fileAcceptance is the proof file in the run folder.
const fileAcceptance = "acceptance.json"

// Counts is "N of M".
type Counts struct {
	Passed int `json:"passed"`
	Total  int `json:"total"`
}

// CommandProof is the proof of one root test command. No output text is stored.
type CommandProof struct {
	Test        string `json:"test"`    // the root test's name
	Command     string `json:"command"` // fixed text with ids only ("root test A1 #1"): the command itself is in coverage.json
	ExitCode    int    `json:"exit_code"`
	TimedOut    bool   `json:"timed_out"`
	DurationMS  int64  `json:"duration_ms"`
	OutputBytes int    `json:"output_bytes"`  // runner.Output.Size()
	OutputSHA   string `json:"output_sha256"` // runner.Output.SHA256(), over every byte the command wrote
	Passed      bool   `json:"passed"`
}

// BulletProof is one requirement (acceptance bullet or checked constraint).
type BulletProof struct {
	Requirement string         `json:"requirement"` // A1, C2, ...
	Text        string         `json:"text"`
	Commands    []CommandProof `json:"commands"`
	Verdict     string         `json:"verdict"` // "pass" or "fail"
}

// AcceptanceFile is <run>/acceptance.json.
type AcceptanceFile struct {
	RequirementsCovered Counts        `json:"requirements_covered"` // copied from coverage.json and requirements.json
	Acceptance          Counts        `json:"acceptance"`
	ConstraintsChecked  Counts        `json:"constraints_checked"`
	Round               int           `json:"round"` // acceptance repair rounds used
	Bullets             []BulletProof `json:"bullets"`
	Constraints         []BulletProof `json:"constraints"`
}

// acceptPlan is what code, not the planner's file alone, says must be proven.
type acceptPlan struct {
	acceptance  []planBullet // requirement kind acceptance, requirement order
	constraints []planBullet // constraints that have at least one root test
}

type planBullet struct {
	req   planner.Requirement
	tests []planner.RootTest // root tests whose Requirement == req.ID, coverage order
	nodes []string           // coverage.json covered[].nodes for the requirement
}

// mapAcceptance computes N and the tests from requirements.json (reqs) and
// coverage.json (cov). It never reads N from coverage.json. It errors, naming
// the requirement id, for an acceptance requirement with no root test or with a
// root test whose command is blank (spec 9 tripwire).
func mapAcceptance(reqs []planner.Requirement, cov planner.CoverageFile) (acceptPlan, error) {
	nodes := map[string][]string{}
	for _, c := range cov.Covered {
		nodes[c.Requirement] = c.Nodes
	}
	var p acceptPlan
	for _, rq := range reqs {
		if rq.Kind != planner.ReqAcceptance && rq.Kind != planner.ReqConstraint {
			continue
		}
		var tests []planner.RootTest
		for _, t := range cov.RootTests {
			if t.Requirement == rq.ID {
				tests = append(tests, t)
			}
		}
		if len(tests) == 0 {
			if rq.Kind == planner.ReqAcceptance {
				return acceptPlan{}, fmt.Errorf("executor: acceptance requirement %s has no root test, so it cannot be proven", rq.ID)
			}
			continue // a constraint with no root test is covered by a node and is not counted
		}
		for _, t := range tests {
			if strings.TrimSpace(t.Command) == "" {
				return acceptPlan{}, fmt.Errorf("executor: requirement %s has a root test with no command", rq.ID)
			}
		}
		pb := planBullet{req: rq, tests: tests, nodes: append([]string(nil), nodes[rq.ID]...)}
		if rq.Kind == planner.ReqAcceptance {
			p.acceptance = append(p.acceptance, pb)
		} else {
			p.constraints = append(p.constraints, pb)
		}
	}
	return p, nil
}

var binNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// buildBinaries builds every main package under cmd/ to <bin>/<dirname> with
// go build -o, through rc.goCmd in envLeaf mode (sandboxed, offline). The
// directory is the scratch one: the sandbox denies writes to the run folder.
func (rc *runCtx) buildBinaries(ctx context.Context) error {
	res := rc.goCmd(ctx, "acceptance", envLeaf, "list", "-f", "{{.Name}} {{.ImportPath}}", "./cmd/...")
	if res.Canceled || ctx.Err() != nil {
		return ctxErrOr(ctx)
	}
	if res.Err != nil {
		return fmt.Errorf("executor: listing the packages to build failed (%w)", res.Err)
	}
	if res.ExitCode != 0 {
		return &stopError{Status: "failed", Reason: "acceptance_build",
			Message: "executor: listing the cmd packages for the acceptance run failed"}
	}
	module := rc.plan.Contracts.Module
	for _, line := range strings.Split(res.Out.Text(), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 || f[0] != "main" {
			continue
		}
		rel, ok := strings.CutPrefix(f[1], module+"/")
		if !ok || !binNameRE.MatchString(filepath.Base(rel)) {
			return &stopError{Status: "failed", Reason: "acceptance_build",
				Message: "executor: a cmd package has a path the acceptance build cannot name"}
		}
		out := filepath.Join(rc.binDir, filepath.Base(rel))
		b := rc.goCmd(ctx, "acceptance", envLeaf, "build", "-o", out, "./"+rel)
		if b.Canceled || ctx.Err() != nil {
			return ctxErrOr(ctx)
		}
		if b.Err != nil || b.ExitCode != 0 {
			return &stopError{Status: "failed", Reason: "acceptance_build",
				Message: "executor: go build of ./" + rc.scrubText(rel) + " for the acceptance run failed"}
		}
	}
	return nil
}

func ctxErrOr(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return context.Canceled
}

// runRootTest runs one root test command with sh -c from the repo root through
// rc.chk.Run. The runner.Result carries the output in memory only; the
// CommandProof carries hashes.
func (rc *runCtx) runRootTest(ctx context.Context, t planner.RootTest) (CommandProof, runner.Result, error) {
	env, err := rc.env("acceptance", envAcceptance)
	if err != nil {
		return CommandProof{}, runner.Result{}, errors.New("executor: the environment of the acceptance run could not be built")
	}
	res := rc.chk.Run(ctx, runner.Spec{
		Dir: rc.o.Repo, Argv: runner.Shell(t.Command), Env: env,
		Timeout: rc.acceptanceTimeout(),
	})
	if res.Canceled || (res.Err == nil && ctx.Err() != nil) {
		return CommandProof{}, res, ctxErrOr(ctx)
	}
	if res.Err != nil {
		return CommandProof{}, res, fmt.Errorf("executor: an acceptance command could not be started (%w)", res.Err)
	}
	p := CommandProof{
		Test: rc.scrubText(t.Name), ExitCode: res.ExitCode, TimedOut: res.TimedOut,
		DurationMS: res.Duration.Milliseconds(), OutputBytes: res.Out.Size(), OutputSHA: res.Out.SHA256(),
		Passed: res.ExitCode == 0 && !res.TimedOut,
	}
	return p, res, nil
}

type failedBullet struct {
	Req      planner.Requirement
	Command  string
	ExitCode int
	TimedOut bool
	Env      bool // the failure is the environment (no database), not a leaf
	Nodes    []string
	Failure  packer.Failure // packer.NewFailure(nil, output, secrets): capped, secret-stripped, memory only
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

var (
	dbRefusedHints = []string{"connection refused", "could not connect", "no such host", "i/o timeout", "econnrefused", "connection reset"}
	dbContextHints = []string{"postgres", "5432", "pq:", "pgx", "database"}
)

// environmentFault reports a failure that is the database being unreachable:
// the brief declares a database secret and the output says a connection to a
// database failed. It reads the output in memory and keeps nothing of it.
func (rc *runCtx) environmentFault(out string) bool {
	declared := false
	for _, s := range rc.plan.Brief.Front.Secrets {
		if s.Name == "TEST_DATABASE_URL" || s.Name == "DATABASE_URL" {
			declared = true
		}
	}
	if !declared {
		return false
	}
	low := strings.ToLower(out)
	has := func(hints []string) bool {
		for _, h := range hints {
			if strings.Contains(low, h) {
				return true
			}
		}
		return false
	}
	return has(dbRefusedHints) && has(dbContextHints)
}

func (rc *runCtx) runBullet(ctx context.Context, pb planBullet) (BulletProof, *failedBullet, error) {
	bp := BulletProof{Requirement: pb.req.ID, Text: rc.scrubText(pb.req.Text), Commands: []CommandProof{}, Verdict: "pass"}
	var fb *failedBullet
	for i, t := range pb.tests {
		proof, res, err := rc.runRootTest(ctx, t)
		if err != nil {
			return bp, nil, err
		}
		proof.Command = fmt.Sprintf("root test %s #%d", pb.req.ID, i+1)
		bp.Commands = append(bp.Commands, proof)
		if proof.Passed {
			continue
		}
		bp.Verdict = "fail"
		if fb == nil {
			text := res.Out.Text()
			fb = &failedBullet{
				Req: pb.req, Command: proof.Command, ExitCode: proof.ExitCode, TimedOut: proof.TimedOut,
				Env: rc.environmentFault(text), Nodes: pb.nodes, Failure: packer.NewFailure(nil, text, rc.secretValues()),
			}
		}
	}
	if len(bp.Commands) == 0 { // unreachable through mapAcceptance; never a pass
		bp.Verdict = "fail"
		fb = &failedBullet{Req: pb.req, ExitCode: -1, Nodes: pb.nodes}
	}
	return bp, fb, nil
}

// acceptanceRun runs every bullet once, in requirement order, one command at a
// time, writes acceptance.json, and returns the failing bullets by requirement
// id. A cancelled context returns its error and writes nothing.
func (rc *runCtx) acceptanceRun(ctx context.Context, p acceptPlan, round int) (AcceptanceFile, map[string]failedBullet, error) {
	file := AcceptanceFile{
		RequirementsCovered: Counts{Passed: len(rc.plan.Coverage.Covered), Total: len(rc.plan.Requirements)},
		Acceptance:          Counts{Total: len(p.acceptance)},
		ConstraintsChecked:  Counts{Total: len(p.constraints)},
		Round:               round, Bullets: []BulletProof{}, Constraints: []BulletProof{},
	}
	failed := map[string]failedBullet{}
	do := func(list []planBullet, into *[]BulletProof, c *Counts) error {
		for _, pb := range list {
			bp, fb, err := rc.runBullet(ctx, pb)
			if err != nil {
				return err
			}
			*into = append(*into, bp)
			if bp.Verdict == "pass" {
				c.Passed++
				rc.emit("acceptance_bullet", pb.req.ID, "pass")
				continue
			}
			failed[pb.req.ID] = *fb
			rc.emit("acceptance_bullet", pb.req.ID, "fail")
		}
		return nil
	}
	if err := do(p.acceptance, &file.Bullets, &file.Acceptance); err != nil {
		return AcceptanceFile{}, nil, err
	}
	if err := do(p.constraints, &file.Constraints, &file.ConstraintsChecked); err != nil {
		return AcceptanceFile{}, nil, err
	}
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return file, failed, errors.New("executor: encoding acceptance.json failed")
	}
	if err := report.WriteFile(rc.o.RunDir, fileAcceptance, append(raw, '\n'), report.WriteOptions{RepoRoot: rc.o.Repo}); err != nil {
		return file, failed, err
	}
	return file, failed, nil
}

func counts(f AcceptanceFile) accResult {
	return accResult{Passed: f.Acceptance.Passed, Total: f.Acceptance.Total,
		ConstraintsPassed: f.ConstraintsChecked.Passed, ConstraintsTotal: f.ConstraintsChecked.Total}
}

// failedIDs is the failing requirement ids: acceptance bullets first, then
// constraints, each in requirement order.
func (rc *runCtx) failedIDs(p acceptPlan, failed map[string]failedBullet) (ids []string, acceptanceFailed bool) {
	for _, pb := range p.acceptance {
		if _, bad := failed[pb.req.ID]; bad {
			ids = append(ids, pb.req.ID)
			acceptanceFailed = true
		}
	}
	for _, pb := range p.constraints {
		if _, bad := failed[pb.req.ID]; bad {
			ids = append(ids, pb.req.ID)
		}
	}
	return ids, acceptanceFailed
}

// acceptance runs acceptanceRun with the repair rounds and returns the counts
// or the stop. It returns nil only when every acceptance bullet passed and
// every checked constraint passed. The counts are those of the last run.
func (rc *runCtx) acceptance(ctx context.Context) (accResult, *stopError, error) {
	p := rc.accept
	bound := rc.cfg.Executor.AcceptanceRepairRounds
	for round := 0; ; round++ {
		file, failed, err := rc.acceptanceRun(ctx, p, round)
		if err != nil {
			if se, ok := stopOf(err); ok {
				return accResult{Total: len(p.acceptance), ConstraintsTotal: len(p.constraints)}, se, nil
			}
			return accResult{Total: len(p.acceptance), ConstraintsTotal: len(p.constraints)}, nil, err
		}
		acc := counts(file)
		if len(failed) == 0 {
			rc.emit("acceptance_passed", "", fmt.Sprintf("Acceptance passed: %d of %d", acc.Passed, acc.Total))
			return acc, nil, nil
		}
		ids, accFailed := rc.failedIDs(p, failed)
		for _, id := range ids {
			if failed[id].Env {
				return acc, &stopError{Status: "failed", Reason: "acceptance_environment",
					Message: fmt.Sprintf("executor: acceptance %s could not reach its database (an environment fault, not a leaf failure)", id)}, nil
			}
		}
		if round >= bound {
			return acc, rc.acceptStop(ids, failed, accFailed), nil
		}
		if se, err := rc.acceptRepair(ctx, round+1, ids, failed); err != nil || se != nil {
			return acc, se, err
		}
	}
}

// acceptStop is the stop after the repair rounds are used up: ids and text of
// the first failing bullet and its exit code, then the other ids. Never output.
func (rc *runCtx) acceptStop(ids []string, failed map[string]failedBullet, accFailed bool) *stopError {
	first := failed[ids[0]]
	how := fmt.Sprintf("command exit %d", first.ExitCode)
	if first.TimedOut {
		how = "command timed out"
	}
	what, reason := "acceptance", "acceptance_failed"
	if !accFailed {
		what, reason = "constraint", "constraint_failed"
	}
	msg := fmt.Sprintf("executor: %s %s failed: %s; %s", what, ids[0], rc.scrubText(firstLine(first.Req.Text)), how)
	if len(ids) > 1 {
		msg += "; also failing: " + strings.Join(ids[1:], ", ")
	}
	return &stopError{Status: "failed", Reason: reason, Message: msg}
}

// attribute is the leaves that own a failing bullet: the leaf nodes the
// coverage file lists for it, or, when it lists none at all (the planner maps
// an acceptance bullet to no node), the leaves whose contract package is main,
// which make the built binaries the bullet exercises. Empty means the bullet
// cannot be attributed.
func (rc *runCtx) attribute(b failedBullet) []string {
	var ids []string
	for _, n := range b.Nodes {
		if rc.plan.Leaf(n) != nil {
			ids = append(ids, n)
		}
	}
	if len(b.Nodes) == 0 {
		for _, l := range rc.plan.Leaves {
			if l.Package == "main" {
				ids = append(ids, l.ID)
			}
		}
	}
	sort.Strings(ids)
	return ids
}

// acceptRepair is one acceptance repair round (spec 9 Failure): every leaf that
// owns a failing bullet is reopened and rebuilt with only its own bullets'
// capped output as the previous failure, then the last wave is checked again.
func (rc *runCtx) acceptRepair(ctx context.Context, round int, ids []string, failed map[string]failedBullet) (*stopError, error) {
	byNode := map[string][]packer.Failure{}
	var lost []string
	for _, id := range ids {
		nodes := rc.attribute(failed[id])
		if len(nodes) == 0 {
			lost = append(lost, id)
		}
		for _, n := range nodes {
			byNode[n] = append(byNode[n], failed[id].Failure)
		}
	}
	if len(lost) > 0 {
		return &stopError{Status: "failed", Reason: "unattributable",
			Message: "executor: acceptance failed for " + strings.Join(lost, ", ") + ", which no leaf owns"}, nil
	}
	nodes := make([]string, 0, len(byNode))
	for n := range byNode {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	for _, id := range nodes {
		l := rc.plan.Leaf(id)
		row, err := rc.o.Board.Get(ctx, rc.plan.RunID, id)
		if err != nil {
			return rc.faultOrStop(fmt.Errorf("executor: reading the row of %s failed", id))
		}
		if row.Status != blackboard.StatusVerified {
			return repairStop("failed", "repair_impossible", "executor: acceptance failed in the files of leaf %s, which is %s and cannot be repaired", id, row.Status), nil
		}
		if err := rc.reopen(ctx, l, "acceptance"); err != nil {
			return rc.faultOrStop(err)
		}
		out, err := rc.runLeaf(ctx, l, leafIn{Previous: mergeFailures(byNode[id]...), Repair: round})
		if err != nil {
			return rc.faultOrStop(err)
		}
		switch {
		case out.Interrupted:
			return interruptStop(out.Reason), nil
		case out.Status == blackboard.StatusEscalated:
			return humanStop(id), nil
		case out.Status != blackboard.StatusVerified:
			return repairStop("failed", "repair_failed", "executor: leaf %s could not be repaired for acceptance in round %d (%s)", id, round, out.Reason), nil
		}
	}
	ws := rc.waves()
	last := ws[len(ws)-1]
	res, err := rc.waveChecks(ctx, last, true)
	if err != nil {
		return rc.faultOrStop(err)
	}
	if res.Pass() {
		return nil, nil
	}
	if un := rc.plan.Attribute(res).Unattributed; len(un) > 0 {
		return unattributableStop(last, un), nil
	}
	return rc.repairWave(ctx, last, res, true)
}

func (rc *runCtx) acceptanceTimeout() time.Duration {
	return time.Duration(rc.cfg.Executor.AcceptanceTimeoutSeconds) * time.Second
}

// accResult is what the acceptance stage proved.
type accResult struct{ Passed, Total, ConstraintsPassed, ConstraintsTotal int }

// finishResult is how the run ended once every wave was done: the final status,
// a reason class, the acceptance counts, extra failure lines (acceptance
// bullets, never output text) and the landing.
type finishResult struct {
	Status, Reason string
	Acc            accResult
	Failures       []string
	Landing        *report.Landing
}

// decide is the first test of the final status: verified only when every leaf
// row is verified. A row that is not verified, or cannot be read, never yields
// verified: escalated when a leaf is escalated, else failed with the first
// reason.
func (rc *runCtx) decide() (status, reason string) {
	ctx := context.Background()
	escalated, first := false, ""
	for _, l := range rc.plan.Leaves {
		row, err := rc.o.Board.Get(ctx, rc.plan.RunID, l.ID)
		if err != nil {
			return "failed", "state_unreadable"
		}
		if row.Status == blackboard.StatusVerified {
			continue
		}
		if row.Status == blackboard.StatusEscalated {
			escalated = true
		}
		if first == "" {
			repMu.Lock()
			first = rc.rep.reasons[l.ID]
			repMu.Unlock()
			if first == "" {
				first = "leaf_not_verified"
			}
		}
	}
	switch {
	case escalated:
		return "escalated", "human_stop"
	case first != "":
		return "failed", first
	}
	return "verified", ""
}

// finish is the only place a run's final status is decided (spec 7.1). It
// returns verified only when every leaf is verified, the last wave's unrestricted
// build, vet and race test passed (runWave and every acceptance repair do that
// before finish is reached), the acceptance run proved N of N, go mod verify
// passed, the final scan is clean and the landing succeeded. Any other end is a
// fixed non-verified status with its reason. A *stopError or a cancelled
// context from a step becomes the result; any other error is a harness fault.
func (rc *runCtx) finish(ctx context.Context, f runFlags) (finishResult, error) {
	none := accResult{Total: len(rc.accept.acceptance), ConstraintsTotal: len(rc.accept.constraints)}
	if status, reason := rc.decide(); status != "verified" {
		return finishResult{Status: status, Reason: reason, Acc: none}, nil
	}
	acc := none
	if !f.skipAcceptance {
		if err := rc.buildBinaries(ctx); err != nil {
			return rc.finishFromError(ctx, err, none)
		}
		var stop *stopError
		var err error
		if acc, stop, err = rc.acceptance(ctx); err != nil {
			return rc.finishFromError(ctx, err, acc)
		}
		if stop != nil {
			return finishResult{Status: stop.Status, Reason: stop.Reason, Acc: acc, Failures: stopFailure(stop)}, nil
		}
		// A repair changed leaves: the verdict is about the leaves as they are now.
		if status, reason := rc.decide(); status != "verified" {
			return finishResult{Status: status, Reason: reason, Acc: acc}, nil
		}
		if acc.Passed != acc.Total || acc.ConstraintsPassed != acc.ConstraintsTotal {
			return finishResult{Status: "failed", Reason: "acceptance_failed", Acc: acc}, nil
		}
	}
	if err := rc.verifyModules(ctx); err != nil {
		return rc.finishFromError(ctx, err, acc)
	}
	found, err := ScanRepo(rc.o.Repo)
	if err != nil {
		return finishResult{}, err
	}
	if len(found) > 0 {
		rc.emit("scan", "", fmt.Sprintf("%d forbidden construct(s)", len(found)))
		reason := "forbidden_source"
		for _, x := range found {
			if filepath.Base(x.File) == "go.mod" {
				reason = "forbidden_go_mod"
			}
		}
		return finishResult{Status: "failed", Reason: reason, Acc: acc, Failures: []string{describeFindings(rc.plan, found)}}, nil
	}
	rc.emit("scan", "", "clean")
	landing, stop, err := rc.land(ctx, acc)
	if err != nil {
		return rc.finishFromError(ctx, err, acc)
	}
	if stop != nil {
		return finishResult{Status: stop.Status, Reason: stop.Reason, Acc: acc, Landing: landing}, nil
	}
	return finishResult{Status: "verified", Acc: acc, Landing: landing}, nil
}

// stopFailure is the stop's message as a report failure line, for the
// acceptance family of stops (the messages hold ids, requirement text and exit
// codes, never output).
func stopFailure(se *stopError) []string {
	switch se.Reason {
	case "acceptance_failed", "constraint_failed", "acceptance_environment", "unattributable", "acceptance_build":
		return []string{strings.TrimPrefix(se.Message, "executor: ")}
	}
	return nil
}

// finishFromError turns the error of a finishing step into a result when it is
// a stop or a cancelled context, and leaves a fault as an error.
func (rc *runCtx) finishFromError(ctx context.Context, err error, acc accResult) (finishResult, error) {
	if se, ok := stopOf(err); ok {
		return finishResult{Status: se.Status, Reason: se.Reason, Acc: acc, Failures: stopFailure(se)}, nil
	}
	if ctx.Err() != nil {
		return finishResult{Status: "interrupted", Reason: "cancelled", Acc: acc}, nil
	}
	return finishResult{}, err
}

// land ends a verified run. diff_only writes <run>/changes.patch (the working
// tree against the base) and commits nothing; otherwise the empty final commit
// on the work branch and a fast-forward of the base through gitland, the only
// git calls of the package. A failure is failed/landing_blocked with the work
// branch left as it is.
func (rc *runCtx) land(ctx context.Context, acc accResult) (*report.Landing, *stopError, error) {
	if se := rc.planIntact(); se != nil {
		return nil, se, nil
	}
	blocked := func(msg string) (*report.Landing, *stopError, error) {
		rc.emit("landing", "", "landing_blocked")
		// The work branch is left as it is; the report names it, with no commit on the base.
		var l *report.Landing
		if rc.state.Branch != "" {
			l = &report.Landing{Branch: rc.state.Branch, MergedInto: rc.baseBranch()}
		}
		return l, &stopError{Status: "failed", Reason: "landing_blocked", Message: "executor: " + msg}, nil
	}
	if rc.diffOnly {
		patch, err := rc.git.Diff(rc.baseBranch())
		if err != nil {
			return blocked("the patch could not be made")
		}
		if err := report.WriteFile(rc.o.RunDir, "changes.patch", patch, report.WriteOptions{RepoRoot: rc.o.Repo}); err != nil {
			return blocked("changes.patch could not be written")
		}
		rc.emit("landing", "", "diff_only: changes.patch")
		return nil, nil, nil
	}
	msg := fmt.Sprintf("%s built, Acceptance passed %d of %d", rc.plan.Brief.Front.Title, acc.Passed, acc.Total)
	hash, err := rc.git.Finish(msg)
	if err != nil {
		if errors.Is(err, gitland.ErrLandingBlocked) {
			return blocked("landing is blocked: the base branch moved, and the work branch is left as it is")
		}
		return blocked("landing failed, and the work branch is left as it is")
	}
	rc.emit("landing", "", hash)
	return &report.Landing{Branch: rc.state.Branch, Commit: hash, MergedInto: rc.baseBranch()}, nil, nil
}
