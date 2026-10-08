package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"text/tabwriter"

	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/report"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/settings"
	"gophermind/gophermind-lib/briefv2/vault"
)

// Exit codes of `gophermind brief plan` and `resume`.
const (
	exitDone    = 0
	exitError   = 1
	exitInvalid = 2
	exitWaiting = 3
)

// printSink writes the planner's progress to the terminal. Model calls are
// not printed one by one; `gophermind brief calls` shows them.
type printSink struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *printSink) Emit(e events.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch e.Kind {
	case events.KindStageStarted:
		fmt.Fprintf(s.w, "%s: started\n", e.Stage)
	case events.KindStageFinished:
		fmt.Fprintf(s.w, "%s: done\n", e.Stage)
	case events.KindWaitingOnHuman:
		fmt.Fprintf(s.w, "%s: %s\n", e.Stage, e.Message)
	case events.KindWarning:
		fmt.Fprintf(s.w, "warning: %s\n", e.Message)
	case events.KindCoverageGap:
		fmt.Fprintf(s.w, "coverage gap, %s\n", e.Message)
	case events.KindLedgerError:
		fmt.Fprintf(s.w, "warning: a model call could not be recorded in the ledger: %s\n", e.Message)
	}
}

// lazyFileGate is the file gate for a run whose folder is only known once
// Load has created it: it looks the run up each time it is used.
type lazyFileGate struct{ runID string }

func (g lazyFileGate) file() (*human.File, error) {
	rec, err := planner.LookupRun(g.runID)
	if err != nil {
		return nil, err
	}
	return human.NewFile(rec.RunDir), nil
}

func (g lazyFileGate) Ask(ctx context.Context, qs []human.Question) ([]human.Answer, error) {
	f, err := g.file()
	if err != nil {
		return nil, err
	}
	return f.Ask(ctx, qs)
}

func (g lazyFileGate) Approve(ctx context.Context, plan human.PlanSummary) (human.Decision, error) {
	f, err := g.file()
	if err != nil {
		return human.Decision{}, err
	}
	return f.Approve(ctx, plan)
}

func (g lazyFileGate) Escalate(ctx context.Context, e human.Escalation) (human.Resolution, error) {
	f, err := g.file()
	if err != nil {
		return human.Resolution{}, err
	}
	return f.Escalate(ctx, e)
}

// briefPlan implements `gophermind brief plan <brief.md>` and
// `gophermind brief resume <run-id>`.
func briefPlan(verb string, args []string, in *os.File, out, errw io.Writer) int {
	fs := flag.NewFlagSet("brief "+verb, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	yes := fs.Bool("yes", false, "approve the plan without showing it")
	gateMode := fs.String("gate", "", "terminal or file")
	fake := fs.String("fake", "", "answer every model call from this fixture directory (several, comma separated: the first holding a reply wins)")
	allowPublic := fs.Bool("allow-public", false, "let public providers see the brief")
	// The target comes first, then the flags; flags first also works.
	target := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		target, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(errw, briefUsage)
		return exitError
	}
	if target == "" && fs.NArg() == 1 {
		target = fs.Arg(0)
	} else if fs.NArg() != 0 {
		target = ""
	}
	if target == "" {
		fmt.Fprintln(errw, briefUsage)
		return exitError
	}

	opts := planner.Options{Yes: *yes, AllowPublic: *allowPublic}
	runID := target
	if verb == "plan" {
		src, err := os.ReadFile(target)
		if err != nil {
			fmt.Fprintf(errw, "error: %v\n", err)
			return exitError
		}
		b, err := brief.Parse(src)
		if err != nil {
			return briefFail(errw, err)
		}
		opts.BriefPath, runID = target, b.Front.ID
	} else {
		opts.RunID = target
	}

	var cfg *settings.Config
	var providers map[string]provider.Provider
	var v *vault.Vault
	openVault := func() (*vault.Vault, error) {
		if v != nil {
			return v, nil
		}
		path, err := vaultPath()
		if err != nil {
			return nil, err
		}
		pass, err := vault.Passphrase("Vault passphrase: ", in, errw)
		if err != nil {
			return nil, err
		}
		v, err = vault.Open(path, pass, vaultOptions)
		return v, err
	}
	if *fake != "" {
		fp, err := planner.FixtureProvider(strings.Split(*fake, ",")...)
		if err != nil {
			fmt.Fprintf(errw, "error: %v\n", err)
			return exitError
		}
		cfg, providers = planner.FixtureSettings(), map[string]provider.Provider{"fake": fp}
	} else {
		path, err := settings.Path()
		if err != nil {
			fmt.Fprintf(errw, "error: %v\n", err)
			return exitError
		}
		if cfg, err = settings.Load(path); err != nil {
			fmt.Fprintf(errw, "error: %v\n", err)
			return exitError
		}
		// Providers get a client that never follows a redirect and ignores the proxy
		// environment. The router bounds every call with its call timeout.
		providers, err = cfg.BuildProviders(providerHTTPClient(cfg), func(name string) (string, error) {
			vlt, err := openVault()
			if err != nil {
				return "", err
			}
			val, ok := vlt.Get(vault.HarnessScope, name)
			if !ok {
				return "", fmt.Errorf("not in the vault; set it with `gophermind brief vault set %s`", name)
			}
			return val, nil
		})
		if err != nil {
			fmt.Fprintf(errw, "error: %v\n", err)
			return exitError
		}
	}

	mode := *gateMode
	if mode == "" {
		mode = cfg.Human.Mode
	}
	deps := planner.Deps{Settings: cfg, OpenSecrets: func() (planner.Secrets, error) { return openVault() }}
	switch mode {
	case "terminal":
		deps.Gate = human.NewTerminal(in, errw)
		deps.PromptSecret = func(name, purpose string) (string, error) {
			return vault.ReadSecret(fmt.Sprintf("Value for %s (%s): ", name, purpose), in, errw)
		}
	case "file":
		deps.Gate = lazyFileGate{runID: runID}
	default:
		fmt.Fprintf(errw, "error: unknown gate %q (want terminal or file)\n", mode)
		return exitError
	}

	board, led := briefBackends()
	sink := &printSink{w: errw}
	rt := router.New(cfg, providers, led, sink, router.WithAllowPublic(*allowPublic))
	deps.Caller, deps.Sink, deps.Board, deps.LedgerErrors = rt, sink, board, rt.LedgerErrors

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	outcome, err := planner.New(deps).Run(ctx, opts)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintf(errw, "interrupted; continue with `gophermind brief resume %s`\n", runID)
			return exitError
		}
		return briefFail(errw, err)
	}
	if outcome == planner.Waiting {
		if rec, lerr := planner.LookupRun(runID); lerr == nil {
			fmt.Fprintf(out, "waiting: answer in %s, then run `gophermind brief resume %s`\n", rec.RunDir, runID)
		}
		return exitWaiting
	}
	st, err := planner.ReadStatus(runID)
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return exitError
	}
	fmt.Fprintf(out, "planned: %s in %s\nRequirements covered: %d of %d\n", st.RunID, st.RunDir, st.Covered, st.Requirements)
	return exitDone
}

// briefFail prints err and returns the exit code it deserves: 2 when the
// brief itself is wrong, 1 for everything else.
func briefFail(errw io.Writer, err error) int {
	var inv *brief.InvalidError
	if errors.As(err, &inv) {
		fmt.Fprintf(errw, "invalid brief: %v\n", err)
		return exitInvalid
	}
	fmt.Fprintf(errw, "error: %v\n", err)
	return exitError
}

// briefStatus implements `gophermind brief status <run-id>`.
func briefStatus(runID string, out, errw io.Writer) int {
	st, err := planner.ReadStatus(runID)
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return exitError
	}
	fmt.Fprintf(out, "run %s\nfolder: %s\nrepository: %s\n\nstages:\n", st.RunID, st.RunDir, st.Repo)
	for _, s := range st.Stages {
		state := "not done"
		if s.Done {
			state = "done"
		}
		fmt.Fprintf(out, "  %-11s %s\n", s.Name, state)
	}
	fmt.Fprintln(out)
	if st.Waiting != "" {
		fmt.Fprintf(out, "waiting on a human: %s\n", st.Waiting)
	}
	fmt.Fprintf(out, "Requirements covered: %d of %d\n", st.Covered, st.Requirements)
	if st.AllowPublic {
		fmt.Fprintln(out, "public providers were allowed to see the brief in this run (--allow-public)")
	}
	if st.LedgerErrors > 0 {
		fmt.Fprintf(out, "incomplete ledger: %d model call(s) could not be recorded\n", st.LedgerErrors)
	}

	board, led := briefBackends()
	sum, err := led.Summary(context.Background(), runID)
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return exitError
	}
	var rep *report.Report
	if r, rerr := report.Read(st.RunDir); rerr == nil {
		rep = &r
	}
	lines, err := executorLines(context.Background(), board, planner.RunRecord{RunID: st.RunID, RunDir: st.RunDir, Repo: st.Repo}, rep)
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return exitError
	}
	for _, l := range lines {
		fmt.Fprintln(out, l)
	}
	fmt.Fprintln(out, "\nmodel calls by task type and node class:")
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "  TASK\tCLASS\tMODEL\tCALLS\tOK\tMALFORMED\tOTHER\tPROMPT TOK\tREPLY TOK\tSECONDS")
	for _, m := range sum {
		ok, bad := m.Outcomes[ledger.OutcomeOK], m.Outcomes[ledger.OutcomeMalformed]
		fmt.Fprintf(tw, "  %s\t%s\t%s/%s\t%d\t%d\t%d\t%d\t%d\t%d\t%.1f\n", m.TaskType, dash(m.NodeClass), m.Provider, m.Model,
			m.Calls, ok, bad, m.Calls-ok-bad, m.PromptTokens, m.CompletionTokens, float64(m.TotalMS)/1000)
	}
	tw.Flush()
	return exitDone
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// briefCalls implements `gophermind brief calls <run-id>`: the ledger as a table.
func briefCalls(runID string, out, errw io.Writer) int {
	if _, err := planner.LookupRun(runID); err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return exitError
	}
	_, led := briefBackends()
	rows, err := led.List(context.Background(), runID, ledger.Filter{})
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return exitError
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "TIME\tSTAGE\tTASK\tCLASS\tMODEL SERVED\tOUTCOME\tPROMPT TOK\tREPLY TOK\tMS")
	for _, c := range rows {
		model := c.ModelServed
		if model == "" {
			model = c.ModelRequested
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s/%s\t%s\t%d\t%d\t%d\n", c.At.UTC().Format("15:04:05"), c.Stage, c.TaskType, dash(c.NodeClass),
			c.Provider, model, c.Outcome, c.PromptTokens, c.CompletionTokens, c.DurationMS)
	}
	tw.Flush()
	fmt.Fprintf(out, "%d call(s)\n", len(rows))
	return exitDone
}

// briefCoverage implements `gophermind brief coverage <run-id>`: what covers
// each requirement of the brief, and the warnings.
func briefCoverage(runID string, out, errw io.Writer) int {
	rec, err := planner.LookupRun(runID)
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return exitError
	}
	cov, err := planner.ReadCoverage(rec.RunDir)
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return exitError
	}
	src, err := os.ReadFile(filepath.Join(rec.RunDir, "brief.md"))
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return exitError
	}
	reqs := planner.ParseRequirements(src)
	text := map[string]string{}
	for _, q := range reqs {
		text[q.ID] = q.Text
		if q.Name != "" {
			text[q.ID] = q.Name
		}
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "REQUIREMENT\tTEXT\tCOVERED BY\tROOT TESTS")
	for _, c := range cov.Covered {
		short := strings.Join(strings.Fields(text[c.Requirement]), " ")
		if rs := []rune(short); len(rs) > 60 {
			short = string(rs[:60])
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", c.Requirement, short, dash(strings.Join(c.Nodes, ", ")), dash(strings.Join(c.RootTests, ", ")))
	}
	tw.Flush()
	fmt.Fprintf(out, "\nRequirements covered: %d of %d (fill rounds: %d)\n", len(cov.Covered), len(reqs), cov.Rounds)
	for _, w := range cov.Warnings {
		fmt.Fprintf(out, "warning: %s\n", w)
	}
	return exitDone
}
