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
	"slices"
	"strings"
	"time"

	"golang.org/x/term"
	"gophermind/internal/agent"
	"gophermind/internal/config"
	"gophermind/internal/llm"
	"gophermind/internal/safety"
	"gophermind/internal/setup"
	"gophermind/internal/tools"
	"gophermind/internal/tui"
	"gophermind/internal/ui"
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
	if len(args) > 0 {
		cmd = strings.ToLower(args[0])
		task = strings.TrimSpace(strings.Join(args[1:], " "))
	}

	// `gophermind version` prints build metadata and exits (needs no config).
	if cmd == "version" {
		fmt.Println(version.String())
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

func usage() {
	fmt.Fprintln(os.Stderr, `gophermind - a minimal agentic coding harness

Usage:
  gophermind                    interactive session (default)
  gophermind config             (re-)run the setup wizard and save config
  gophermind version            print build version and exit
  gophermind run "task"         one-shot: run a task and exit
  gophermind ask "question"     one-shot: answer without modifying files

On first interactive launch with nothing configured, a short setup wizard runs
and saves your choices to the global config (see below); later launches skip it.

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
