package executor

// The acceptance stage (spec 9) and the one place a run's final status is
// decided (spec 7.1). Binaries are built once, outside the working tree's
// tracked files, and every root test of the brief's acceptance bullets (and of
// the constraints that have one) runs against them. The proof keeps each
// command's exit code and the SHA-256 of what it printed, never the output.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gophermind/gophermind-lib/briefv2/acceptcheck"
	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/gitland"
	"gophermind/gophermind-lib/briefv2/packer"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/report"
	"gophermind/gophermind-lib/briefv2/runner"
	"gophermind/gophermind-lib/briefv2/vault"
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
	Test        string `json:"test"`                     // the root test's name
	Command     string `json:"command"`                  // fixed text with ids only ("root test A1 #1"): the command itself is in coverage.json
	CommandSHA  string `json:"command_sha256,omitempty"` // SHA-256 of the raw command text, never the text
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

// BinaryProof is the SHA-256 of one built binary, taken before the first command.
type BinaryProof struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// acceptanceSchema is the version of acceptance.json; 2 added schema_version,
// complete, binaries and command_sha256 (all additive).
const acceptanceSchema = 2

// AcceptanceFile is <run>/acceptance.json. It is written at the start of every
// round with complete false and again at the end with complete true, so a round
// that was cut off leaves the file of its start, never a half-counted result.
type AcceptanceFile struct {
	SchemaVersion       int           `json:"schema_version,omitempty"`
	Complete            bool          `json:"complete"`
	Reason              string        `json:"reason,omitempty"` // why a round that ended early is not complete
	Binaries            []BinaryProof `json:"binaries,omitempty"`
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
func mapAcceptance(reqs []planner.Requirement, cov planner.CoverageFile, bins ...string) (acceptPlan, error) {
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
	if ids := vacuousBullets(p, bins); len(ids) > 0 {
		return acceptPlan{}, &vacuousError{IDs: ids}
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
	rc.removeBinaries()
	rc.bins = map[string]string{}
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
		sum, err := fileSHA(out)
		if err != nil {
			return &stopError{Status: "failed", Reason: "acceptance_build",
				Message: "executor: a binary built for the acceptance run cannot be read"}
		}
		rc.bins[filepath.Base(rel)] = sum
	}
	return nil
}

// removeBinaries deletes the binaries of the last build, each a regular file
// directly inside the bin directory under a name this run recorded.
func (rc *runCtx) removeBinaries() {
	for name := range rc.bins {
		if name != filepath.Base(name) || name == "." || name == ".." || name == "" {
			continue
		}
		p := filepath.Join(rc.binDir, name)
		if fi, err := os.Lstat(p); err == nil && fi.Mode().IsRegular() && filepath.Dir(p) == rc.binDir {
			_ = os.Remove(p)
		}
	}
}

// fileSHA is the SHA-256 of a regular file, refusing a link.
func fileSHA(path string) (string, error) {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return "", errors.New("not a regular file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (rc *runCtx) binNames() []string {
	names := make([]string, 0, len(rc.bins))
	for n := range rc.bins {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// binaryProofs is the recorded hashes, in name order.
func (rc *runCtx) binaryProofs() []BinaryProof {
	var out []BinaryProof
	for _, n := range rc.binNames() {
		out = append(out, BinaryProof{Name: n, SHA256: rc.bins[n]})
	}
	return out
}

// checkBinaries fails the run when a built binary is no longer the one that was
// built: a command of the acceptance run could write to the scratch directory.
func (rc *runCtx) checkBinaries() *stopError {
	for _, n := range rc.binNames() {
		sum, err := fileSHA(filepath.Join(rc.binDir, n))
		if err != nil || sum != rc.bins[n] {
			return &stopError{Status: "failed", Reason: "acceptance_tampered",
				Message: "executor: a built binary changed while the acceptance run was going"}
		}
	}
	return nil
}

// acceptPidFile is where a command may record the pid of a server it starts
// (GM_ACCEPTANCE_PIDFILE), so that one that escaped the process group is found.
func (rc *runCtx) acceptPidFile() string { return filepath.Join(rc.scratch, "acceptance.pid") }

// freeAddr is a free loopback address for one command.
func freeAddr() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer l.Close()
	return l.Addr().String(), nil
}

var portRE = regexp.MustCompile(`(?:localhost|127\.0\.0\.1|\[::1\]):([0-9]{1,5})`)

// literalPorts are the loopback ports a command names (localhost:N,
// 127.0.0.1:N, [::1]:N), sorted and unique, as numbers in text.
func literalPorts(cmd string) []string {
	seen := map[int]bool{}
	var nums []int
	for _, m := range portRE.FindAllStringSubmatch(cmd, -1) {
		n, err := strconv.Atoi(m[1])
		if err == nil && n > 0 && n < 65536 && !seen[n] {
			seen[n] = true
			nums = append(nums, n)
		}
	}
	sort.Ints(nums)
	out := make([]string, len(nums))
	for i, n := range nums {
		out[i] = strconv.Itoa(n)
	}
	return out
}

func accepting(port string) bool {
	for _, h := range []string{"127.0.0.1", "::1"} {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(h, port), 200*time.Millisecond)
		if err == nil {
			c.Close()
			return true
		}
	}
	return false
}

// afterCommand is what the harness checks once a command is over and its
// process group was killed: the binaries are intact, nothing listens on the
// command's address and the pid the command recorded is gone. A leftover is
// killed, and the run fails acceptance_leak.
func (rc *runCtx) afterCommand(addr string, ports []string) *stopError {
	if se := rc.checkBinaries(); se != nil {
		return se
	}
	leak := false
	if raw, err := os.ReadFile(rc.acceptPidFile()); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && pid > 1 {
			if syscall.Kill(pid, 0) == nil {
				leak = true
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	}
	deadline := time.Now().Add(time.Second)
	for addr != "" {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err != nil {
			break
		}
		c.Close()
		if time.Now().After(deadline) {
			leak = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	var open []string
	for _, p := range ports {
		deadline := time.Now().Add(time.Second)
		for accepting(p) {
			if time.Now().After(deadline) {
				open = append(open, p)
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	if leak || len(open) > 0 {
		msg := "executor: a process started by an acceptance command outlived it (killed)"
		if len(open) > 0 {
			msg += "; still accepting on port " + strings.Join(open, ", ")
		}
		return &stopError{Status: "failed", Reason: "acceptance_leak", Message: msg}
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
// rc.chk.Run, on a fresh loopback address it is given through
// GM_ACCEPTANCE_ADDR and GM_ACCEPTANCE_URL (and the built binaries' directory
// through GM_ACCEPTANCE_BIN). A command that is one plain go test invocation is
// run as go test -json instead and judged by the test runner's pass rule. A
// command that runs the acceptance package gets the address of the server the
// plan's serve declaration started, while one is running. The runner.Result
// carries the output in memory only; the CommandProof carries hashes.
func (rc *runCtx) runRootTest(ctx context.Context, t planner.RootTest, unset ...string) (CommandProof, runner.Result, error) {
	return rc.runRootTestFull(ctx, t, rc.acceptanceTimeout(), unset...)
}

// runRootTestFull is runRootTest with the command's time limit given.
func (rc *runCtx) runRootTestFull(ctx context.Context, t planner.RootTest, timeout time.Duration, unset ...string) (CommandProof, runner.Result, error) {
	env, err := rc.env("acceptance", envAcceptance)
	if err != nil {
		return CommandProof{}, runner.Result{}, errors.New("executor: the environment of the acceptance run could not be built")
	}
	gt, isGoTest := acceptcheck.ParseGoTest(t.Command)
	unset = append(append([]string(nil), unset...), gt.Unset...)
	for _, name := range unset {
		kept := env[:0]
		for _, e := range env {
			if !strings.HasPrefix(e, name+"=") {
				kept = append(kept, e)
			}
		}
		env = kept
	}
	shared := isGoTest && isAcceptancePkg(gt.Pkg) && rc.serveAddr != ""
	addr := rc.serveAddr
	if !shared {
		if addr, err = freeAddr(); err != nil {
			return CommandProof{}, runner.Result{}, errors.New("executor: no free loopback port for the acceptance run")
		}
	}
	env = append(env, "GM_ACCEPTANCE_ADDR="+addr, "GM_ACCEPTANCE_URL=http://"+addr,
		"GM_ACCEPTANCE_BIN="+rc.binDir, "GM_ACCEPTANCE_PIDFILE="+rc.acceptPidFile())
	_ = os.Remove(rc.acceptPidFile())
	ports := literalPorts(t.Command)
	for _, p := range ports {
		if accepting(p) { // something else holds it: a stale server could make the bullet pass
			return CommandProof{}, runner.Result{}, &stopError{Status: "failed", Reason: "acceptance_environment",
				Message: "executor: port " + p + " named by an acceptance command is already in use (an environment fault)"}
		}
	}
	spec := runner.Spec{Dir: rc.o.Repo, Argv: runner.Shell(t.Command), Env: env, Timeout: timeout}
	if isGoTest {
		argv, err := rc.goTestArgv(gt)
		if err != nil {
			return CommandProof{}, runner.Result{}, err
		}
		spec.Argv = argv
		spec.Env = append(env, gt.Env...)
	}
	res := rc.chk.Run(ctx, spec)
	if res.Canceled || (res.Err == nil && ctx.Err() != nil) {
		return CommandProof{}, res, ctxErrOr(ctx)
	}
	if res.Err != nil {
		return CommandProof{}, res, fmt.Errorf("executor: an acceptance command could not be started (%w)", res.Err)
	}
	after := addr
	if shared {
		after = "" // the server is still up: its shutdown is checked when it stops
	}
	if se := rc.afterCommand(after, ports); se != nil {
		return CommandProof{}, res, se
	}
	sum := sha256.Sum256([]byte(t.Command))
	passed := res.ExitCode == 0 && !res.TimedOut
	if isGoTest {
		passed = goTestPassed(res, gt)
	}
	p := CommandProof{
		Test: rc.scrubText(t.Name), CommandSHA: hex.EncodeToString(sum[:]), ExitCode: res.ExitCode, TimedOut: res.TimedOut,
		DurationMS: res.Duration.Milliseconds(), OutputBytes: res.Out.Size(), OutputSHA: res.Out.SHA256(),
		Passed: passed,
	}
	return p, res, nil
}

// goTestArgv is go test -json for a recognised plain go test command: the
// flags the command named (-tags, -race, -short, -run as an anchored list) and
// its package. -v is dropped: -json already carries every event.
func (rc *runCtx) goTestArgv(gt acceptcheck.GoTest) ([]string, error) {
	argv := []string{rc.goBin, "test", "-count=1", "-json"}
	if gt.Tags != "" {
		argv = append(argv, "-tags", gt.Tags)
	}
	if gt.Race {
		argv = append(argv, "-race")
	}
	if gt.Short {
		argv = append(argv, "-short")
	}
	if len(gt.Funcs) > 0 {
		re, err := runner.RunRegex(gt.Funcs)
		if err != nil {
			return nil, errors.New("executor: an acceptance go test names a test the runner cannot select")
		}
		argv = append(argv, "-run", re)
	}
	return append(argv, gt.Pkg), nil
}

// goTestPassed is the runner's pass rule for a go test acceptance command: it
// exited 0 in time, at least one test ran and passed, none failed, the build
// held, and every test its -run named passed (a skipped test is not a pass). A
// stream cut by the output cap keeps the exit status and the events it holds.
func goTestPassed(res runner.Result, gt acceptcheck.GoTest) bool {
	if res.ExitCode != 0 || res.TimedOut {
		return false
	}
	rep := runner.ParseTestJSON(strings.NewReader(res.Out.Text()))
	if rep.Events == 0 || rep.PkgFailed || rep.BuildFailed || len(rep.Failed) > 0 || len(rep.Passed) == 0 {
		return false
	}
	if res.Out.Truncated() {
		return true
	}
	for _, name := range gt.Funcs {
		found := false
		for k := range rep.Passed {
			if strings.HasSuffix(k, "."+name) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// plainTestOutput turns a go test -json stream into the text the test printed,
// for the capped failure lines a repair round shows a leaf: JSON event lines
// become their Output field, any other line stays.
func plainTestOutput(text string) string {
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		var ev struct{ Output string }
		if strings.HasPrefix(line, "{") && json.Unmarshal([]byte(line), &ev) == nil {
			b.WriteString(ev.Output)
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

type failedBullet struct {
	Req      planner.Requirement
	Command  string
	ExitCode int
	TimedOut bool
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

func (rc *runCtx) runBullet(ctx context.Context, pb planBullet) (BulletProof, *failedBullet, error) {
	bp := BulletProof{Requirement: pb.req.ID, Text: rc.scrubText(pb.req.Text), Commands: []CommandProof{}, Verdict: "pass"}
	var fb *failedBullet
	unset := rc.unsetNames(pb.req.Text)
	for i, t := range pb.tests {
		proof, res, err := rc.runRootTest(ctx, t, unset...)
		if err != nil {
			return bp, nil, err
		}
		proof.Command = fmt.Sprintf("root test %s #%d", pb.req.ID, i+1)
		if len(unset) > 0 {
			proof.Command += " (unset " + strings.Join(unset, ", ") + ")"
		}
		bp.Commands = append(bp.Commands, proof)
		if proof.Passed {
			continue
		}
		bp.Verdict = "fail"
		if fb == nil {
			text := res.Out.Text()
			fb = &failedBullet{
				Req: pb.req, Command: proof.Command, ExitCode: proof.ExitCode, TimedOut: proof.TimedOut,
				Nodes: pb.nodes, Failure: packer.NewFailure(nil, text, rc.secretValues()),
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
		SchemaVersion: acceptanceSchema, Binaries: rc.binaryProofs(),
	}
	if err := rc.writeAcceptance(file); err != nil { // the start of the round: complete is false
		return file, nil, err
	}
	if se := rc.checkAcceptTests(); se != nil {
		return file, nil, se
	}
	snap, err := TakeSnapshot(rc.o.Repo, rc.git)
	if err != nil {
		return file, nil, err
	}
	var sp *serveProc
	if rc.needsServe(p) {
		if sp, err = rc.startServe(ctx); err != nil {
			return file, nil, err
		}
		defer rc.dropServe(sp)
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
	if sp != nil {
		if se := rc.stopServe(sp); se != nil {
			return file, failed, se
		}
	}
	stray, err := snap.Stray(rc.o.Repo, rc.git)
	if err != nil {
		return file, failed, err
	}
	if len(stray) > 0 {
		file.Reason = "acceptance_stray" // the round is not complete
		if err := rc.writeAcceptance(file); err != nil {
			return file, failed, err
		}
		if err := Revert(rc.git, stray); err != nil {
			return file, failed, errors.New("executor: removing the files the acceptance commands wrote failed")
		}
		rc.emit("stray_write", "acceptance", fmt.Sprintf("%d files written by the acceptance commands were removed", len(stray)))
		return file, failed, &stopError{Status: "failed", Reason: "acceptance_stray",
			Message: fmt.Sprintf("executor: the acceptance commands of round %d wrote %d file(s) into the repository (removed)", round, len(stray))}
	}
	file.Complete = true
	if err := rc.writeAcceptance(file); err != nil {
		return file, failed, err
	}
	return file, failed, nil
}

func (rc *runCtx) writeAcceptance(file AcceptanceFile) error {
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return errors.New("executor: encoding acceptance.json failed")
	}
	return report.WriteFile(rc.o.RunDir, fileAcceptance, append(raw, '\n'), report.WriteOptions{RepoRoot: rc.o.Repo})
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
		var unmapped []string
		for _, id := range ids {
			if len(rc.attribute(failed[id])) == 0 {
				unmapped = append(unmapped, id)
			}
		}
		if len(unmapped) > 0 {
			return acc, &stopError{Status: "failed", Reason: "acceptance_unmapped",
				Message: "executor: acceptance failed for " + strings.Join(unmapped, ", ") + ", which no leaf is mapped to, so no repair was tried"}, nil
		}
		if round >= bound {
			return acc, rc.acceptStop(ids, failed, accFailed), nil
		}
		if se, err := rc.acceptRepair(ctx, round+1, ids, failed); err != nil || se != nil {
			return acc, se, err
		}
		// A repair changed leaves, maybe under cmd/: the rerun must use a binary
		// built from them, with a new baseline for the tamper check.
		if err := rc.buildBinaries(ctx); err != nil {
			if se, ok := stopOf(err); ok {
				return acc, se, nil
			}
			return acc, nil, err
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
// coverage file lists for it (spec 9). A bullet with none is unmapped and ends
// the run; there is no guessing which leaf to rebuild.
func (rc *runCtx) attribute(b failedBullet) []string {
	var ids []string
	for _, n := range b.Nodes {
		if rc.plan.Leaf(n) != nil {
			ids = append(ids, n)
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

// checkEnvironment is the harness's own connectivity check before acceptance:
// every declared secret whose value is a URL with a loopback host is dialled
// (2 seconds). An unreachable one stops the run acceptance_environment, naming
// the secret only; no output of any command is read to decide it.
func (rc *runCtx) checkEnvironment() *stopError {
	if rc.o.Secrets == nil {
		return nil
	}
	scope := vault.RunScope(rc.plan.RunID)
	for _, sec := range rc.plan.Brief.Front.Secrets {
		if !rc.needsSecret(sec.Name) {
			continue
		}
		val, ok := rc.o.Secrets.Get(scope, sec.Name)
		if !ok {
			continue
		}
		u, err := url.Parse(val)
		if err != nil || u.Scheme == "" || u.Host == "" {
			continue
		}
		host := u.Hostname()
		if host != "127.0.0.1" && host != "localhost" && host != "::1" {
			continue
		}
		port := u.Port()
		if port == "" {
			port = map[string]string{"postgres": "5432", "postgresql": "5432", "mysql": "3306", "redis": "6379", "http": "80", "https": "443"}[u.Scheme]
		}
		if port == "" {
			continue
		}
		c, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 2*time.Second)
		if err != nil {
			return &stopError{Status: "failed", Reason: "acceptance_environment",
				Message: "executor: the host of secret " + sec.Name + " is not reachable (an environment fault, not a leaf failure)"}
		}
		c.Close()
	}
	return nil
}

// needsSecret says that some bullet runs with the variable: its text or one of
// its commands names it and the bullet does not say "with NAME unset".
func (rc *runCtx) needsSecret(name string) bool {
	for _, list := range [][]planBullet{rc.accept.acceptance, rc.accept.constraints} {
		for _, pb := range list {
			unset := false
			for _, u := range rc.unsetNames(pb.req.Text) {
				if u == name {
					unset = true
				}
			}
			if unset {
				continue
			}
			if strings.Contains(pb.req.Text, name) {
				return true
			}
			for _, t := range pb.tests {
				if strings.Contains(t.Command, name) {
					return true
				}
			}
		}
	}
	return false
}

// treeClean requires that only declared files differ from the landed state:
// nothing in commit mode; in diff_only the plan's own files.
func (rc *runCtx) treeClean() *stopError {
	dirty, err := rc.git.Dirty()
	if err != nil {
		return &stopError{Status: "failed", Reason: "tree_not_clean", Message: "executor: the repository state could not be read"}
	}
	ok := map[string]bool{}
	if rc.diffOnly {
		for _, p := range rc.wave0Paths() {
			ok[p] = true
		}
		for _, l := range rc.plan.Leaves {
			ok[l.File] = true
		}
	}
	n := 0
	for _, p := range dirty {
		if !ok[p] {
			n++
		}
	}
	if n > 0 {
		return &stopError{Status: "failed", Reason: "tree_not_clean",
			Message: fmt.Sprintf("executor: %d path(s) outside the plan's files are dirty after acceptance", n)}
	}
	return nil
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
		if se := rc.checkEnvironment(); se != nil {
			return finishResult{Status: se.Status, Reason: se.Reason, Acc: none, Failures: stopFailure(se)}, nil
		}
		if err := rc.buildBinaries(ctx); err != nil {
			return rc.finishFromError(ctx, err, none)
		}
		if ids := vacuousBullets(rc.accept, rc.binNames()); len(ids) > 0 {
			se := &vacuousError{IDs: ids}
			return finishResult{Status: "failed", Reason: "acceptance_vacuous", Acc: none, Failures: []string{strings.TrimPrefix(se.Error(), "executor: ")}}, nil
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
		// Build, vet and the race tests over ./... once more, now that acceptance
		// (and any repair it made) is over.
		ws := rc.waves()
		res, err := rc.waveChecks(ctx, ws[len(ws)-1], true)
		if err != nil {
			return rc.finishFromError(ctx, err, acc)
		}
		if !res.Pass() {
			kind := integrationKind(res)
			return finishResult{Status: "failed", Reason: "final_" + kind, Acc: acc,
				Failures: []string{"the final " + kind + " check failed after acceptance"}}, nil
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
	if !f.skipAcceptance {
		if se := rc.treeClean(); se != nil {
			return finishResult{Status: se.Status, Reason: se.Reason, Acc: acc, Failures: stopFailure(se)}, nil
		}
	}
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
	case "acceptance_failed", "constraint_failed", "acceptance_environment", "acceptance_unmapped", "acceptance_tampered",
		"acceptance_stray", "acceptance_leak", "acceptance_vacuous", "acceptance_build", "acceptance_serve", "acceptance_not_red", "tree_not_clean", "foreign_dirt", "repo_moved", "base_moved":
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
			l = &report.Landing{Branch: rc.state.Branch}
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
