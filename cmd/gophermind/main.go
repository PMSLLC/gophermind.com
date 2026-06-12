// Command gophermind is a minimal agentic coding harness. Run it with no
// arguments for an interactive terminal session, or `run`/`ask` for one-shot
// use. It drives an OpenAI-compatible model through a read/search/edit/shell
// tool loop against the current repository.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"gophermind/internal/agent"
	"gophermind/internal/config"
	"gophermind/internal/llm"
	"gophermind/internal/safety"
	"gophermind/internal/tools"
	"gophermind/internal/tui"
	"gophermind/internal/ui"
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
	verboseFlag := flag.Bool("v", false, "verbose: stream assistant text and tool results")
	transcriptFlag := flag.String("transcript", cfg.TranscriptPath, "write the full wire-level message history (JSONL) to this path at session end; MAY CONTAIN SENSITIVE PROMPTS/RESPONSES (file written 0600, no credentials included)")
	flag.Usage = usage
	flag.Parse()

	// Which flags the user set explicitly — these take precedence over a
	// profile's resolved values.
	set := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { set[f.Name] = true })

	// Select and resolve the profile first (flag > env). When a profile is
	// active it fills BaseURL/Model/APIKey/HTTPTimeout from per-profile env >
	// built-in defaults; an unknown name is a hard error.
	cfg.Profile = *profileFlag
	cfg.RootDir = *rootFlag
	cfg.ApprovalMode = *modeFlag
	cfg.MaxIter = *maxFlag
	cfg.InsecureTLS = *insecureFlag
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
	if err := cfg.Validate(); err != nil {
		return err
	}

	args := flag.Args()
	cmd := "chat"
	task := ""
	if len(args) > 0 {
		cmd = strings.ToLower(args[0])
		task = strings.TrimSpace(strings.Join(args[1:], " "))
	}

	client := llm.New(cfg.BaseURL, cfg.APIKey, cfg.Model, cfg.HTTPTimeout, cfg.InsecureTLS)
	client.Fallbacks = cfg.FallbackModels
	client.SetTemperature(cfg.Temperature)
	client.SetTopP(cfg.TopP)
	client.Retry = llm.RetryPolicy{
		MaxAttempts: cfg.MaxAttempts,
		BaseDelay:   cfg.RetryBaseDelay,
		MaxDelay:    llm.DefaultRetryPolicy.MaxDelay,
	}
	if cfg.CacheEnabled {
		client.Cache = &llm.Cache{Dir: cfg.CacheDir, TTL: cfg.CacheTTL}
	}

	// Auto-discover the model when none was configured.
	if cfg.Model == "" {
		discovered, err := client.DiscoverModel(context.Background())
		if err != nil {
			return fmt.Errorf("no model set and discovery failed: %w (set -model or GOPHERMIND_MODEL)", err)
		}
		cfg.Model = discovered
		client.Model = discovered
	}

	// Probe the model's capabilities (context window, max output, tool support)
	// so they are available to adapt truncation/iteration limits. This never
	// fails: it degrades to a built-in table and conservative defaults, and the
	// result is cached per endpoint+model on the client.
	caps := client.ProbeCapabilities(context.Background())
	fmt.Fprintf(os.Stderr, "model capabilities: %s\n", caps)

	reg := tools.NewRegistry(
		tools.ReadFile(cfg.RootDir),
		tools.ListFiles(cfg.RootDir),
		tools.Search(cfg.RootDir),
		tools.WriteFile(cfg.RootDir),
		tools.EditFile(cfg.RootDir),
		tools.RunShell(cfg.RootDir, cfg.CmdTimeout),
	)

	// A single shared stdin reader, used by both the REPL and approval prompts.
	stdin := bufio.NewReader(os.Stdin)
	approve := safety.ApprovalFunc(safety.Auto)
	if cfg.ApprovalMode == "ask" {
		approve = func(tool, argsJSON string) bool { return ui.Confirm(stdin, tool, argsJSON) }
	}

	switch cmd {
	case "chat":
		if !isatty() {
			return fmt.Errorf("interactive session needs a terminal; use `run`/`ask` for non-interactive use")
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
		})
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
		answer, sendErr := ag.Send(ctx, task)
		// Write the transcript even when the turn errored: a partial history is
		// still useful for debugging. The dump never includes credentials.
		if cfg.TranscriptPath != "" {
			if err := ag.WriteTranscript(cfg.TranscriptPath); err != nil {
				fmt.Fprintln(os.Stderr, "warning: transcript export failed:", err)
			}
		}
		if sendErr != nil {
			return sendErr
		}
		fmt.Println(answer)
		// Token + cost meter goes to stderr so stdout stays pipeable.
		fmt.Fprintln(os.Stderr, ag.Usage().String())
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command: %s", cmd)
	}
}

// isatty reports whether stdin is an interactive terminal.
func isatty() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && (fi.Mode()&os.ModeCharDevice) != 0
}

func usage() {
	fmt.Fprintln(os.Stderr, `gophermind - a minimal agentic coding harness

Usage:
  gophermind                    interactive session (default)
  gophermind run "task"         one-shot: run a task and exit
  gophermind ask "question"     one-shot: answer without modifying files

Environment (all optional; flags override):
  GOPHERMIND_BASE_URL   endpoint (default: built-in)
  GOPHERMIND_MODEL      model name (default: auto-discovered)
  GOPHERMIND_API_KEY    bearer token (omit when reached over VPN)
  GOPHERMIND_APPROVAL   auto|ask (default: ask)
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
