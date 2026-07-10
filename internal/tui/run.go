package tui

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gophermind/internal/agent"
	"gophermind/internal/llm"
	"gophermind/internal/tools"
)

// Config carries everything Run needs from the caller.
type Config struct {
	Client           *llm.Client
	Registry         *tools.Registry
	Model            string
	Mode             string // "auto" | "ask"
	MaxIter          int
	InputPricePer1K  float64 // per-1K-token input price for the cost meter
	OutputPricePer1K float64 // per-1K-token output price for the cost meter
	// TranscriptPath, when non-empty, receives a JSONL dump of the full message
	// history when the session exits. It may contain sensitive prompt/response
	// content; the agent writes it 0600 and never includes credentials.
	TranscriptPath string
	// SystemSuffix is appended to the agent's system prompt (e.g. project
	// instructions from CLAUDE.md/AGENTS.md).
	SystemSuffix string
}

// Run starts the interactive TUI and blocks until the user quits.
func Run(cfg Config) error {
	build := func(sub chan tea.Msg, allowed *allowSet) *agent.Agent {
		approve := func(tool, args string) bool {
			if cfg.Mode == "auto" || allowed.has(tool) {
				return true
			}
			reply := make(chan bool, 1)
			sub <- approvalMsg{tool: tool, args: args, reply: reply}
			return <-reply
		}
		onEvent := func(e agent.Event) {
			if msg := eventToMsg(e); msg != nil {
				sub <- msg
			}
		}
		ag := agent.New(cfg.Client, cfg.Registry, cfg.MaxIter, approve, onEvent)
		ag.SetPrices(cfg.InputPricePer1K, cfg.OutputPricePer1K)
		if cfg.SystemSuffix != "" {
			ag.AppendSystemPrompt(cfg.SystemSuffix)
		}
		return ag
	}

	// Detect the terminal background ONCE, here, before tea.Run() puts stdin into
	// raw mode and starts its input reader. lipgloss caches the result (sync.Once),
	// so every AdaptiveColor render during the session reuses it without re-querying,
	// and we pass a fixed glamour style so the render loop never issues an OSC
	// background-color query either. Doing this at runtime instead would leak the
	// terminal's escape-sequence reply into the textarea.
	glamourStyle := "light"
	if lipgloss.HasDarkBackground() {
		glamourStyle = "dark"
	}

	m := newModel(build, cfg.Model, cfg.Mode, glamourStyle)
	final, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	// On exit, flush the full message history if a transcript path was set. This
	// runs once, after the UI has torn down, so it never interferes with the
	// alt-screen. A write failure is surfaced but does not mask a UI error.
	if cfg.TranscriptPath != "" {
		if fm, ok := final.(model); ok && fm.agent != nil {
			if werr := fm.agent.WriteTranscript(cfg.TranscriptPath); werr != nil {
				fmt.Fprintln(os.Stderr, "warning: transcript export failed:", werr)
			}
		}
	}
	return err
}
