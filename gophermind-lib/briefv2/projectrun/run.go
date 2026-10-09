package projectrun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/envcheck"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/executor"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/settings"
	"gophermind/gophermind-lib/briefv2/tree"
	"gophermind/gophermind-lib/briefv2/vault"
)

// runPlanner is the one planner call of a run. Tests replace it to watch or
// wrap what the planner is given; production never does.
var runPlanner = func(ctx context.Context, d planner.Deps, o planner.Options) (planner.Outcome, error) {
	return planner.New(d).Run(ctx, o)
}

// deps is everything Run builds once the preflight has passed. There is no
// database handle: the stores are files in the run folder.
type deps struct {
	cfg   *settings.Config
	vlt   *vault.Vault // nil when the brief declares no secret and no provider needs a key
	rt    *router.Router
	board blackboard.Blackboard
	led   ledger.Ledger
}

// open builds the router and the stores. It uses pre.Config (the base URLs
// that answered), reads the vault passphrase from the environment only and
// never prompts. The vault is opened only when the brief declares a secret or
// a provider names an api_key_secret; the preflight's open vault is reused.
func (e Env) open(_ context.Context, _ Options, pre PreflightResult, b *brief.Brief, sink events.Sink) (*deps, error) {
	cfg := pre.Config
	if cfg == nil {
		return nil, errors.New("projectrun: the preflight left no settings")
	}
	needVault := len(b.Front.Secrets) > 0
	for _, pc := range cfg.Providers {
		if pc.APIKeySecret != "" {
			needVault = true
		}
	}
	var vlt *vault.Vault
	if needVault {
		if v, ok := pre.Store.(*vault.Vault); ok && v != nil {
			vlt = v
		} else {
			pass := e.Getenv(vault.PassphraseEnv)
			if pass == "" {
				return nil, fmt.Errorf("projectrun: %s is empty", vault.PassphraseEnv)
			}
			path, err := e.VaultPath()
			if err != nil {
				return nil, fmt.Errorf("projectrun: vault path: %w", err)
			}
			if vlt, err = e.OpenVault(path, pass); err != nil {
				return nil, errors.New("projectrun: the vault did not open with this passphrase")
			}
		}
	}
	providers, err := e.BuildProviders(cfg, func(name string) (string, error) {
		if vlt == nil {
			return "", fmt.Errorf("provider key %s: no vault is open", name)
		}
		val, ok := vlt.Get(vault.HarnessScope, name)
		if !ok {
			return "", fmt.Errorf("provider key %s is not in the vault; set it with `gophermind brief vault set %s`", name, name)
		}
		return val, nil
	})
	if err != nil {
		return nil, fmt.Errorf("projectrun: providers: %w", err)
	}
	board, led := e.Backends()
	rt := router.New(cfg, providers, led, sink, router.WithAllowPublic(false))
	return &deps{cfg: cfg, vlt: vlt, rt: rt, board: board, led: led}, nil
}

// progressWriter hands progress lines to a goroutine through a bounded queue.
// Write never blocks: when the queue is full (a stuck stderr) the line is
// dropped and counted. The Sink writes while holding its mutex, so this is
// what keeps a slow terminal from stalling the planner or the executor.
type progressWriter struct {
	ch      chan []byte
	done    chan struct{}
	mu      sync.RWMutex
	closed  bool
	dropped atomic.Int64
}

const progressQueue = 4096

func newProgressWriter(w io.Writer) *progressWriter {
	p := &progressWriter{ch: make(chan []byte, progressQueue), done: make(chan struct{})}
	go func() {
		defer close(p.done)
		for line := range p.ch {
			_, _ = w.Write(line)
		}
	}()
	return p
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !p.closed {
		select {
		case p.ch <- append([]byte(nil), b...):
		default:
			p.dropped.Add(1)
		}
	}
	return len(b), nil
}

// Close stops accepting lines and waits briefly for the queue to drain; a
// writer that is still stuck after that is abandoned.
func (p *progressWriter) Close() {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		close(p.ch)
	}
	p.mu.Unlock()
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
	}
}

var stageErrRE = regexp.MustCompile(`^planner: ([a-z]+): `)

var planStages = map[string]bool{"clarify": true, "confirm": true, "contract": true, "decompose": true, "enrich": true, "coverage": true, "approve": true, "testwriter": true}

// planStopReason names the planner stage an error came from: the stage in the
// "planner: <stage>:" prefix, plan:load for an error with no stage prefix
// (Load runs before the stages), plan:unknown for a prefix naming no stage.
func planStopReason(err error) string {
	m := stageErrRE.FindStringSubmatch(err.Error())
	switch {
	case m == nil || m[1] == "load":
		return "plan:load"
	case planStages[m[1]]:
		return "plan:" + m[1]
	}
	return "plan:unknown"
}

func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }

// Run plans a brief and builds it in one process: preflight, state, planner,
// approval, executor and report (spec sections 4 and 5).
func Run(ctx context.Context, o Options, env Env) (result Result) {
	// Run never panics. stage names what was running; a panic is reported with
	// a fixed message and the stage, never the panic value, which could carry a
	// secret. Once the report exists the usual finish path still runs.
	// The deferred recover covers Run's own goroutine only. A panic in a
	// goroutine the executor starts (executor/schedule.go workers, limits.go,
	// serve.go, leaf.go) is outside its reach and is the executor's to recover.
	stage := "preflight"
	var finish func(status, stop, note string) Result
	defer func() {
		if r := recover(); r == nil {
			return
		}
		func() {
			defer func() {
				if recover() != nil {
					result = Result{ExitFault, "harness_fault", "harness_fault"}
				}
			}()
			fmt.Fprintf(o.Err, "error: internal fault in the %s stage; the run was stopped\n", stage)
			result = Result{ExitFault, "harness_fault", "harness_fault"}
			if finish != nil {
				result = finish("harness_fault", "harness_fault", "")
			}
		}()
	}()
	if o.Out == nil {
		o.Out = io.Discard
	}
	if o.Err == nil {
		o.Err = io.Discard
	}
	raw, err := os.ReadFile(o.BriefPath)
	if err != nil {
		fmt.Fprintf(o.Err, "error: the brief could not be read: %v\n", err)
		return Result{ExitFailed, "failed", "brief_unreadable"}
	}
	b, err := brief.Parse(raw)
	if err != nil {
		var inv *brief.InvalidError
		if errors.As(err, &inv) {
			fmt.Fprintf(o.Err, "invalid brief: %v\n", err)
			return Result{ExitInvalid, "invalid_brief", "invalid_brief"}
		}
		fmt.Fprintf(o.Err, "error: %v\n", err)
		return Result{ExitFailed, "failed", "brief_unreadable"}
	}
	id := b.Front.ID

	if o.PrintStatePaths {
		ps, err := StatePaths(o, b, env)
		if err != nil {
			fmt.Fprintf(o.Err, "error: %v\n", err)
			return Result{ExitFailed, "failed", "state_paths"}
		}
		PrintStatePaths(o.Out, ps)
		return Result{}
	}

	pre := Preflight(ctx, o, env, b)
	if len(Failed(pre.Checks)) > 0 {
		PrintPreflight(o.Err, pre.Checks)
		return Result{ExitPreflight, "preflight_failed", "preflight"}
	}
	if o.PreflightOnly {
		PrintPreflight(o.Out, pre.Checks)
		return Result{}
	}

	repo := o.Repo
	if repo == "" {
		repo = b.Front.Repo
	}
	if abs, err := planner.ResolveRepo(repo); err == nil {
		repo = abs
	}
	binPath, perr := env.Executable()
	if perr != nil {
		binPath = "unknown"
	}
	ver := env.Version()
	rep := &ProjectReport{
		RunID: id, Title: b.Front.Title,
		Binary:    BinaryInfo{Path: binPath, Version: ver.Version, Commit: ver.Commit, Date: ver.Date},
		Mode:      "unattended",
		Graded:    o.Graded,
		Resumed:   o.Resume,
		Repo:      RepoInfo{Path: repo, BriefRepo: b.Front.Repo, BaseBranch: b.Front.BaseBranch},
		Preflight: pre.Checks, StartedAt: stamp(env.Now()),
		Ambiguity: AmbiguityInfo{BriefSetting: b.Front.OnAmbiguity, MilestoneApprovals: b.Front.MilestoneApprovals,
			ByAnsweredBy: map[string]int{}, ClarifyDefaulted: []ClarifyDefault{}},
		PlannerWarningLines: []string{}, Stages: []StageInfo{}, ByNodeClass: []ClassStat{}, Secrets: []Provisioned{},
	}
	if o.Attended {
		rep.Mode = "attended"
	}
	rep.Ambiguity.Effective = b.Front.OnAmbiguity
	if !o.Attended {
		rep.Ambiguity.Effective = "assume_and_document (unattended policy overrides " + b.Front.OnAmbiguity + ")"
	}
	if head, err := env.Git(ctx, repo, "rev-parse", "HEAD"); err == nil {
		rep.Repo.HeadAtStart = strings.TrimSpace(head)
	}
	for _, p := range pre.Probes {
		if p.Answered {
			rep.Providers = append(rep.Providers, ProviderInfo{Name: p.Provider, Host: p.Host, Fallback: p.Fallback})
		}
	}

	// From here on every write to Err goes through the bounded progress
	// queue, so a stuck stderr can drop lines but never hold up the run, the
	// report or teardown.
	rawErr := o.Err // prompts go here, synchronously: they block on the user anyway and must never drop
	progress := newProgressWriter(o.Err)
	errw := io.Writer(progress)
	o.Err = errw
	sink := NewSink(progress)
	// No deferred Close: the panic handler above runs after any defer made here
	// and must still be able to write; finish closes the queue.

	// finish ends the run on every path after this point: stamp, write
	// _state/project.json once the run folder exists, print the report, and
	// only then let the progress queue drain (bounded).
	finish = func(status, stop, note string) Result {
		rep.Status, rep.StopReason = status, stop
		rep.ExitCode = ExitCode(status, stop)
		rep.FinishedAt = stamp(env.Now())
		rep.Warnings = sink.Counts()
		rep.GradedValid = true
		if o.Graded && rep.Executor != nil && rep.Executor.Resumed {
			rep.GradedValid, rep.GradedInvalidReason = false, "the run resumed"
		}
		if rec, err := planner.LookupRun(id); err == nil {
			if fi, serr := os.Stat(rec.RunDir); serr == nil && fi.IsDir() {
				rep.RunDir = rec.RunDir
				readPlanFacts(rep, rec.RunDir)
				rep.Warnings.DuplicatesIgnored = duplicatesIgnored(rec.RunDir)
				rep.ProgressDropped = int(progress.dropped.Load())
				if err := WriteReport(rec.RunDir, rep); err != nil {
					fmt.Fprintf(o.Err, "warning: _state/project.json could not be written: %v\n", err)
				}
			}
		}
		fmt.Fprint(o.Out, rep.Text())
		if note != "" {
			fmt.Fprintln(o.Out, note)
		}
		progress.Close()
		return Result{rep.ExitCode, status, stop}
	}

	if o.Attended && o.In == nil {
		fmt.Fprintln(o.Err, "error: --attended needs a terminal to read answers from")
		return finish("failed", "attended_no_input", "")
	}

	stage = "setup"
	d, err := env.open(ctx, o, pre, b, sink)
	if err != nil {
		fmt.Fprintf(o.Err, "error: %v\n", err)
		return finish("harness_fault", "harness_fault", "")
	}
	var store SecretStore
	if d.vlt != nil {
		store = d.vlt
	}
	if rep.Secrets, err = ProvisionGenerated(store, b, o.Generate, o.Resume, env.Rand); err != nil {
		fmt.Fprintf(o.Err, "error: %v\n", err)
		return finish("harness_fault", "harness_fault", "")
	}
	if rep.Secrets == nil {
		rep.Secrets = []Provisioned{}
	}

	var gate human.Gate
	deps := planner.Deps{Caller: d.rt, Sink: sink, Board: d.board, Settings: d.cfg, LedgerErrors: d.rt.LedgerErrors}
	if d.vlt != nil {
		deps.OpenSecrets = func() (planner.Secrets, error) { return d.vlt, nil }
	}
	if o.Attended {
		gate = human.NewTerminal(o.In, rawErr)
		deps.PromptSecret = func(name, purpose string) (string, error) {
			return vault.ReadSecret(fmt.Sprintf("Value for %s (%s): ", name, purpose), o.In, rawErr)
		}
	} else {
		gate = newUnattendedGate(func() (string, error) {
			rec, err := planner.LookupRun(id)
			return rec.RunDir, err
		})
	}
	deps.Gate = gate

	po := planner.Options{Repo: o.Repo, Unattended: !o.Attended}
	if o.Resume {
		po.RunID = id
	} else {
		po.BriefPath = o.BriefPath
	}
	stage = "planner"
	outcome, perr2 := runPlanner(ctx, deps, po)
	if o.Attended {
		rep.Secrets = markPrompted(rep.Secrets, store, id)
	}
	switch {
	case perr2 != nil:
		var inv *brief.InvalidError
		switch {
		case errors.Is(perr2, context.Canceled) || errors.Is(perr2, context.DeadlineExceeded) || ctx.Err() != nil:
			return finish("interrupted", "interrupted", "interrupted: resume with --resume")
		case errors.As(perr2, &inv):
			fmt.Fprintf(o.Err, "invalid brief: %v\n", perr2)
			return finish("invalid_brief", "invalid_brief", "")
		}
		fmt.Fprintf(o.Err, "error: %v\n", perr2)
		return finish("failed", planStopReason(perr2), "")
	case outcome != planner.Done:
		fmt.Fprintf(o.Err, "error: the planner is waiting for an answer in a run that has nobody to ask (outcome %q)\n", outcome)
		return finish("failed", "plan:waiting", "")
	}

	rec, err := planner.LookupRun(id)
	if err != nil {
		fmt.Fprintf(o.Err, "error: %v\n", err)
		return finish("harness_fault", "harness_fault", "")
	}
	var secrets executor.Secrets
	if len(b.Front.Secrets) > 0 && d.vlt != nil {
		secrets = d.vlt
	}
	xo := executor.Options{
		RunDir: rec.RunDir, Repo: rec.Repo, Caller: d.rt, Board: d.board, Ledger: d.led,
		Gate: gate, Sink: sink, Settings: d.cfg, Secrets: secrets, LedgerErrors: d.rt.LedgerErrors,
		EnvNotes: envcheck.EnvNotes(pre.Probes),
	}
	stage = "executor"
	xr, xerr := env.RunExecutor(ctx, xo)
	if xerr != nil {
		var inv *brief.InvalidError
		if errors.As(xerr, &inv) {
			fmt.Fprintf(o.Err, "invalid brief: %v\n", xerr)
			return finish("invalid_brief", "invalid_brief", "")
		}
		fmt.Fprintf(o.Err, "error: %s\n", xerr)
		return finish("harness_fault", "harness_fault", "")
	}
	stage = "report"
	rep.Executor = &xr
	rep.Resumed = o.Resume || xr.Resumed
	rep.ByNodeClass = nodeClassTable(ctx, d, id, rec.RunDir, errw)
	note := ""
	if xr.Status == "interrupted" {
		note = "interrupted: resume with --resume"
	}
	return finish(xr.Status, xr.StopReason, note)
}

// nodeClassTable builds by_node_class from the blackboard rows of the leaves, the class map
// and the implement calls. A read error leaves the table empty with a warning.
func nodeClassTable(ctx context.Context, d *deps, id, runDir string, errw io.Writer) []ClassStat {
	rows, err := d.board.List(ctx, id, blackboard.Filter{})
	if err != nil {
		fmt.Fprintf(errw, "warning: node class table not built: blackboard rows: %v\n", err)
		return []ClassStat{}
	}
	// The board holds a row for the root and every component as well; the
	// table is about leaves, so keep the function nodes only.
	t, err := tree.NewStore(runDir).Load()
	if err != nil {
		fmt.Fprintf(errw, "warning: node class table not built: plan tree: %v\n", err)
		return []ClassStat{}
	}
	leaves := rows[:0:0]
	for _, r := range rows {
		if n, ok := t.Nodes[r.NodeID]; ok && n.Kind == tree.KindFunction {
			leaves = append(leaves, r)
		}
	}
	rows = leaves
	classes, err := planner.ReadClasses(runDir)
	if err != nil {
		fmt.Fprintf(errw, "warning: node class table not built: classes: %v\n", err)
		return []ClassStat{}
	}
	calls, err := d.led.List(ctx, id, ledger.Filter{TaskType: "implement"})
	if err != nil {
		fmt.Fprintf(errw, "warning: node class table not built: ledger: %v\n", err)
		return []ClassStat{}
	}
	stats := ClassStats(rows, classes, calls)
	if stats == nil {
		stats = []ClassStat{}
	}
	return stats
}

func duplicatesIgnored(runDir string) int {
	_, total, _, err := planner.IgnoredDuplicates(runDir)
	if err != nil {
		return 0
	}
	return total
}

// readPlanFacts fills the report from the files the planner left, as far as
// they exist. It runs on every path, so a plan that stopped at a stage still
// reports the stages it finished and the coverage it reached. A corrupt
// coverage.json is a warning; a missing one is zero covered.
func readPlanFacts(rep *ProjectReport, runDir string) {
	if answers, err := planner.ReadAnswers(runDir); err == nil {
		for _, a := range answers {
			if a.Assumed && a.Stage == "clarify" {
				rep.Ambiguity.ClarifyDefaulted = append(rep.Ambiguity.ClarifyDefaulted, ClarifyDefault{ID: a.ID, Question: a.Question, Answer: a.Answer})
			}
		}
	}
	if qc, err := planner.ReadQuestionCounts(runDir); err == nil {
		rep.Ambiguity.ByAnsweredBy = qc.ByAnsweredBy
		rep.Ambiguity.Rounds, rep.Ambiguity.ClarifyCalls = qc.Rounds, qc.Calls
		rep.Ambiguity.ConservativeAssumptions = qc.MidStageAssumed
	}
	if u, ok, err := planner.ReadUnderstanding(runDir); err == nil && ok {
		rep.Understanding = UnderstandingInfo{ConfirmedBy: u.ConfirmedBy, Hash: u.Hash}
	}
	if ap, ok, err := planner.ReadApproval(runDir); err == nil && ok {
		rep.Approval = ApprovalInfo{By: ap.ApprovedBy, PlanHash: ap.PlanHash, UnderstandingHash: ap.UnderstandingHash}
	}
	if reqs, err := planner.ReadRequirements(runDir); err == nil {
		rep.Plan.RequirementsCovered.Total = len(reqs)
		for _, r := range reqs {
			if r.Kind == planner.ReqAcceptance {
				rep.Plan.AcceptanceTotal++
			}
		}
		if cov, err := planner.ReadCoverage(runDir); err == nil {
			rep.Plan.Warnings = len(cov.Warnings)
			seen := map[string]bool{}
			for _, c := range cov.Covered {
				seen[c.Requirement] = true
			}
			rep.Plan.RequirementsCovered.Covered = len(seen)
		} else if !errors.Is(err, planner.ErrNoCoverage) {
			rep.CoverageError = "coverage.json could not be read: " + planner.JSONErr(err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		rep.CoverageError = "requirements.json could not be read: " + planner.JSONErr(err)
	}
	if t, err := tree.NewStore(runDir).Load(); err == nil {
		maxWave := -1
		for _, n := range t.Nodes {
			if n.Kind != tree.KindFunction {
				continue
			}
			rep.Plan.Functions++
			if n.Wave != nil && *n.Wave > maxWave {
				maxWave = *n.Wave
			}
		}
		rep.Plan.Waves = maxWave + 1
	}
	if st, err := planner.ReadStatus(rep.RunID); err == nil {
		for _, s := range st.Stages {
			status := "pending"
			if s.Done {
				status = "done"
			}
			rep.Stages = append(rep.Stages, StageInfo{Name: s.Name, Status: status})
		}
	}
	if _, total, _, err := planner.IgnoredDuplicates(runDir); err == nil && total > 0 {
		rep.PlannerWarningLines = append(rep.PlannerWarningLines, fmt.Sprintf("duplicates ignored: %d", total))
	}
}
