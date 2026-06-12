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

	baseFlag := flag.String("base", cfg.BaseURL, "OpenAI-compatible endpoint base URL")
	rootFlag := flag.String("root", cfg.RootDir, "repository root directory")
	modelFlag := flag.String("model", cfg.Model, "model name (default: auto-discover from the endpoint)")
	modeFlag := flag.String("mode", cfg.ApprovalMode, "approval mode for mutating tools: auto|ask")
	maxFlag := flag.Int("max", cfg.MaxIter, "maximum loop iterations per turn")
	insecureFlag := flag.Bool("insecure", cfg.InsecureTLS, "skip TLS verification (self-signed internal endpoints)")
	verboseFlag := flag.Bool("v", false, "verbose: stream assistant text and tool results")
	flag.Usage = usage
	flag.Parse()

	cfg.BaseURL = *baseFlag
	cfg.RootDir = *rootFlag
	cfg.Model = *modelFlag
	cfg.ApprovalMode = *modeFlag
	cfg.MaxIter = *maxFlag
	cfg.InsecureTLS = *insecureFlag
	if err := cfg.Validate(); err != nil {
		return err
	}

	args := flag.Args()
	cmd := "chat"
	if len(args) > 0 {
		cmd = strings.ToLower(args[0])
	}
	task := strings.TrimSpace(strings.Join(args[1:], " "))

	client := llm.New(cfg.BaseURL, cfg.APIKey, cfg.Model, cfg.HTTPTimeout, cfg.InsecureTLS)

	// Auto-discover the model when none was configured.
	if cfg.Model == "" {
		discovered, err := client.DiscoverModel(context.Background())
		if err != nil {
			return fmt.Errorf("no model set and discovery failed: %w (set -model or GOPHERMIND_MODEL)", err)
		}
		cfg.Model = discovered
		client.Model = discovered
	}

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
			Client:   client,
			Registry: reg,
			Model:    cfg.Model,
			Mode:     cfg.ApprovalMode,
			MaxIter:  cfg.MaxIter,
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
		answer, err := ag.Send(ctx, task)
		if err != nil {
			return err
		}
		fmt.Println(answer)
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

Flags:`)
	flag.PrintDefaults()
}
