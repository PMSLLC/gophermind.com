package tui

import (
	"context"
	"sync"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"gophermind/internal/agent"
)

type state int

const (
	stateIdle state = iota
	stateWorking
	stateApproval
)

// allowSet is a mutex-guarded set of tools the user chose to always allow this
// session. It is shared between the UI goroutine (which adds on the "a" key) and
// the agent goroutine (which reads in the approval closure).
type allowSet struct {
	mu sync.Mutex
	m  map[string]bool
}

func newAllowSet() *allowSet { return &allowSet{m: map[string]bool{}} }

func (a *allowSet) add(tool string) {
	a.mu.Lock()
	a.m[tool] = true
	a.mu.Unlock()
}

func (a *allowSet) has(tool string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.m[tool]
}

const (
	inputHeight  = 3 // bordered single-line textarea
	statusHeight = 1
)

type model struct {
	agent   *agent.Agent
	sub     chan tea.Msg // agent events + approval requests + done/err arrive here
	allowed *allowSet

	model string // model name, for the status line
	mode  string // "auto" | "ask"

	input    textarea.Model
	viewport viewport.Model
	spin     spinner.Model
	render   *glamour.TermRenderer

	content string // committed transcript shown in the viewport
	stream  string // prose buffered during the current streaming turn

	st      state
	pending approvalMsg // valid when st == stateApproval
	cancel  context.CancelFunc

	tokens int
	width  int
	height int
	ready  bool
}

// newModel builds the model. buildAgent receives the bridge channel and the
// shared always-allow set so the agent's approval closure can consult them.
func newModel(buildAgent func(sub chan tea.Msg, allowed *allowSet) *agent.Agent, modelName, mode string) model {
	sub := make(chan tea.Msg, 64)
	allowed := newAllowSet()

	ta := textarea.New()
	ta.Placeholder = "Ask gophermind to do something…"
	ta.Prompt = "› "
	ta.ShowLineNumbers = false
	ta.SetHeight(1)
	ta.CharLimit = 0
	ta.Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	r, _ := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(80))

	return model{
		agent:    buildAgent(sub, allowed),
		sub:      sub,
		allowed:  allowed,
		model:    modelName,
		mode:     mode,
		input:    ta,
		viewport: viewport.New(0, 0),
		spin:     sp,
		render:   r,
		st:       stateIdle,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.spin.Tick, waitFor(m.sub))
}

// appendLine adds one line to the committed transcript.
func (m *model) appendLine(s string) {
	if m.content != "" {
		m.content += "\n"
	}
	m.content += s
}

// sync pushes the committed content plus any in-progress stream into the
// viewport and scrolls to the bottom.
func (m *model) sync() {
	if !m.ready {
		return
	}
	body := m.content
	if m.stream != "" {
		if body != "" {
			body += "\n"
		}
		body += m.stream
	}
	m.viewport.SetContent(body)
	m.viewport.GotoBottom()
}
