package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/gitland"
	"gophermind/gophermind-lib/briefv2/packer"
	"gophermind/gophermind-lib/briefv2/pathsafe"
	"gophermind/gophermind-lib/briefv2/proxy"
	"gophermind/gophermind-lib/briefv2/runner"
	"gophermind/gophermind-lib/briefv2/sandbox"
	"gophermind/gophermind-lib/briefv2/settings"
)

// goCacheOverride is set only by the test binary's TestMain: one shared
// GOCACHE for every test of the process, so a package compiled once is not
// compiled again by each test. Empty in production (the cache is per run).
var goCacheOverride string

// hostOS is runtime.GOOS; tests replace it to reach the off-darwin rule.
var hostOS = runtime.GOOS

// stopError is how a stage ends the run with a status instead of a fault. Run
// (Task 12b) turns it into a Report; a plain error is a harness fault. Status is
// a report status: failed | escalated | interrupted. Message never holds reply
// text, command output or a secret.
type stopError struct{ Status, Reason, Message string }

func (e *stopError) Error() string { return e.Message }

// runCtx is everything one run shares (called rc everywhere).
type runCtx struct {
	o         Options
	cfg       *settings.Config
	plan      *Plan
	state     State
	chk       Checker // *runner.Runner in production
	git       gitland.Repo
	closers   []func() error // the CLI, the executor's own proxy; Run closes them
	prox      *proxy.Proxy
	streak    *proxy.Streak // critical-host streak, mirrored into state.CriticalStreak
	policy    packer.ImportPolicy
	diffOnly  bool
	bins      map[string]string // built binary name -> SHA-256, taken when it was built
	accept    acceptPlan        // what the acceptance stage must prove (spec 9), computed before anything starts
	sandboxOn bool
	goBin     string
	gitBin    string

	scratch, home, goCache, modCache, binDir string

	rep       *runReport
	sched     schedState    // the scheduler's memory of interrupted leaves (Task 11b)
	limit     time.Duration // test override of max_run_minutes (Task 14); zero means the setting
	heartbeat time.Duration // test override of heartbeat_seconds; zero means the setting
}

// runReport holds the counters that become report.Input.
type runReport struct {
	weak, repairs int
	blocked       map[string]string // leaf id -> id of the dependency that blocks it
	reasons       map[string]string // leaf id -> final reason of a failed or escalated leaf
	sandboxExec   string            // "available" or "missing": stated with the sandbox setting in the report environment
}

// sandboxLabel is the sandbox setting as the report states it: "on" or "off".
func (rc *runCtx) sandboxLabel() string {
	if rc.sandboxOn {
		return "on"
	}
	return "off"
}

func (rc *runCtx) emit(kind, node, msg string) {
	rc.o.Sink.Emit(events.Event{Kind: kind, NodeID: node, Message: msg, At: rc.o.Now()})
}

// close runs the closers, last first.
func (rc *runCtx) close() {
	for i := len(rc.closers) - 1; i >= 0; i-- {
		_ = rc.closers[i]()
	}
	rc.closers = nil
}

// startRun is preflight through the Wave 0 commit. Nothing here calls a model.
// The returned runCtx is non-nil whenever anything was opened, even with an
// error, so the caller can close it; the error may be a *stopError.
func startRun(ctx context.Context, o Options) (*runCtx, error) {
	rc, err := newRunCtx(ctx, o)
	if err != nil {
		return rc, err
	}
	if err := rc.begin(ctx); err != nil {
		return rc, err
	}
	return rc, nil
}

// newRunCtx is the part of startRun before git: validation, the plan, the
// landing check and preflight (spec steps 1 to 7).
func newRunCtx(ctx context.Context, o Options) (*runCtx, error) {
	if err := o.validate(); err != nil {
		return nil, err
	}
	plan, err := LoadPlan(o.RunDir, o.Repo)
	if err != nil {
		return nil, err
	}
	if err := gitland.ValidateLanding(plan.Brief.Front.Landing); err != nil {
		return nil, err
	}
	state, err := LoadState(o.RunDir)
	if err != nil {
		return nil, err
	}
	rc := &runCtx{
		o: o, cfg: o.Settings, plan: plan, state: state,
		policy: plan.Policy(), diffOnly: plan.Brief.Front.Landing == "diff_only",
		streak: &proxy.Streak{},
		rep:    &runReport{blocked: map[string]string{}, reasons: map[string]string{}},
	}
	if err := rc.loadReasons(); err != nil {
		return nil, err
	}
	// Spec 9 tripwire, before a file is touched or a model is called: an
	// acceptance bullet with no root test can never make N smaller.
	if rc.accept, err = mapAcceptance(plan.Requirements, plan.Coverage, plan.binNames()...); err != nil {
		reason := "acceptance_unmapped"
		var v *vacuousError
		if errors.As(err, &v) {
			reason = "acceptance_vacuous"
		}
		return rc, &stopError{Status: "failed", Reason: reason, Message: err.Error()}
	}
	// The git layer first: it confirms the repository root before preflight
	// creates any directory under it.
	if err := rc.openGit(); err != nil {
		rc.close()
		return nil, err
	}
	if err := rc.preflight(ctx); err != nil {
		rc.close()
		return nil, err
	}
	return rc, nil
}

func (rc *runCtx) openGit() error {
	if rc.o.Git != nil {
		rc.git = rc.o.Git
		return nil
	}
	cli, err := gitland.NewCLI(rc.plan.Repo, rc.plan.RunID)
	if err != nil {
		return errors.New("executor: the git layer could not start (the repository root was not confirmed)")
	}
	rc.git = cli
	rc.closers = append(rc.closers, cli.Close)
	return nil
}

func pathEntries(p string) []string {
	var out []string
	for _, d := range filepath.SplitList(p) {
		if filepath.IsAbs(d) {
			out = append(out, d)
		}
	}
	return out
}

// lookIn finds name in the directories of a PATH value.
func lookIn(name, pathValue string) (string, bool) {
	for _, d := range pathEntries(pathValue) {
		p := filepath.Join(d, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p, true
		}
	}
	return "", false
}

func sandboxExecAvailable() bool {
	if _, err := exec.LookPath("sandbox-exec"); err == nil {
		return true
	}
	_, err := os.Stat("/usr/bin/sandbox-exec")
	return err == nil
}

// ensureDir makes a directory the harness owns. A symbolic link or a file in
// its place is refused: model-written code can write under the repository.
func ensureDir(path string) error {
	fi, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(path, 0o700); err != nil {
			return errors.New("executor: preflight: creating a working directory failed")
		}
		return nil
	case err != nil:
		return errors.New("executor: preflight: a working directory is not readable")
	case fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir():
		return errors.New("executor: preflight: a working directory is not a plain directory")
	}
	return nil
}

func within(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// preflight is spec steps 2 to 7: tools, sandbox, model chains, directories,
// proxy and runner.
func (rc *runCtx) preflight(ctx context.Context) error {
	cfg := rc.cfg
	var ok bool
	if rc.goBin, ok = lookIn("go", cfg.Toolchain["PATH"]); !ok {
		return errors.New("executor: preflight: go not found on PATH")
	}
	gitBin, err := exec.LookPath("git")
	if err != nil {
		return errors.New("executor: preflight: git not found on PATH")
	}
	rc.gitBin = gitBin

	on, err := sandbox.Required(cfg.Executor.Sandbox, hostOS)
	if err != nil {
		return fmt.Errorf("executor: preflight: %w", err)
	}
	rc.sandboxOn = on
	avail := "missing"
	if sandboxExecAvailable() {
		avail = "available"
	}
	rc.rep.sandboxExec = avail
	if on {
		if err := sandbox.Preflight(ctx); err != nil {
			return fmt.Errorf("executor: preflight: %w", err)
		}
	}
	rc.emit("sandbox", "", fmt.Sprintf("%s; sandbox-exec %s", rc.sandboxLabel(), avail))

	if err := checkChains(cfg); err != nil {
		return err
	}

	realHome, err := os.UserHomeDir()
	if err != nil || realHome == "" {
		return errors.New("executor: preflight: the home directory is not known")
	}
	rc.home = realHome
	id := rc.plan.RunID
	rc.scratch = filepath.Join(rc.o.Repo, ".gophermind", id+"-scratch")
	rc.goCache = filepath.Join(rc.scratch, "gocache")
	if goCacheOverride != "" {
		rc.goCache = goCacheOverride
	}
	// The built binaries live in the scratch directory: the sandbox denies
	// writes to the run folder, and go build -o runs inside it.
	rc.binDir = filepath.Join(rc.scratch, "bin")
	if rc.scratch == rc.o.RunDir || within(rc.o.RunDir, rc.scratch) {
		return errors.New("executor: preflight: the run folder must not contain the scratch directory")
	}
	mc := cfg.Executor.GoModCache
	switch {
	case strings.HasPrefix(mc, "~/"):
		rc.modCache = filepath.Join(realHome, mc[2:])
	case filepath.IsAbs(mc):
		rc.modCache = filepath.Clean(mc)
	default:
		return errors.New("executor: preflight: executor.go_mod_cache must start with ~/ or be absolute")
	}
	if rc.modCache == realHome || rc.modCache == "/" {
		return errors.New("executor: preflight: executor.go_mod_cache is too broad")
	}
	if err := os.MkdirAll(filepath.Dir(rc.scratch), 0o700); err != nil {
		return errors.New("executor: preflight: creating a working directory failed")
	}
	for _, d := range []string{rc.scratch, rc.goCache, filepath.Join(rc.scratch, "gopath"), rc.modCache, rc.binDir} {
		if err := ensureDir(d); err != nil {
			return err
		}
	}

	if err := rc.startProxy(); err != nil {
		return err
	}

	var profile *sandbox.Profile
	if on {
		goroot, err := rc.goroot(ctx)
		if err != nil {
			return err
		}
		ro := []string{goroot, filepath.Dir(rc.goBin), filepath.Dir(rc.gitBin)}
		ro = append(ro, pathEntries(cfg.Toolchain["PATH"])...)
		profile = &sandbox.Profile{
			Repo: rc.o.Repo, RunDir: rc.o.RunDir, Scratch: rc.scratch, GoCache: rc.goCache,
			GoModCache: rc.modCache, ReadOnly: ro, Home: realHome,
		}
	}
	rc.chk = runner.New(runner.Config{Sandbox: profile, OutputCap: cfg.Executor.OutputCapBytes, Grace: 2 * time.Second})
	return nil
}

// goroot runs go env GOROOT once with an explicit minimal environment.
func (rc *runCtx) goroot(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, rc.goBin, "env", "GOROOT")
	cmd.Dir = rc.o.Repo
	cmd.Env = []string{"PATH=" + rc.cfg.Toolchain["PATH"], "HOME=" + rc.scratch, "GOCACHE=" + rc.goCache, "GOTOOLCHAIN=local", "GOFLAGS=", "GOPATH=" + filepath.Join(rc.scratch, "gopath")}
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", errors.New("executor: preflight: go env GOROOT failed")
	}
	g := strings.TrimSpace(out.String())
	if !filepath.IsAbs(g) {
		return "", errors.New("executor: preflight: go env GOROOT gave no directory")
	}
	return g, nil
}

// startProxy uses Options.Proxy or starts the executor's own, closed through rc.closers.
func (rc *runCtx) startProxy() error {
	if rc.o.Proxy != nil {
		rc.prox = rc.o.Proxy
		return nil
	}
	var network []brief.Network
	seen := map[string]int{}
	for _, l := range rc.plan.Leaves {
		for _, h := range l.Network {
			host := strings.ToLower(strings.TrimSpace(h.Host))
			if i, ok := seen[host]; ok {
				network[i].Critical = network[i].Critical || h.Critical
				continue
			}
			seen[host] = len(network)
			network = append(network, brief.Network{Host: host, Critical: h.Critical})
		}
	}
	var urls []string
	for _, p := range rc.cfg.Providers {
		urls = append(urls, p.BaseURL)
	}
	rules, err := proxy.BuildRules(network, urls, false)
	if err != nil {
		return fmt.Errorf("executor: preflight: the network allowlist is not valid: %w", err)
	}
	p, err := proxy.New(proxy.Config{
		Listen: rc.cfg.Executor.Proxy.Listen, LogPath: filepath.Join(rc.o.RunDir, rc.cfg.Executor.Proxy.Log), Rules: rules,
	})
	if err != nil {
		return errors.New("executor: preflight: the network proxy did not start")
	}
	rc.prox = p
	rc.closers = append(rc.closers, p.Close)
	return nil
}

// checkChains is spec 7.5: for standard and strong (the tiers the executor
// uses) the chain must hold an entry whose provider exists and may see a node's
// text: a private provider, or a public one when privacy.mode is need_to_know.
func checkChains(cfg *settings.Config) error {
	for _, tier := range []string{"standard", "strong"} {
		usable := false
		for _, entry := range cfg.Models[tier] {
			name, _, ok := settings.SplitEntry(entry)
			if !ok {
				continue
			}
			vis, found := cfg.Visibility(name)
			if found && (vis == settings.Private || cfg.Privacy.Mode == "need_to_know") {
				usable = true
				break
			}
		}
		if !usable {
			return fmt.Errorf("executor: preflight: tier %s has no usable model: privacy.mode is %s and no provider in the chain is private (set privacy.mode or add a private model)", tier, cfg.Privacy.Mode)
		}
	}
	return nil
}

// allowlist is the hosts the harness proxy allows: every leaf's network hosts
// (critical ones also in critical), the host of each provider base_url, and the
// Go module hosts. Both lists are sorted and unique.
func allowlist(p *Plan, cfg *settings.Config) (allow, critical []string) {
	all := map[string]bool{"proxy.golang.org": true, "sum.golang.org": true}
	crit := map[string]bool{}
	for _, l := range p.Leaves {
		for _, h := range l.Network {
			host := strings.ToLower(strings.TrimSpace(h.Host))
			all[host] = true
			if h.Critical {
				crit[host] = true
			}
		}
	}
	for _, pc := range cfg.Providers {
		if u, err := url.Parse(pc.BaseURL); err == nil && u.Hostname() != "" {
			all[strings.ToLower(u.Hostname())] = true
		}
	}
	for h := range all {
		allow = append(allow, h)
	}
	for h := range crit {
		critical = append(critical, h)
	}
	sort.Strings(allow)
	sort.Strings(critical)
	return allow, critical
}

// allowDirty is every leaf's test file: the Test-writer leaves them
// uncommitted, and Wave 0 commits them.
func (rc *runCtx) allowDirty() []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range rc.plan.Leaves {
		if !seen[l.TestFile] {
			seen[l.TestFile] = true
			out = append(out, l.TestFile)
		}
	}
	sort.Strings(out)
	return out
}

// begin is spec steps 8 to 10: git, the board and state, then Wave 0 on a fresh
// run. Fresh versus resume is decided by _state/executor.json alone.
func (rc *runCtx) begin(ctx context.Context) error {
	if rc.state.StartedAt != "" {
		if rc.state.Wave0Done {
			return rc.resume(ctx)
		}
		return rc.retryWave0(ctx)
	}

	allow := map[string]bool{}
	for _, p := range rc.allowDirty() {
		allow[p] = true
	}
	dirty, err := rc.git.Dirty()
	if err != nil {
		return errors.New("executor: the repository state could not be read")
	}
	var foreign []string
	for _, p := range dirty {
		if !allow[p] {
			foreign = append(foreign, p)
		}
	}
	if len(foreign) > 0 {
		shown := foreign
		if len(shown) > 3 {
			shown = shown[:3]
		}
		return fmt.Errorf("executor: repository is not clean: %d path(s) outside the plan's test files: %s", len(foreign), strings.Join(shown, ", "))
	}

	branch := ""
	if !rc.diffOnly {
		base := rc.plan.Brief.Front.BaseBranch
		if base == "" {
			base = "main"
		}
		branch = rc.plan.Brief.Front.WorkBranchName()
		if err := rc.git.Start(base, branch, rc.allowDirty()); err != nil {
			return fmt.Errorf("executor: starting the work branch failed: %w", err)
		}
	}

	ids := make([]string, 0, len(rc.plan.Leaves))
	waves := map[string]int{}
	for _, l := range rc.plan.Leaves {
		ids = append(ids, l.ID)
		waves[l.ID] = l.Wave
	}
	if err := rc.o.Board.InitRun(ctx, rc.plan.RunID, ids, waves); err != nil {
		return errors.New("executor: the blackboard could not be initialised")
	}
	hashes := map[string]string{}
	for k, v := range rc.plan.Hashes {
		hashes[k] = v
	}
	rc.state.PlanHashes = hashes
	rc.state.StartedAt = rc.o.Now().UTC().Format(time.RFC3339)
	rc.state.Branch = branch
	if err := rc.state.Save(rc.o.RunDir); err != nil {
		return err
	}
	return rc.wave0(ctx)
}

// wave0Paths is every file Wave 0 may leave dirty or commit: the tests, the
// contract type files, the stubs, go.mod and go.sum.
func (rc *runCtx) wave0Paths() []string {
	paths := append([]string{}, rc.allowDirty()...)
	for _, t := range rc.plan.Contracts.Types {
		paths = append(paths, t.File)
	}
	for _, l := range rc.plan.Leaves {
		paths = append(paths, l.StubFile)
	}
	return uniqueSorted(append(paths, "go.mod", "go.sum"))
}

// retryWave0 re-enters Wave 0 for a run that started and stopped before its
// Wave 0 commit (a plan defect, a forbidden go.mod, an unreachable proxy).
// The work branch is reused only while it still points at the base tip, the
// dirty paths must be Wave 0's own, and every step of Wave 0 is idempotent
// (types and stubs are rewritten only when they differ, RedChecked persists,
// nothing is committed twice because nothing was).
func (rc *runCtx) retryWave0(ctx context.Context) error {
	if !reflect.DeepEqual(rc.state.PlanHashes, rc.plan.Hashes) {
		return errors.New("executor: the plan changed since this run started; Wave 0 cannot be retried")
	}
	allow := rc.wave0Paths()
	if rc.diffOnly {
		ok := map[string]bool{}
		for _, p := range allow {
			ok[p] = true
		}
		dirty, err := rc.git.Dirty()
		if err != nil {
			return errors.New("executor: the repository state could not be read")
		}
		var foreign []string
		for _, p := range dirty {
			if !ok[p] {
				foreign = append(foreign, p)
			}
		}
		if len(foreign) > 0 {
			return fmt.Errorf("executor: repository is not clean: %d path(s) outside Wave 0's files: %s", len(foreign), firstThree(foreign))
		}
	} else {
		base := rc.plan.Brief.Front.BaseBranch
		if base == "" {
			base = "main"
		}
		if err := rc.git.Reenter(base, rc.state.Branch, allow); err != nil {
			return fmt.Errorf("executor: Wave 0 cannot be retried: %w", err)
		}
	}
	rc.emit("wave0_retry", "", "re-entering Wave 0")
	return rc.wave0(ctx)
}

// resume is the basic resume of a run whose Wave 0 is done; Task 14 replaces
// it with the full one (files on disk, foreign dirt, limits). It refuses a plan
// that changed since the run started, makes sure the work branch is checked
// out, and records that the run resumed once a row is past pending. Nothing
// here calls a model, so a verified leaf is never built again: the scheduler
// skips every verified row.
func (rc *runCtx) resume(ctx context.Context) error {
	if !reflect.DeepEqual(rc.state.PlanHashes, rc.plan.Hashes) {
		return &stopError{Status: "failed", Reason: "plan_changed",
			Message: "executor: the plan changed since this run started"}
	}
	// A diff_only repair that was cut off leaves its candidate on disk and the
	// verified file under _state: put the verified file back first.
	if err := restorePriors(rc.o.Repo, rc.o.RunDir, rc.plan.Leaves); err != nil {
		return err
	}
	if !rc.diffOnly && rc.state.Branch != "" {
		cur, err := rc.git.Branch()
		if err != nil {
			return errors.New("executor: the current branch could not be read")
		}
		if cur != rc.state.Branch {
			if err := rc.git.Start(rc.baseBranch(), rc.state.Branch, nil); err != nil {
				return errors.New("executor: the work branch of this run could not be checked out")
			}
		}
	}
	rows, err := rc.o.Board.List(ctx, rc.plan.RunID, blackboard.Filter{})
	if err != nil {
		return errors.New("executor: the blackboard could not be read")
	}
	for _, r := range rows {
		if r.Status != blackboard.StatusPending {
			rc.state.Resumed = true
			break
		}
	}
	if !rc.state.Resumed {
		return nil
	}
	return rc.state.Save(rc.o.RunDir)
}

// wave0 is spec 14 in order: types and stubs, go.mod, the deps step, the scan,
// the build check, the red check, the commit.
func (rc *runCtx) wave0(ctx context.Context) error {
	repo := rc.o.Repo
	typePaths, err := WriteTypes(repo, rc.plan.Contracts, rc.policy)
	if err != nil {
		return fmt.Errorf("executor: Wave 0: the contract types could not be written: %w", err)
	}
	stubs := make([]string, 0, len(rc.plan.Leaves))
	for _, l := range rc.plan.Leaves {
		src, err := stubFor(rc.plan.Contracts, rc.policy, l)
		if err != nil {
			return fmt.Errorf("executor: Wave 0: the stub of %s could not be made: %w", l.ID, err)
		}
		if cur, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(l.StubFile))); err == nil && bytes.Equal(cur, src) {
			stubs = append(stubs, l.StubFile)
			continue
		}
		if err := pathsafe.Replace(repo, l.StubFile, src); err != nil {
			return fmt.Errorf("executor: Wave 0: writing the stub of %s failed: %w", l.ID, err)
		}
		stubs = append(stubs, l.StubFile)
	}

	modPath := filepath.Join(repo, "go.mod")
	if fi, err := os.Lstat(modPath); errors.Is(err, os.ErrNotExist) {
		if res := rc.goCmd(ctx, "init", envLeaf, "mod", "init", rc.plan.Contracts.Module); res.Err != nil || res.ExitCode != 0 {
			return fmt.Errorf("executor: Wave 0: go mod init failed (exit %d)", res.ExitCode)
		}
	} else if err != nil || !fi.Mode().IsRegular() {
		return errors.New("executor: Wave 0: go.mod is not a regular file")
	}

	if err := rc.runDeps(ctx); err != nil {
		return err
	}

	found, err := ScanRepo(repo)
	if err != nil {
		return err
	}
	found = append(found, ScanGoFlags(os.Getenv("GOFLAGS"))...)
	if len(found) == 0 {
		rc.emit("scan", "", "clean")
	} else {
		rc.emit("scan", "", fmt.Sprintf("%d forbidden construct(s)", len(found)))
		reason := "forbidden_source"
		for _, f := range found {
			if f.File == "GOFLAGS" || filepath.Base(f.File) == "go.mod" {
				reason = "forbidden_go_mod"
			}
		}
		return &stopError{Status: "failed", Reason: reason, Message: "executor: Wave 0: " + describeFindings(rc.plan, found)}
	}

	// A test may write files while the checks run. Whatever the checks add that
	// was not there before is removed again (new files only), also when a check
	// fails so that a retry starts from the declared files; a declared file whose
	// content changed stops the run.
	before, err := TakeSnapshot(repo, rc.git)
	if err != nil {
		return errors.New("executor: the repository state could not be read")
	}
	checkErr := rc.wave0Checks(ctx)
	cleanErr := rc.cleanCheckOutput(before)
	if checkErr != nil {
		return checkErr
	}
	if cleanErr != nil {
		return cleanErr
	}

	paths := append([]string{}, rc.allowDirty()...)
	paths = append(paths, typePaths...)
	paths = append(paths, stubs...)
	paths = append(paths, "go.mod")
	if fi, err := os.Lstat(filepath.Join(repo, "go.sum")); err == nil && fi.Mode().IsRegular() {
		paths = append(paths, "go.sum")
	}
	paths = uniqueSorted(paths)
	if !rc.diffOnly {
		if err := rc.onlyDeclaredDirt(paths); err != nil {
			return err
		}
		if se := rc.planIntact(); se != nil {
			return se
		}
		hash, err := rc.git.CommitWave0(paths)
		if err != nil {
			return fmt.Errorf("executor: the Wave 0 commit failed: %w", err)
		}
		rc.emit("wave0_commit", "", hash)
	} else {
		rc.emit("wave0_commit", "", "diff_only: nothing committed")
	}
	rc.state.Wave0Done = true
	return rc.state.Save(rc.o.RunDir)
}

// cleanCheckOutput removes the files the Wave 0 checks created (for example the
// binary of go build on one main package) and stops the run when a check
// changed a file that was already there. Only paths that were clean before are
// deleted, through gitland's guarded Restore.
func (rc *runCtx) cleanCheckOutput(before Snapshot) error {
	stray, err := before.Stray(rc.o.Repo, rc.git)
	if err != nil {
		return errors.New("executor: the repository state could not be read")
	}
	var created, changed []string
	for _, p := range stray {
		if _, was := before[p]; was {
			changed = append(changed, p)
		} else {
			created = append(created, p)
		}
	}
	if len(changed) > 0 {
		return &stopError{Status: "failed", Reason: "wave0_stray",
			Message: fmt.Sprintf("executor: a Wave 0 check changed %d existing file(s): %s", len(changed), firstThree(changed))}
	}
	if len(created) > 0 {
		if err := Revert(rc.git, created); err != nil {
			return fmt.Errorf("executor: removing the output of the Wave 0 checks failed: %w", err)
		}
		rc.emit("wave0_cleanup", "", fmt.Sprintf("removed %d file(s) made by the checks", len(created)))
	}
	return nil
}

func firstThree(ps []string) string {
	if len(ps) > 3 {
		ps = ps[:3]
	}
	return strings.Join(ps, ", ")
}

// onlyDeclaredDirt refuses to commit Wave 0 when anything outside the declared
// files changed: a go command or a stray write must not slip into the commit
// or stay behind it.
func (rc *runCtx) onlyDeclaredDirt(paths []string) error {
	declared := map[string]bool{}
	for _, p := range paths {
		declared[p] = true
	}
	dirty, err := rc.git.Dirty()
	if err != nil {
		return errors.New("executor: the repository state could not be read")
	}
	var extra []string
	for _, p := range dirty {
		if !declared[p] {
			extra = append(extra, p)
		}
	}
	if len(extra) == 0 {
		return nil
	}
	shown := extra
	if len(shown) > 3 {
		shown = shown[:3]
	}
	return &stopError{Status: "failed", Reason: "wave0_stray",
		Message: fmt.Sprintf("executor: Wave 0 left %d file(s) outside the declared set: %s", len(extra), strings.Join(shown, ", "))}
}

func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// locations is " at file:line, file:line" (at most 5), or "". Positions only.
func locations(ls []runner.Location) string {
	if len(ls) == 0 {
		return ""
	}
	var parts []string
	for i, l := range ls {
		if i == 5 {
			parts = append(parts, fmt.Sprintf("and %d more", len(ls)-5))
			break
		}
		parts = append(parts, fmt.Sprintf("%s:%d", l.File, l.Line))
	}
	return " at " + strings.Join(parts, ", ")
}

// faultOf is a harness fault or a cancellation of a check, as an error.
func faultOf(v runner.Verdict, what string) error {
	if v.Class == runner.ClassCancelled {
		return fmt.Errorf("executor: %s was cancelled", what)
	}
	return fmt.Errorf("executor: %s could not run (%w)", what, v.Err)
}

// wave0Checks is the build check of everything, then the red check.
func (rc *runCtx) wave0Checks(ctx context.Context) error {
	env, err := rc.env("wave0", envLeaf)
	if err != nil {
		return err
	}
	if v := rc.chk.BuildVet(ctx, rc.o.Repo, env); !v.Pass() {
		if v.Faulted() {
			return faultOf(v, "the Wave 0 build check")
		}
		return &stopError{Status: "failed", Reason: "wave0_build",
			Message: "executor: Wave 0 build check failed (" + v.Class + ")" + locations(v.Locations)}
	}
	return rc.redCheck(ctx)
}

// redCheck runs each leaf's test against its stub, once, before its first model
// call. A failure is the expected outcome. A pass is a weak test (warned, the
// run continues). A build or vet failure is a plan defect and stops the run.
func (rc *runCtx) redCheck(ctx context.Context) error {
	for _, l := range rc.plan.Leaves {
		if rc.state.RedChecked[l.ID] {
			continue
		}
		v := rc.leafCheck(ctx, l, []string{l.StubFile})
		switch {
		case v.Faulted():
			return faultOf(v, "the red check of "+l.ID)
		case v.Pass():
			rc.rep.weak++
			rc.emit("weak_test", l.ID, "the test passes against the stub")
		case v.Class == runner.ClassBuild || v.Class == runner.ClassVet:
			return &stopError{Status: "failed", Reason: "wave0_build",
				Message: fmt.Sprintf("executor: the red check of %s failed to %s (a plan defect)%s", l.ID, v.Class, locations(v.Locations))}
		}
		rc.state.RedChecked[l.ID] = true
		if err := rc.state.Save(rc.o.RunDir); err != nil {
			return err
		}
	}
	return nil
}
