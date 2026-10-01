package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/term"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/executor"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/report"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/settings"
	"gophermind/gophermind-lib/briefv2/tree"
	"gophermind/gophermind-lib/briefv2/vault"
)

// Exit codes beyond those in brief_plan.go (spec 19).
const (
	exitEscalated   = 4 // an escalated leaf or a human stop
	exitInterrupted = 5 // signal or max_run_minutes; resumable
	exitPreflight   = 6 // `brief run --check-env` found a failed check; not a run
)

// runHook is nil in production. A test sets it to replace settings loading and
// provider construction.
var runHook func() (*settings.Config, map[string]provider.Provider, error)

// runExecutor is executor.Run; tests replace it to see the Options or to
// return a canned Report.
var runExecutor = executor.Run

// exitFor maps a report to the process exit code (spec 19).
func exitFor(r executor.Report) int { return report.ExitCode(r.Status, r.StopReason) }

// parseRunArgs reads `<id> [flags]` or `[flags] <id>`; ok is false for any
// other shape.
func parseRunArgs(name string, args []string, setup func(*flag.FlagSet)) (id string, ok bool) {
	fs := flag.NewFlagSet("brief "+name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	setup(fs)
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return "", false
	}
	if id == "" && fs.NArg() == 1 {
		id = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return "", false
	}
	return id, id != ""
}

// errNoPassphrase is the fixed message of a run that needs the vault with no
// passphrase and no terminal: a non-interactive run never blocks on a prompt.
var errNoPassphrase = errors.New("the vault passphrase is not set: set GOPHERMIND_VAULT_PASSPHRASE (no terminal to prompt)")

func runPassphrase(in *os.File, errw io.Writer) (string, error) {
	if v := os.Getenv(vault.PassphraseEnv); v != "" {
		return v, nil
	}
	if in != nil && term.IsTerminal(int(in.Fd())) {
		p, err := vault.ReadLine("Vault passphrase: ", in, errw)
		if err == nil && p != "" {
			return p, nil
		}
	}
	return "", errNoPassphrase
}

// briefRun implements `gophermind brief run <id> [--gate terminal|file] [--repo <path>] [--workers n]`
// and `gophermind brief run <id> --check-env`.
func briefRun(args []string, in *os.File, out, errw io.Writer) int {
	var gateMode, repoFlag string
	workers := 1
	var checkEnv bool
	id, ok := parseRunArgs("run", args, func(fs *flag.FlagSet) {
		fs.StringVar(&gateMode, "gate", "", "terminal or file")
		fs.StringVar(&repoFlag, "repo", "", "run against this clone instead of the brief's repo")
		fs.IntVar(&workers, "workers", 1, "must be 1")
		fs.BoolVar(&checkEnv, "check-env", false, "check the environment without a model call")
	})
	if !ok {
		fmt.Fprintln(errw, briefUsage)
		return exitError
	}
	if err := executor.CheckWorkers(workers); err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return exitError
	}
	rec, err := planner.LookupRun(id)
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return exitError
	}
	repo := rec.Repo
	if repoFlag != "" {
		abs, err := filepath.Abs(repoFlag)
		if fi, serr := os.Stat(abs); err != nil || serr != nil || !fi.IsDir() {
			fmt.Fprintln(errw, "error: --repo must be an existing directory that holds a git repository")
			return exitError
		}
		if _, err := os.Lstat(filepath.Join(abs, ".git")); err != nil {
			fmt.Fprintln(errw, "error: --repo must be an existing directory that holds a git repository")
			return exitError
		}
		repo = abs
	}
	if !checkEnv {
		if err := planner.VerifyApproval(rec.RunDir); err != nil {
			fmt.Fprintf(errw, "error: the plan is not approved (approval.json is missing or does not match the plan); run gophermind brief resume %s to approve it\n", id)
			return exitError
		}
	}

	var v *vault.Vault
	openVault := func() (*vault.Vault, error) {
		if v != nil {
			return v, nil
		}
		path, err := vaultPath()
		if err != nil {
			return nil, err
		}
		pass, err := runPassphrase(in, errw)
		if err != nil {
			return nil, err
		}
		v, err = vault.Open(path, pass, vaultOptions)
		return v, err
	}

	ctx := context.Background()
	var cfg *settings.Config
	var providers map[string]provider.Provider
	var notes []string
	if runHook != nil {
		if cfg, providers, err = runHook(); err != nil {
			fmt.Fprintf(errw, "error: %v\n", err)
			return exitError
		}
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
	}
	if checkEnv {
		return briefPreflight(ctx, rec, repo, cfg, openVault, in, out)
	}
	if providers == nil {
		var res []probeResult
		client := &http.Client{}
		cfg, res = resolveBaseURLs(ctx, cfg, client)
		for _, r := range res {
			if !r.Answered {
				fmt.Fprintf(errw, "warning: provider %s did not answer within %d s; the router will keep trying\n", r.Provider, int(probeTimeout/time.Second))
			}
		}
		notes = envNotes(res)
		providers, err = cfg.BuildProviders(client, func(name string) (string, error) {
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
	runCfg := *cfg
	runCfg.Executor.Workers = workers

	mode := gateMode
	if mode == "" {
		mode = cfg.Human.Mode
	}
	var gate human.Gate
	switch mode {
	case "terminal":
		gate = human.NewTerminal(in, errw)
	case "file":
		gate = lazyFileGate{runID: id}
	default:
		fmt.Fprintf(errw, "error: unknown gate %q (want terminal or file)\n", mode)
		return exitError
	}

	var secrets executor.Secrets
	src, err := os.ReadFile(filepath.Join(rec.RunDir, "brief.md"))
	if err != nil {
		fmt.Fprintln(errw, "error: the run folder has no readable brief.md")
		return exitError
	}
	b, err := brief.Parse(src)
	if err != nil {
		return briefFail(errw, err)
	}
	if len(b.Front.Secrets) > 0 {
		vlt, err := openVault()
		if err != nil {
			fmt.Fprintf(errw, "error: %v\n", err)
			return exitError
		}
		secrets = vlt
	}

	d, code := openBriefDB(errw)
	if d == nil {
		return code
	}
	defer d.Close()
	sink := &runSink{printSink: &printSink{w: errw}}
	led := ledger.NewSQLite(d)
	rt := router.New(&runCfg, providers, led, sink)
	opts := executor.Options{
		RunDir: rec.RunDir, Repo: repo, Caller: rt, Board: blackboard.NewSQLite(d), Ledger: led,
		Gate: gate, Sink: sink, Settings: &runCfg, Secrets: secrets, LedgerErrors: rt.LedgerErrors, EnvNotes: notes,
	}
	if runCfg.Executor.Sandbox == "off" {
		fmt.Fprintln(errw, "sandbox: off")
	}
	rep, err := runExecutor(ctx, opts)
	return finishRun(id, rec.RunDir, rep, err, out, errw)
}

// finishRun prints what a run left behind, on every path: the error, the
// report path and the summary, whose last two lines are the proof lines.
func finishRun(id, runDir string, rep executor.Report, err error, out, errw io.Writer) int {
	code := exitError
	if err != nil {
		var inv *brief.InvalidError
		if errors.As(err, &inv) {
			code = briefFail(errw, err)
		} else {
			fmt.Fprintf(errw, "error: %s\n", faultMessage(err))
		}
	}
	if rep.RunID == "" { // the executor made no report
		return code
	}
	if err == nil {
		code = exitFor(rep)
	}
	switch {
	case rep.Status == "escalated" && rep.StopReason == "waiting_on_human":
		fmt.Fprintf(errw, "waiting: answer in %s, then run gophermind brief run %s\n", runDir, id)
	case rep.Status == "interrupted":
		fmt.Fprintf(errw, "interrupted; continue with `gophermind brief run %s`\n", id)
	}
	if rep.Sandbox == "off" {
		fmt.Fprintln(errw, "sandbox: off")
	}
	fmt.Fprintf(errw, "report: %s\n", filepath.Join(runDir, report.FileName))
	fmt.Fprint(out, rep.Summary())
	return code
}

// faultMessage words a harness error. The two refusals that write no report
// get fixed messages; everything else is the executor's own error, which never
// quotes a reply, command output or a secret.
func faultMessage(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "plan files changed"):
		return "the plan changed since the run started; nothing was run"
	case strings.Contains(msg, "held by a live worker"):
		return "a leaf is held by a live worker; wait for its claim to go stale and run again"
	}
	return msg
}

// briefReport implements `gophermind brief report <id> [--json]`.
func briefReport(args []string, out, errw io.Writer) int {
	var asJSON bool
	id, ok := parseRunArgs("report", args, func(fs *flag.FlagSet) { fs.BoolVar(&asJSON, "json", false, "print report.json unchanged") })
	if !ok {
		fmt.Fprintln(errw, briefUsage)
		return exitError
	}
	rec, err := planner.LookupRun(id)
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return exitError
	}
	path := filepath.Join(rec.RunDir, report.FileName)
	if _, err := os.Lstat(path); err != nil {
		fmt.Fprintf(errw, "no report for %s; run gophermind brief run %s\n", id, id)
		return exitError
	}
	rep, err := report.Read(rec.RunDir)
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return exitError
	}
	if asJSON {
		raw, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(errw, "error: report.json cannot be read\n")
			return exitError
		}
		out.Write(raw)
		return exitDone
	}
	fmt.Fprint(out, rep.Summary())
	return exitDone
}

// runSink adds the executor's progress to printSink: ids, counts and fixed
// class words only, one line each, safe to read after a kill -9.
type runSink struct{ *printSink }

func (s *runSink) Emit(e events.Event) {
	var line string
	node, msg := safeLine(e.NodeID), safeLine(e.Message)
	with := func(prefix string) string {
		if msg == "" {
			return prefix
		}
		return prefix + " (" + msg + ")"
	}
	switch e.Kind {
	case "leaf_verified":
		line = node + ": verified"
	case "leaf_failed":
		line = with(node + ": failed")
	case "leaf_escalated":
		line = with(node + ": escalated")
	case "blocked":
		line = node + ": " + msg
	case "wave_check":
		line = fmt.Sprintf("wave %s checked: %s", strings.TrimPrefix(node, "wave-"), msg)
	case "warning":
		line = "warning: " + msg
	case "resume":
		line = "resumed: " + msg
	case "resume_blocked":
		line = "resume blocked: " + msg
	case "acceptance_passed":
		line = msg
	case "interrupted":
		line = with(node + ": interrupted")
	case "landing":
		line = "landing: " + msg
	default:
		s.printSink.Emit(e)
		return
	}
	s.printSink.mu.Lock()
	defer s.printSink.mu.Unlock()
	fmt.Fprintln(s.printSink.w, line)
}

// safeLine keeps the first line of s, printable ASCII only, at most 160 bytes.
func safeLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	b := make([]byte, 0, len(s))
	for _, r := range s {
		if r < 0x20 || r > 0x7e {
			r = '?'
		}
		b = append(b, byte(r))
		if len(b) == 160 {
			break
		}
	}
	return string(b)
}

// ---- status ----

// executorLines returns the lines `brief status` adds. It is nil until the
// executor has done something to the run: there is a report, a leaf has left
// pending, or the executor wrote its state file (the planner already creates
// the blackboard rows). It reads rows and files and takes no claim.
func executorLines(ctx context.Context, board blackboard.Blackboard, rec planner.RunRecord, rep *report.Report) ([]string, error) {
	rows, err := board.List(ctx, rec.RunID, blackboard.Filter{})
	if err != nil {
		return nil, err
	}
	started := rep != nil
	for _, r := range rows {
		if r.Status != blackboard.StatusPending {
			started = true
		}
	}
	if st, err := executor.LoadState(rec.RunDir); err == nil && st.StartedAt != "" {
		started = true
	}
	if !started {
		return nil, nil
	}
	tr, err := tree.NewStore(rec.RunDir).Load()
	if err != nil {
		return nil, err
	}
	waves, err := tr.ComputeWaves()
	if err != nil {
		return nil, err
	}
	// Leaves are the function nodes; components and the root have rows too.
	status := map[string]blackboard.Status{}
	counts := map[string]int{}
	for _, r := range rows {
		if n, ok := tr.Nodes[r.NodeID]; !ok || n.Kind != tree.KindFunction {
			continue
		}
		status[r.NodeID] = r.Status
		switch r.Status {
		case blackboard.StatusPending:
			counts["pending"]++
		case blackboard.StatusReady:
			counts["ready"]++
		case blackboard.StatusEscalated:
			counts["escalated"]++
		case blackboard.StatusFailed:
			counts["failed"]++
		case blackboard.StatusVerified:
			counts["verified"]++
		default:
			counts["in progress"]++
		}
	}
	var lines []string
	if rep != nil {
		lines = append(lines, "executor: "+rep.Status)
		if rep.StopReason != "" {
			lines = append(lines, "stop reason: "+rep.StopReason)
		}
	} else {
		lines = append(lines, "executor: in progress")
	}
	if len(status) == 0 {
		return lines, nil
	}
	total, done := map[int]int{}, map[int]int{}
	for id, st := range status {
		w := waves[id]
		total[w]++
		if st == blackboard.StatusVerified {
			done[w]++
		}
	}
	wavesDone := 0
	for w, n := range total {
		if done[w] == n {
			wavesDone++
		}
	}
	lines = append(lines, fmt.Sprintf("waves done: %d of %d", wavesDone, len(total)))
	leaf := fmt.Sprintf("leaves: verified %d of %d", counts["verified"], len(status))
	var rest []string
	for _, k := range []string{"pending", "ready", "in progress", "escalated", "failed"} {
		if counts[k] > 0 {
			rest = append(rest, fmt.Sprintf("%s %d", k, counts[k]))
		}
	}
	if len(rest) > 0 {
		leaf += "; " + strings.Join(rest, ", ")
	}
	lines = append(lines, leaf)

	// A pending leaf is blocked when a direct dependency failed, escalated or is blocked.
	ids := make([]string, 0, len(status))
	for id := range status {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	blocker := map[string]string{}
	var find func(id string, seen map[string]bool) string
	find = func(id string, seen map[string]bool) string {
		if b, ok := blocker[id]; ok {
			return b
		}
		if seen[id] || status[id] != blackboard.StatusPending {
			return ""
		}
		seen[id] = true
		deps := append([]string(nil), tr.Nodes[id].DependsOn...)
		sort.Strings(deps)
		for _, d := range deps {
			st, has := status[d]
			if !has {
				continue
			}
			if st == blackboard.StatusFailed || st == blackboard.StatusEscalated || find(d, seen) != "" {
				blocker[id] = d
				return d
			}
		}
		return ""
	}
	for _, id := range ids {
		if dep := find(id, map[string]bool{}); dep != "" {
			lines = append(lines, fmt.Sprintf("blocked: %s (waiting on %s)", id, dep))
		}
	}
	return lines, nil
}

// ---- base URLs ----

// probeTimeout bounds one reachability probe of a provider base URL.
var probeTimeout = 3 * time.Second

// probeResult is what resolveBaseURLs found for one provider of the strong tier.
type probeResult struct {
	Provider string
	Host     string // host name only of the base URL that answered
	Answered bool
	Fallback bool // a base_url_fallbacks entry answered, not base_url
}

// resolveBaseURLs probes every provider of the strong tier: base_url first,
// then base_url_fallbacks in order. The first that answers within
// probeTimeout is used. It returns a copy of cfg with those base URLs, so the
// override lasts for the process and the config file is never rewritten; a
// provider nothing answered for keeps its base_url.
func resolveBaseURLs(ctx context.Context, cfg *settings.Config, client *http.Client) (*settings.Config, []probeResult) {
	c := *cfg
	c.Providers = append([]settings.ProviderConfig(nil), cfg.Providers...)
	var res []probeResult
	seen := map[string]bool{}
	for _, entry := range cfg.Models["strong"] {
		name, _, ok := settings.SplitEntry(entry)
		if !ok || seen[name] {
			continue
		}
		seen[name] = true
		for i, p := range c.Providers {
			if p.Name != name {
				continue
			}
			r := probeResult{Provider: name}
			for j, base := range append([]string{p.BaseURL}, p.BaseURLFallbacks...) {
				if probeBaseURL(ctx, client, base) {
					r.Answered, r.Fallback = true, j > 0
					if u, err := url.Parse(base); err == nil {
						r.Host = u.Hostname()
					}
					c.Providers[i].BaseURL = base
					break
				}
			}
			res = append(res, r)
		}
	}
	return &c, res
}

// probeBaseURL says whether base answers: GET /api/version on its origin, then
// GET <base>/models, within probeTimeout in all. Any answer below 500 counts.
func probeBaseURL(ctx context.Context, client *http.Client, base string) bool {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	for _, target := range []string{u.Scheme + "://" + u.Host + "/api/version", strings.TrimRight(base, "/") + "/models"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return false
		}
		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return false
			}
			continue
		}
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		if resp.StatusCode < 500 {
			return true
		}
	}
	return false
}

// envNotes is what the report's environment section says about the providers:
// the host that answered, host only.
func envNotes(res []probeResult) []string {
	var notes []string
	for _, r := range res {
		if r.Answered {
			notes = append(notes, fmt.Sprintf("provider %s: base url host %s answered", r.Provider, safeLine(r.Host)))
		}
	}
	return notes
}
