package tui

import (
	tea "github.com/charmbracelet/bubbletea"
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
		return ag
	}

	m := newModel(build, cfg.Model, cfg.Mode)
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}
