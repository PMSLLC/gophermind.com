// Command gophermind is a minimal agentic coding harness. Run it with no
// arguments for an interactive terminal session, or `run`/`ask` for one-shot
// use. It drives an OpenAI-compatible model through a read/search/edit/shell
// tool loop against the current repository.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
	"gophermind/internal/agent"
	"gophermind/internal/config"
	"gophermind/internal/doctor"
	"gophermind/internal/jobs"
	"gophermind/internal/llm"
	"gophermind/internal/persona"
	"gophermind/internal/project"
	"gophermind/internal/safety"
	"gophermind/internal/session"
	"gophermind/internal/setup"
	"gophermind/internal/stream"
	"gophermind/internal/tools"
	"gophermind/internal/tui"
	"gophermind/internal/ui"
	"gophermind/internal/update"
	"gophermind/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	profileFlag := flag.String("profile", cfg.Profile, "provider profile to select (e.g. local-llama, openai, anthropic-proxy)")
	flag.StringVar(profileFlag, "p", cfg.Profile, "alias for -profile")
	baseFlag := flag.String("base", cfg.BaseURL, "OpenAI-compatible endpoint base URL")
	rootFlag := flag.String("root", cfg.RootDir, "repository root directory")
	modelFlag := flag.String("model", cfg.Model, "model name (default: auto-discover from the endpoint)")
	modeFlag := flag.String("mode", cfg.ApprovalMode, "approval mode for mutating tools: auto|ask")
	maxFlag := flag.Int("max", cfg.MaxIter, "maximum loop iterations per turn")
	insecureFlag := flag.Bool("insecure", cfg.InsecureTLS, "skip TLS verification (self-signed internal endpoints)")
	clientCertFlag := flag.String("client-cert", cfg.ClientCertPath, "PEM client certificate for mutual TLS (requires -client-key); secure alternative to -insecure")
	clientKeyFlag := flag.String("client-key", cfg.ClientKeyPath, "PEM client private key for mutual TLS (requires -client-cert)")
	caCertFlag := flag.String("ca-cert", cfg.CACertPath, "PEM CA bundle to trust for the server (appended to system roots; keeps verification ON)")
	verboseFlag := flag.Bool("v", false, "verbose: stream assistant text and tool results")
	personaFlag := flag.String("persona", "", "task-tuned system-prompt preset: reviewer|architect|tester")
	thinkFlag := flag.String("think", "", "reasoning effort sent with each request: low|medium|high (empty = off)")
	speedFlag := flag.Bool("speed", false, "use the faster model (GOPHERMIND_SPEED_MODEL, or the first fallback) as primary")
	fortuneDefault := "on"
	if v := strings.TrimSpace(os.Getenv("GOPHERMIND_FORTUNE")); v != "" {
		fortuneDefault = v
	}
	fortuneFlag := flag.String("fortune", fortuneDefault, "startup fortune: on|off")
	noBannerFlag := flag.Bool("no-banner", false, "suppress the startup banner/splash (clean output in scripts/CI)")
	quietFlag := flag.Bool("quiet", false, "quiet: suppress the banner and non-essential stderr chatter")
	flag.BoolVar(quietFlag, "q", false, "alias for -quiet")
	versionFlag := flag.Bool("version", false, "print version and exit")
	printFlag := flag.Bool("print", false, "non-interactive print mode for external drivers (Claude-Code-compatible stream-json)")
	inputFmtFlag := flag.String("input-format", "text", "print mode input format: text|stream-json")
	outputFmtFlag := flag.String("output-format", "text", "print mode output format: text|stream-json")
	sessionIDFlag := flag.String("session-id", "", "print mode: pre-assign a session id (persisted for resume)")
	resumeFlag := flag.String("resume", "", "print mode: resume a saved session by id")
	appendSysFlag := flag.String("append-system-prompt", "", "print mode: append text to the system prompt")
	permModeFlag := flag.String("permission-mode", "", "print mode: auto (full access) | plan (read-only: denies edits/shell)")
	readOnlyFlag := flag.Bool("read-only", false, "deny all mutating tools (write/edit/shell/move/delete/mkdir/patch) in every mode")
	planFlag := flag.Bool("plan", false, "run/ask: emit a structured plan before executing")
	parallelFlag := flag.Bool("parallel", false, "run/ask: execute independent tool calls in a turn concurrently")
	verifyFlag := flag.Bool("verify", false, "run/ask: have a second (verifier) agent check the result and trigger one correction round if incomplete")
	schemaFlag := flag.String("schema", "", "run/ask: force a JSON-schema-constrained response, reading the schema from this file")
	toolBudgetFlag := flag.Int("tool-budget", 0, "run/ask: max tool calls per turn (0 = default)")
	maxCostFlag := flag.Float64("max-cost", 0, "run/ask: abort when estimated cost (USD) exceeds this (0 = unlimited)")
	maxTokensFlag := flag.Int("max-tokens", 0, "run/ask: abort when total tokens exceed this (0 = unlimited)")
	maxDurationFlag := flag.Duration("max-duration", 0, "run/ask: abort when wall-clock exceeds this (e.g. 2m; 0 = unlimited)")
	transcriptFlag := flag.String("transcript", cfg.TranscriptPath, "write the full wire-level message history (JSONL) to this path at session end; MAY CONTAIN SENSITIVE PROMPTS/RESPONSES (file written 0600, no credentials included)")
	flag.Usage = usage
	flag.Parse()

	// Which flags the user set explicitly — these take precedence over a
	// profile's resolved values.
	set := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { set[f.Name] = true })

	// `--version` (and the `version` subcommand) print build metadata and exit 0.
	// The flag form keeps conformance probes (e.g. OpenCoven's `conjure test`,
	// which calls `--version`) happy.
	if *versionFlag {
		fmt.Println(version.String())
		return nil
	}

	// Select and resolve the profile first (flag > env). When a profile is
	// active it fills BaseURL/Model/APIKey/HTTPTimeout from per-profile env >
	// built-in defaults; an unknown name is a hard error.
	cfg.Profile = *profileFlag
	cfg.RootDir = *rootFlag
	cfg.ApprovalMode = *modeFlag
	cfg.MaxIter = *maxFlag
	cfg.InsecureTLS = *insecureFlag
	cfg.ClientCertPath = *clientCertFlag
	cfg.ClientKeyPath = *clientKeyFlag
	cfg.CACertPath = *caCertFlag
	cfg.TranscriptPath = *transcriptFlag
	cfg, err = cfg.ApplyProfile()
	if err != nil {
		return err
	}

	// Explicit endpoint flags override the profile's resolved values.
	if set["base"] {
		cfg.BaseURL = *baseFlag
	}
	if set["model"] {
		cfg.Model = *modelFlag
	}

	args := flag.Args()
	cmd := "chat"
	task := ""
	switch {
	case *printFlag:
		// In print mode the whole positional tail is the prompt, not a subcommand,
		// so it bypasses the chat/config/version/wizard paths entirely.
		cmd = "print"
		task = strings.TrimSpace(strings.Join(args, " "))
	case len(args) > 0:
		cmd = strings.ToLower(args[0])
		task = strings.TrimSpace(strings.Join(args[1:], " "))
	}

	// `gophermind version` prints build metadata and exits (needs no config).
	if cmd == "version" {
		fmt.Println(version.String())
		return nil
	}

	// `gophermind sessions [list|show <id>|rm <id>]` manages the saved session
	// store and exits (needs no endpoint).
	if cmd == "sessions" {
		return runSessions(args[1:])
	}

	// `gophermind status` prints a compact prompt line (model + branch) for
	// PS1/starship integration and exits.
	if cmd == "status" {
		fmt.Println(promptLine(cfg.Model, gitBranchOf(cfg.RootDir)))
		return nil
	}

	// `gophermind audit verify <file>` checks a tamper-evident audit log's chain.
	if cmd == "audit" {
		if len(args) < 3 || strings.ToLower(args[1]) != "verify" {
			return fmt.Errorf("usage: gophermind audit verify <file>")
		}
		if err := safety.VerifyAuditFile(args[2]); err != nil {
			return fmt.Errorf("audit verification FAILED: %w", err)
		}
		fmt.Fprintln(os.Stderr, "✓ audit log intact")
		return nil
	}

	// `gophermind config` always (re-)runs the setup wizard, pre-filled with the
	// current values, then saves and exits.
	if cmd == "config" {
		res, err := runSetupWizard(cfg)
		if err != nil {
			return err
		}
		p, err := config.ConfigFilePath()
		if err != nil {
			return err
		}
		if err := setup.WriteEnv(p, res.Pairs()); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "✓ saved to %s\n", p)
		return nil
	}

	// First-run setup: only for an interactive chat session with nothing yet
	// configured (no saved config, and no base URL from env/flag/profile). This
	// never fires for `run`/`ask`, piped input, or an already-configured user.
	_, baseEnvSet := os.LookupEnv("GOPHERMIND_BASE_URL")
	baseProvided := baseEnvSet || set["base"] || cfg.Profile != ""
	if cmd == "chat" && setup.NeedsSetup(baseProvided, config.GlobalConfigExists(), isatty()) {
		fmt.Fprintln(os.Stderr, "No config found — let's set you up. (re-run anytime with `gophermind config`)")
		res, err := runSetupWizard(cfg)
		if err != nil {
			return err
		}
		if p, perr := config.ConfigFilePath(); perr != nil {
			fmt.Fprintln(os.Stderr, "warning: could not resolve config path:", perr)
		} else if werr := setup.WriteEnv(p, res.Pairs()); werr != nil {
			fmt.Fprintln(os.Stderr, "warning: could not save config:", werr)
		} else {
			fmt.Fprintf(os.Stderr, "✓ saved to %s\n", p)
		}
		// Apply the just-captured values to this session (the file is for next time).
		cfg.BaseURL = res.BaseURL
		if res.APIKey != "" {
			cfg.APIKey = res.APIKey
		}
		cfg.Model = res.Model
		cfg.ApprovalMode = res.ApprovalMode
		if res.MaxIter > 0 {
			cfg.MaxIter = res.MaxIter
		}
	}

	// `gophermind doctor` runs environment/config diagnostics and exits. It must
	// work even when the endpoint is unset or unreachable (that's what it checks),
	// so it runs before Validate and before the client is built.
	if cmd == "doctor" {
		results := doctor.Checks(doctor.Params{BaseURL: cfg.BaseURL, Model: cfg.Model, Root: cfg.RootDir})
		if !doctor.Report(os.Stdout, results) {
			return fmt.Errorf("doctor: some checks failed")
		}
		return nil
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	// NewWithTLS fails fast on bad cert/key/CA material so a misconfigured secure
	// deployment errors at startup rather than mid-request. The zero TLSOptions
	// (no cert/key/CA, insecure=false) reproduces the prior default transport.
	client, err := llm.NewWithTLS(cfg.BaseURL, cfg.APIKey, cfg.Model, cfg.HTTPTimeout, llm.TLSOptions{
		InsecureSkipVerify: cfg.InsecureTLS,
		ClientCertPath:     cfg.ClientCertPath,
		ClientKeyPath:      cfg.ClientKeyPath,
		CACertPath:         cfg.CACertPath,
	})
	if err != nil {
		return fmt.Errorf("TLS setup: %w", err)
	}
	client.Fallbacks = cfg.FallbackModels
	client.SetTemperature(cfg.Temperature)
	client.SetTopP(cfg.TopP)

	// --think: send a reasoning-effort hint with each request.
	if *thinkFlag != "" {
		switch *thinkFlag {
		case "low", "medium", "high":
			client.SetReasoningEffort(*thinkFlag)
		default:
			return fmt.Errorf("invalid -think %q: use low, medium, or high", *thinkFlag)
		}
	}

	// --speed: swap the primary model for a faster one (explicit GOPHERMIND_SPEED_MODEL,
	// else the first configured fallback). With neither available it is a no-op warning.
	if *speedFlag {
		speed := cfg.SpeedModel
		if speed == "" && len(cfg.FallbackModels) > 0 {
			speed = cfg.FallbackModels[0]
		}
		if speed == "" {
			fmt.Fprintln(os.Stderr, "warning: -speed set but no speed model configured (set GOPHERMIND_SPEED_MODEL or a fallback); ignoring")
		} else {
			cfg.Model = speed
			client.Model = speed
		}
	}
	client.Retry = llm.RetryPolicy{
		MaxAttempts: cfg.MaxAttempts,
		BaseDelay:   cfg.RetryBaseDelay,
		MaxDelay:    llm.DefaultRetryPolicy.MaxDelay,
	}
	if cfg.CacheEnabled {
		client.Cache = &llm.Cache{Dir: cfg.CacheDir, TTL: cfg.CacheTTL}
	}

	// Resolve the model. With none configured, auto-discover the first the
	// endpoint serves. With one configured, validate it against /v1/models so a
	// typo fails fast at startup — dumping the models the endpoint actually
	// offers — instead of surfacing as a cryptic error mid-request. The check is
	// best-effort: if /v1/models errors or lists nothing we cannot validate, so
	// we proceed and let the completion request surface any real error.
	if cfg.Model == "" {
		discovered, err := client.DiscoverModel(context.Background())
		if err != nil {
			return fmt.Errorf("no model set and discovery failed: %w (set -model or GOPHERMIND_MODEL)", err)
		}
		cfg.Model = discovered
		client.Model = discovered
	} else if models, err := client.ListModels(context.Background()); err == nil && len(models) > 0 && !slices.Contains(models, cfg.Model) {
		return fmt.Errorf("model %q not found at %s\nmodels available at this endpoint:\n  %s\n(set -model or GOPHERMIND_MODEL to one of these)",
			cfg.Model, cfg.BaseURL, strings.Join(models, "\n  "))
	}

	// Probe the model's capabilities (context window, max output, tool support)
	// so they are available to adapt truncation/iteration limits. This never
	// fails: it degrades to a built-in table and conservative defaults, and the
	// result is cached per endpoint+model on the client.
	caps := client.ProbeCapabilities(context.Background())
	if !*quietFlag {
		fmt.Fprintf(os.Stderr, "model capabilities: %s\n", caps)
	}

	// Shared session-wide budget for the network tools (0/0 = unlimited).
	netBudget := tools.NewNetBudget(cfg.NetMaxRequests, cfg.NetMaxBytes)

	reg := tools.NewRegistry(
		tools.ReadFileRange(cfg.RootDir),  // read_file + optional line ranges
		tools.ListFilesGlob(cfg.RootDir),  // list_files + include/exclude globs
		tools.SearchEnhanced(cfg.RootDir), // search + context/flags/paging
		tools.WriteFile(cfg.RootDir),
		tools.EditFileMulti(cfg.RootDir), // edit_file + replace_all
		tools.RunShellEnhanced(cfg.RootDir, cfg.CmdTimeout, tools.ShellLimits{
			CPUSeconds:  cfg.ShellCPUSeconds,
			MaxMemoryMB: cfg.ShellMaxMemMB,
			MaxProcs:    cfg.ShellMaxProcs,
		}), // run_shell + timeout/workdir/resource-limits
		tools.FileStat(cfg.RootDir),
		tools.MoveFile(cfg.RootDir),
		tools.DeleteFile(cfg.RootDir),
		tools.Mkdir(cfg.RootDir),
		tools.PatchApply(cfg.RootDir),
		tools.FetchURL(cfg.FetchAllowHosts, netBudget),    // gated, egress-controlled URL fetch
		tools.HTTPRequest(cfg.FetchAllowHosts, netBudget), // gated HTTP API caller (methods/headers/body)
		tools.FindSymbol(cfg.RootDir),                     // definition-aware symbol search
		tools.GitInfo(cfg.RootDir),                        // read-only structured git (log/status/diff)
		tools.InspectData(cfg.RootDir),                    // read-only CSV/JSON schema + preview
		tools.AnalyzeLog(cfg.RootDir),                     // read-only log severity summary
		tools.CreateMigration(cfg.RootDir),                // gated: scaffold a timestamped SQL migration
	)

	// A single shared stdin reader, used by both the REPL and approval prompts.
	stdin := bufio.NewReader(os.Stdin)
	approve := safety.ApprovalFunc(safety.Auto)
	if cfg.ApprovalMode == "ask" {
		approve = func(tool, argsJSON string) bool { return ui.Confirm(stdin, tool, argsJSON) }
	}
	if *readOnlyFlag {
		approve = safety.ReadMode() // deny every gated (mutating) tool
	} else if pol := loadRepoPolicy(cfg.RootDir); pol != nil {
		// A .gophermind/policy file layers per-tool approval on top of the base
		// decision: always/never resolve without prompting, ask defers to it.
		approve = safety.PolicyApproval(pol, approve)
	}
	// Opt-in judge model (GOPHERMIND_JUDGE): route gated approvals to a small
	// model against a spec; a judge outage defers to the base decision above.
	if !*readOnlyFlag && envTruthy("GOPHERMIND_JUDGE") {
		approve = safety.JudgeApproval(newJudge(client), approve)
	}

	// Resolve an optional persona preset and compose it with repo instructions
	// (CLAUDE.md/AGENTS.md) into the system-prompt suffix used by chat and run/ask.
	personaText := ""
	if *personaFlag != "" {
		p, ok := persona.Preset(*personaFlag)
		if !ok {
			return fmt.Errorf("unknown -persona %q: choose one of %s", *personaFlag, strings.Join(persona.Names(), ", "))
		}
		personaText = p
	}
	// Opt-in dynamic context (GOPHERMIND_REPO_CONTEXT): inject a compact repo
	// orientation (git branch/status + top-level tree) so the model orients
	// without spending tool calls.
	repoContext := ""
	if envTruthy("GOPHERMIND_REPO_CONTEXT") {
		repoContext = project.RepoContext(cfg.RootDir)
	}
	systemSuffix := composeSystem(personaText, project.Instructions(cfg.RootDir), project.Skills(cfg.RootDir), repoContext)
	// Prompt token-budget guardrail: keep injected context (persona + repo
	// instructions + repo map) under ~25% of the model's context window so the
	// task itself always has room. Skipped when the window is unknown.
	if caps.ContextWindow > 0 {
		systemSuffix = project.CapContext(systemSuffix, caps.ContextWindow/4)
	}
	// Prompt linting: surface overly long or self-contradicting instructions so
	// the user can fix them (advisory only; suppressed by --quiet).
	if !*quietFlag {
		for _, w := range project.LintInstructions(systemSuffix) {
			fmt.Fprintln(os.Stderr, "prompt lint:", w)
		}
	}

	switch cmd {
	case "chat":
		if !isatty() {
			return fmt.Errorf("interactive session needs a terminal; use `run`/`ask` for non-interactive use")
		}
		// Opt-in update notice (GOPHERMIND_UPDATE_CHECK): nudge upgrades without
		// ever blocking startup — a failed/slow check is silent.
		if updateCheckEnabled() && !*quietFlag {
			if notice, ok := update.Check(version.Version, func() (string, error) {
				return update.LatestFromGitHub("jbrahy/gophermind.com")
			}); ok {
				fmt.Fprintln(os.Stderr, notice)
			}
		}
		return tui.Run(tui.Config{
			Client:           client,
			Registry:         reg,
			Model:            cfg.Model,
			Mode:             cfg.ApprovalMode,
			MaxIter:          cfg.MaxIter,
			InputPricePer1K:  cfg.InputPricePer1K,
			OutputPricePer1K: cfg.OutputPricePer1K,
			TranscriptPath:   cfg.TranscriptPath,
			SystemSuffix:     systemSuffix,
			ReadOnly:         *readOnlyFlag,
			NoBanner:         *noBannerFlag || *quietFlag,
			NoFortune:        strings.EqualFold(*fortuneFlag, "off"),
			RedactTranscript: redactTranscriptEnabled(),
			AuditPath:        strings.TrimSpace(os.Getenv("GOPHERMIND_AUDIT_LOG")),
		})
	case "print":
		return runPrint(client, reg, cfg, printOptions{
			prompt: task, inputFmt: *inputFmtFlag, outputFmt: *outputFmtFlag,
			sessionID: *sessionIDFlag, resumeID: *resumeFlag,
			appendSys: *appendSysFlag, permMode: *permModeFlag, readOnly: *readOnlyFlag,
		})
	case "serve":
		// Webhook mode: each POST /run spawns a fresh agent turn, isolated from
		// other requests. Blocks until the process is stopped.
		run := func(ctx context.Context, t string) (string, error) {
			ag := agent.New(client, reg, cfg.MaxIter, approve, nil)
			ag.SetPrices(cfg.InputPricePer1K, cfg.OutputPricePer1K)
			ag.SetAuditLog(auditLog())
			if systemSuffix != "" {
				ag.AppendSystemPrompt(systemSuffix)
			}
			return ag.Send(ctx, t)
		}
		return runServe(run)
	case "queue":
		if task == "" {
			return fmt.Errorf("queue requires a file of tasks (one per line)")
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		return runQueue(ctx, client, reg, cfg, approve, systemSuffix, task, *verboseFlag)
	case "run", "ask":
		if task == "" {
			return fmt.Errorf("%s requires a task argument", cmd)
		}
		if cmd == "ask" {
			task = "Answer the following question about this repository without modifying any files:\n" + task
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		printer := ui.Printer{Verbose: *verboseFlag}
		ag := agent.New(client, reg, cfg.MaxIter, approve, printer.Event)
		ag.SetPrices(cfg.InputPricePer1K, cfg.OutputPricePer1K)
		ag.SetRedactTranscript(redactTranscriptEnabled())
		ag.SetAuditLog(auditLog())
		if systemSuffix != "" {
			ag.AppendSystemPrompt(systemSuffix)
		}
		// Cost/time guardrails apply to the default Send strategy.
		if *maxCostFlag > 0 || *maxTokensFlag > 0 || *maxDurationFlag > 0 {
			ag = ag.WithGuardrails(agent.Guardrails{
				MaxCostUSD:  *maxCostFlag,
				MaxTokens:   *maxTokensFlag,
				MaxDuration: *maxDurationFlag,
			})
		}
		// --schema forces a single schema-constrained JSON response (bypasses the
		// tool loop and other strategies).
		if *schemaFlag != "" {
			schema, err := loadJSONSchema(*schemaFlag)
			if err != nil {
				return err
			}
			out, sErr := ag.StructuredOutput(ctx, task, schema)
			if sErr != nil {
				return sErr
			}
			fmt.Println(out)
			return nil
		}
		// Pick the turn strategy from flags (all share Send's signature).
		send := ag.Send
		switch {
		case *planFlag:
			send = ag.PlanThenExecute
		case *parallelFlag:
			send = ag.DispatchParallel
		case *toolBudgetFlag > 0:
			ag = ag.WithToolCallBudget(*toolBudgetFlag)
			send = ag.SendWithBudget
		}
		// --verify wraps the chosen strategy with a second-agent verification pass.
		if *verifyFlag {
			inner := send
			send = func(ctx context.Context, task string) (string, error) {
				return ag.VerifyResult(ctx, task, inner)
			}
		}
		answer, sendErr := send(ctx, task)
		// Write the transcript even when the turn errored: a partial history is
		// still useful for debugging. The dump never includes credentials.
		if cfg.TranscriptPath != "" {
			if err := ag.WriteTranscript(cfg.TranscriptPath); err != nil {
				fmt.Fprintln(os.Stderr, "warning: transcript export failed:", err)
			}
		}
		// --output-format json: emit one machine-readable result object (errors
		// included) so run/ask is scriptable. Default text mode prints the answer
		// to stdout and the usage meter to stderr.
		switch *outputFmtFlag {
		case "json":
			return renderJSONResult(os.Stdout, answer, ag.Usage(), cfg.Model, sendErr)
		case "text", "":
			if sendErr != nil {
				return sendErr
			}
			fmt.Println(answer)
			// Token + cost meter goes to stderr so stdout stays pipeable.
			fmt.Fprintln(os.Stderr, ag.Usage().String())
			return nil
		default:
			return fmt.Errorf("invalid -output-format %q for %s: use text or json", *outputFmtFlag, cmd)
		}
	default:
		usage()
		return fmt.Errorf("unknown command: %s", cmd)
	}
}

// printOptions carries the resolved flags for print mode.
type printOptions struct {
	prompt    string
	inputFmt  string
	outputFmt string
	sessionID string
	resumeID  string
	appendSys string
	permMode  string
	readOnly  bool
}

func runPrint(client *llm.Client, reg *tools.Registry, cfg config.Config, o printOptions) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Permission mode maps the driver's access level to gophermind's approval
	// gate: "plan"/read-only denies the mutating (gated) tools so the model can
	// explore and propose but not edit or run shell; the default auto-approves
	// (print mode is programmatic, so there's no human to prompt).
	approve := safety.ApprovalFunc(safety.Auto)
	switch o.permMode {
	case "plan", "read_only", "read-only":
		approve = safety.ReadMode()
	}
	if o.readOnly {
		approve = safety.ReadMode()
	}

	sessionID, resumeID := o.sessionID, o.resumeID
	// Resolve the session id and whether it should be persisted: --resume loads
	// and continues an existing session; --session-id pre-assigns one; otherwise
	// the session is ephemeral (no disk footprint).
	persist := false
	if resumeID != "" {
		resumeID = session.Resolve(resumeID) // a friendly alias -> its session id
		sessionID, persist = resumeID, true
	} else if sessionID != "" {
		persist = true
	}
	if sessionID == "" {
		sessionID = stream.NewSessionID()
	}

	streamOut := o.outputFmt == "stream-json"
	var enc *stream.Encoder
	onEvent := func(agent.Event) {}
	if streamOut {
		enc = stream.NewEncoder(os.Stdout, sessionID)
		onEvent = func(e agent.Event) { _ = enc.Handle(e) }
	}

	ag := agent.New(client, reg, cfg.MaxIter, approve, onEvent)
	ag.SetPrices(cfg.InputPricePer1K, cfg.OutputPricePer1K)
	ag.SetAuditLog(auditLog())

	if resumeID != "" {
		if err := session.Load(resumeID, ag); err != nil {
			return fmt.Errorf("resume %q: %w", resumeID, err)
		}
	} else {
		// Fresh session only; on resume the loaded history already carries its
		// system prompt (re-appending would stack).
		if instr := project.Instructions(cfg.RootDir); instr != "" {
			ag.AppendSystemPrompt(instr)
		}
		if o.appendSys != "" {
			ag.AppendSystemPrompt(o.appendSys)
		}
	}
	save := func() error {
		if !persist {
			return nil
		}
		return session.Save(sessionID, ag)
	}

	if streamOut {
		var toolNames []string
		for _, d := range reg.Definitions() {
			toolNames = append(toolNames, d.Function.Name)
		}
		afterTurn := (func() error)(nil)
		if persist {
			afterTurn = save
		}
		return stream.Run(ctx, enc, ag, stream.Options{
			In:          os.Stdin,
			InputFormat: o.inputFmt,
			Prompt:      o.prompt,
			Model:       cfg.Model,
			Tools:       toolNames,
			Cwd:         cfg.RootDir,
			AfterTurn:   afterTurn,
		})
	}

	// text output: behave like `run`, printing only the final answer.
	answer, err := ag.Send(ctx, o.prompt)
	if serr := save(); serr != nil && err == nil {
		err = serr
	}
	if err != nil {
		return err
	}
	fmt.Println(answer)
	return nil
}

// isatty reports whether stdin is an interactive terminal.
func isatty() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && (fi.Mode()&os.ModeCharDevice) != 0
}

// runSetupWizard drives the first-run/`config` setup wizard against stdin/stderr.
// Model discovery uses a short-lived client honoring the current TLS settings, so
// the picker reflects what the chosen endpoint actually serves. On an interactive
// terminal the API key is read without echo; otherwise (piped input) it is read
// as a normal line through the wizard's own reader.
func runSetupWizard(cfg config.Config) (setup.Result, error) {
	opts := setup.Options{
		In:       os.Stdin,
		Out:      os.Stderr,
		Profiles: config.BuiltinProfileNames(),
		ListModels: func(baseURL, apiKey string) ([]string, error) {
			c, err := llm.NewWithTLS(baseURL, apiKey, "", 15*time.Second, llm.TLSOptions{
				InsecureSkipVerify: cfg.InsecureTLS,
				ClientCertPath:     cfg.ClientCertPath,
				ClientKeyPath:      cfg.ClientKeyPath,
				CACertPath:         cfg.CACertPath,
			})
			if err != nil {
				return nil, err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			return c.ListModels(ctx)
		},
		Defaults: setup.Result{BaseURL: cfg.BaseURL, Model: cfg.Model, ApprovalMode: cfg.ApprovalMode, MaxIter: cfg.MaxIter},
	}
	if isatty() {
		opts.ReadSecret = func() (string, error) {
			b, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr) // ReadPassword swallows the echoed newline
			return strings.TrimSpace(string(b)), err
		}
	}
	return setup.Run(opts)
}

// runQueue implements the `queue` subcommand: it reads a file of tasks (one per
// line; blank lines and #comments ignored), runs each through the agent in
// order with a live status trail, prints a per-job summary, and exits non-zero
// if any task failed.
func runQueue(ctx context.Context, client *llm.Client, reg *tools.Registry, cfg config.Config, approve safety.ApprovalFunc, systemSuffix, file string, verbose bool) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("read task file: %w", err)
	}
	q := jobs.New()
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		q.Add(line)
	}
	total := len(q.Jobs())
	if total == 0 {
		return fmt.Errorf("no tasks found in %s", file)
	}

	printer := ui.Printer{Verbose: verbose}
	ag := agent.New(client, reg, cfg.MaxIter, approve, printer.Event)
	ag.SetPrices(cfg.InputPricePer1K, cfg.OutputPricePer1K)
	ag.SetAuditLog(auditLog())
	if systemSuffix != "" {
		ag.AppendSystemPrompt(systemSuffix)
	}

	q.Run(ctx, ag.Send, func(j *jobs.Job) {
		if j.Status == jobs.Running {
			fmt.Fprintf(os.Stderr, "▶ [%d/%d] %s\n", j.ID, total, j.Task)
		}
	})

	// Per-job summary to stdout (pipeable), counts to stderr.
	for _, j := range q.Jobs() {
		switch j.Status {
		case jobs.Done:
			fmt.Printf("✓ #%d %s\n", j.ID, j.Task)
		case jobs.Failed:
			fmt.Printf("✗ #%d %s\n   error: %s\n", j.ID, j.Task, j.Err)
		default:
			fmt.Printf("· #%d %s (not run)\n", j.ID, j.Task)
		}
	}
	done, failed, pending := q.Counts()
	fmt.Fprintf(os.Stderr, "\n%d done, %d failed, %d not run\n", done, failed, pending)
	if failed > 0 {
		return fmt.Errorf("%d task(s) failed", failed)
	}
	return nil
}

// runSessions implements the `sessions` subcommand: list (default), show <id>,
// and rm <id>, operating on the persisted session store.
func runSessions(args []string) error {
	action := "list"
	if len(args) > 0 {
		action = strings.ToLower(args[0])
	}
	switch action {
	case "list", "ls":
		infos, err := session.List()
		if err != nil {
			return err
		}
		if len(infos) == 0 {
			fmt.Fprintln(os.Stderr, "no saved sessions")
			return nil
		}
		for _, s := range infos {
			fmt.Printf("%-24s  %s  %3d msgs  %s\n",
				s.ID, s.ModTime.Format("2006-01-02 15:04"), s.Messages, s.Title)
		}
		return nil
	case "diff":
		if len(args) < 2 {
			return fmt.Errorf("usage: sessions diff <id>")
		}
		out, err := session.Diff(session.Resolve(args[1]))
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	case "branch", "fork":
		if len(args) < 3 {
			return fmt.Errorf("usage: sessions branch <src-id> <new-id> [turn]")
		}
		atTurn := 0
		if len(args) >= 4 {
			n, err := strconv.Atoi(args[3])
			if err != nil || n < 0 {
				return fmt.Errorf("sessions branch: turn must be a non-negative integer, got %q", args[3])
			}
			atTurn = n
		}
		if err := session.Branch(session.Resolve(args[1]), args[2], atTurn); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "✓ branched %q → %q\n", args[1], args[2])
		return nil
	case "alias":
		if len(args) < 3 {
			return fmt.Errorf("usage: sessions alias <name> <id>")
		}
		if err := session.SetAlias(args[1], args[2]); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "✓ alias %q → session %q\n", args[1], args[2])
		return nil
	case "show", "cat":
		if len(args) < 2 {
			return fmt.Errorf("sessions show requires a session id")
		}
		p, err := session.Path(session.Resolve(args[1]))
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("session %q not found", args[1])
		}
		os.Stdout.Write(data)
		return nil
	case "rm", "remove", "delete":
		if len(args) < 2 {
			return fmt.Errorf("sessions rm requires a session id")
		}
		if err := session.Remove(session.Resolve(args[1])); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "✓ removed session %q\n", args[1])
		return nil
	case "gc":
		days := 30
		if len(args) >= 2 {
			n, err := strconv.Atoi(args[1])
			if err != nil || n <= 0 {
				return fmt.Errorf("sessions gc: days must be a positive integer, got %q", args[1])
			}
			days = n
		}
		removed, err := session.GC(time.Duration(days) * 24 * time.Hour)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "✓ removed %d session(s) older than %d days\n", len(removed), days)
		return nil
	case "export":
		if len(args) < 3 {
			return fmt.Errorf("usage: sessions export <id> <file>")
		}
		if err := session.Export(args[1], args[2]); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "✓ exported session %q to %s\n", args[1], args[2])
		return nil
	case "import":
		if len(args) < 3 {
			return fmt.Errorf("usage: sessions import <file> <id>")
		}
		if err := session.Import(args[1], args[2]); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "✓ imported %s as session %q\n", args[1], args[2])
		return nil
	default:
		return fmt.Errorf("unknown sessions action %q (use list, show <id>, rm <id>, gc [days], export <id> <file>, import <file> <id>, alias <name> <id>, branch <src> <new> [turn], diff <id>)", action)
	}
}

// composeSystem joins a persona preset and repo instructions into one system
// suffix, dropping empties so either may be absent.
func composeSystem(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "\n\n")
}

// updateCheckEnabled reports whether the opt-in startup update check is on,
// via a truthy GOPHERMIND_UPDATE_CHECK value.
func updateCheckEnabled() bool {
	return envTruthy("GOPHERMIND_UPDATE_CHECK")
}

// redactTranscriptEnabled reports whether transcripts/sessions should have
// secrets and PII scrubbed on write (GOPHERMIND_REDACT_TRANSCRIPT).
func redactTranscriptEnabled() bool {
	return envTruthy("GOPHERMIND_REDACT_TRANSCRIPT")
}

// auditLog returns a tamper-evident audit log when GOPHERMIND_AUDIT_LOG names a
// path, or nil (auditing disabled) otherwise. SetAuditLog(nil) is safe.
func auditLog() *safety.AuditLog {
	if path := strings.TrimSpace(os.Getenv("GOPHERMIND_AUDIT_LOG")); path != "" {
		return safety.NewAuditLog(path)
	}
	return nil
}

// newJudge builds a JudgeFunc that asks the model whether a gated tool call is
// allowed, against a spec (GOPHERMIND_JUDGE_SPEC or a safe default). It forces a
// structured judge_verdict tool call and parses the decision. The verdict tool
// choice is set only for this call and restored afterward.
func newJudge(client *llm.Client) safety.JudgeFunc {
	spec := strings.TrimSpace(os.Getenv("GOPHERMIND_JUDGE_SPEC"))
	if spec == "" {
		spec = "Allow safe, task-relevant actions. Deny anything destructive, anything that exfiltrates secrets, or anything acting outside the repository."
	}
	verdictTool := llm.Tool{
		Type: "function",
		Function: llm.Function{
			Name:        "judge_verdict",
			Description: "Decide whether the proposed tool call should be allowed.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"approve": map[string]any{"type": "boolean", "description": "True to allow the tool call."},
					"reason":  map[string]any{"type": "string", "description": "Brief justification."},
				},
				"required": []string{"approve"},
			},
		},
	}
	return func(tool, argsJSON string) (bool, string, error) {
		msgs := []llm.Message{
			{Role: "system", Content: "You are a security gate for an autonomous coding agent. " + spec + " Respond ONLY by calling judge_verdict."},
			{Role: "user", Content: fmt.Sprintf("Proposed tool call:\ntool: %s\narguments: %s\n\nAllow it?", tool, argsJSON)},
		}
		client.SetToolChoice(&llm.ToolChoiceConfig{Forced: &llm.ToolChoiceForced{Name: "judge_verdict"}})
		defer client.SetToolChoice(nil)
		reply, _, err := client.Complete(context.Background(), msgs, []llm.Tool{verdictTool})
		if err != nil {
			return false, "", err
		}
		for _, tc := range reply.ToolCalls {
			if tc.Function.Name != "judge_verdict" {
				continue
			}
			var v struct {
				Approve bool   `json:"approve"`
				Reason  string `json:"reason"`
			}
			if json.Unmarshal([]byte(tc.Function.Arguments), &v) != nil {
				return false, "", fmt.Errorf("judge verdict parse failed")
			}
			return v.Approve, v.Reason, nil
		}
		return false, "", fmt.Errorf("judge produced no verdict")
	}
}

// loadJSONSchema reads and parses a JSON-schema object from a file, for
// --schema-constrained responses.
func loadJSONSchema(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read schema: %w", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("parse schema %s: %w", path, err)
	}
	return schema, nil
}

// loadRepoPolicy loads .gophermind/policy from the repo root if present. A
// missing file returns nil (no policy); a malformed file warns and returns nil
// so a typo never silently changes trust boundaries.
func loadRepoPolicy(root string) *safety.Policy {
	path := filepath.Join(root, ".gophermind", "policy")
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	pol, err := safety.LoadPolicy(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: ignoring .gophermind/policy:", err)
		return nil
	}
	return pol
}

// envTruthy reports whether the named env var holds a truthy value.
func envTruthy(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `gophermind - a minimal agentic coding harness

Usage:
  gophermind                    interactive session (default)
  gophermind config             (re-)run the setup wizard and save config
  gophermind sessions [list|show <id>|rm <id>|gc [days]|export <id> <file>|import <file> <id>]
  gophermind doctor             run environment/config diagnostics and exit
  gophermind status             print a compact prompt line (model + branch)
  gophermind audit verify <file>  verify a tamper-evident audit log's chain
  gophermind version            print build version and exit
  gophermind run "task"         one-shot: run a task and exit
  gophermind ask "question"     one-shot: answer without modifying files
  gophermind queue <file>       run a file of tasks (one per line) in order
  gophermind serve              webhook server: POST /run {task} runs one task

On first interactive launch with nothing configured, a short setup wizard runs
and saves your choices to the global config (see below); later launches skip it.

Selected flags:
  -think low|medium|high  send a reasoning-effort hint with each request
  -speed                  use a faster model as primary (GOPHERMIND_SPEED_MODEL or first fallback)
  -no-banner, -quiet/-q   suppress the startup splash (and, with -quiet, stderr chatter)

Environment (all optional; flags override):
  GOPHERMIND_BASE_URL   endpoint (default: built-in)
  GOPHERMIND_MODEL      model name (default: auto-discovered)
  GOPHERMIND_API_KEY    bearer token (omit when reached over VPN)
  GOPHERMIND_APPROVAL   auto|ask (default: ask)
  GOPHERMIND_CLIENT_CERT  PEM client cert for mutual TLS (with _CLIENT_KEY; also -client-cert)
  GOPHERMIND_CLIENT_KEY   PEM client key for mutual TLS (with _CLIENT_CERT; also -client-key)
  GOPHERMIND_CA_CERT      PEM CA to trust for the server, appended to system roots,
                          verification stays ON — the secure alternative to -insecure
                          (also -ca-cert). Precedence: with -insecure, verification is
                          OFF but a configured client cert is still presented.
  GOPHERMIND_TEMPERATURE  sampling temperature [0,2] (default: 0; also /temp)
  GOPHERMIND_TOP_P        nucleus top_p (0,1] (default: unset; also /topp)
  GOPHERMIND_PROFILE    provider profile to select (also -profile/-p)
  GOPHERMIND_TRANSCRIPT JSONL dump of the full message history at session end
                        (also --transcript); MAY CONTAIN SENSITIVE PROMPTS/
                        RESPONSES — written 0600, never includes credentials

Provider profiles (selectable with -profile/-p):
  Built-ins: local-llama, openai, anthropic-proxy. Each profile resolves its
  endpoint from per-profile env vars over built-in defaults. Define your own by
  setting GOPHERMIND_PROFILE_<NAME>_BASE_URL (the name's '-' becomes '_', e.g.
  anthropic-proxy => GOPHERMIND_PROFILE_ANTHROPIC_PROXY_*):
    GOPHERMIND_PROFILE_<NAME>_BASE_URL
    GOPHERMIND_PROFILE_<NAME>_API_KEY
    GOPHERMIND_PROFILE_<NAME>_MODEL
    GOPHERMIND_PROFILE_<NAME>_TIMEOUT   (seconds)

Flags:`)
	flag.PrintDefaults()
}
