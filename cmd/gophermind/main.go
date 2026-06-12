// Command gophermind is a minimal agentic coding harness. Run it with no
// arguments for an interactive terminal session, or `run`/`ask` for one-shot
// use. It drives an OpenAI-compatible model through a read/search/edit/shell
// tool loop against the current repository.
package main

import (
	"bufio"
	"context"
	"errors"
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
		printer := ui.Printer{Verbose: true} // interactive: always show activity
		ag := agent.New(client, reg, cfg.MaxIter, approve, printer.Event)
		return repl(stdin, ag, cfg)
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

// repl runs the interactive session. Each line is one turn; Ctrl-C interrupts
// the current turn and returns to the prompt; Ctrl-D (EOF) or "exit"/"quit"
// ends the session.
func repl(stdin *bufio.Reader, ag *agent.Agent, cfg config.Config) error {
	fmt.Fprintf(os.Stderr, "gophermind — %s @ %s  [%s mode, root %s]\n", cfg.Model, cfg.BaseURL, cfg.ApprovalMode, cfg.RootDir)
	fmt.Fprintln(os.Stderr, "Type a task. Ctrl-C interrupts, Ctrl-D or \"exit\" quits.")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	for {
		fmt.Fprint(os.Stderr, "\n› ")
		line, err := stdin.ReadString('\n')
		if err != nil { // EOF
			fmt.Fprintln(os.Stderr)
			return nil
		}
		line = strings.TrimSpace(line)
		switch line {
		case "":
			continue
		case "exit", "quit":
			return nil
		}

		// Drop any stray interrupt that arrived while idle at the prompt.
		select {
		case <-sigCh:
		default:
		}

		turnCtx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			select {
			case <-sigCh:
				cancel()
			case <-done:
			}
		}()

		answer, err := ag.Send(turnCtx, line)
		close(done)
		cancel()

		switch {
		case errors.Is(err, context.Canceled):
			fmt.Fprintln(os.Stderr, "\n(interrupted)")
		case err != nil:
			fmt.Fprintln(os.Stderr, "error:", err)
		case answer != "":
			fmt.Println("\n" + answer)
		}
	}
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
