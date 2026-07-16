package tui

import (
	"context"
	"sync"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/jbrahy/bubblecomplete"
	"github.com/jbrahy/bubblecomplete/ngram"
	"gophermind/internal/agent"
	"gophermind/internal/banner"
	"gophermind/internal/prompthistory"
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

const statusHeight = 1

type model struct {
	agent   *agent.Agent
	sub     chan tea.Msg // agent events + approval requests + done/err arrive here
	allowed *allowSet

	model string // model name, for the status line
	mode  string // "auto" | "ask"

	temperature float64  // current sampling temperature, mirrored from the client
	topP        *float64 // current top_p (nil when unset), mirrored from the client

	input    textarea.Model
	viewport viewport.Model
	spin     spinner.Model
	render   *glamour.TermRenderer

	// complete is the predictive-text controller (ghost text / popup menu),
	// fed by the command/file/recall/markov providers installed in newModel.
	// hist backs the recall provider and persists submitted prompts; ngram is
	// trained on that same history to back the markov provider. Wiring keys
	// and rendering to complete is Task 11's scope — here it is only
	// constructed and kept inert.
	complete bubblecomplete.Model
	hist     *prompthistory.Store
	ngram    *ngram.Model

	content string // committed transcript shown in the viewport
	stream  string // prose buffered during the current streaming turn

	st      state
	pending approvalMsg // valid when st == stateApproval
	cancel  context.CancelFunc

	// /project setup state machine (see project.go). proj is projNone unless a
	// guided new-project flow is active; projTurn marks that the in-flight agent
	// turn belongs to that flow so its completion is post-processed specially.
	proj        projPhase
	projName    string
	projRetries int
	projTurn    bool

	usage  agent.UsageSnapshot // running session token + cost meter
	width  int
	height int
	ready  bool

	// glamourStyle is a fixed glamour style name ("dark"/"light"), resolved once
	// before the program starts. Using a fixed style (never glamour.WithAutoStyle)
	// keeps the running Update loop from issuing an OSC background-color query,
	// whose reply would otherwise race Bubble Tea's stdin reader and leak into the
	// textarea.
	glamourStyle string

	// banner is the startup splash (gopher art + version + recent changes + a
	// random fortune), rendered once at construction so the fortune is stable for
	// the session.
	banner string
}

// newModel builds the model. buildAgent receives the bridge channel and the
// shared always-allow set so the agent's approval closure can consult them.
func newModel(buildAgent func(sub chan tea.Msg, allowed *allowSet) *agent.Agent, modelName, mode, glamourStyle string, noBanner, noFortune bool) model {
	sub := make(chan tea.Msg, 64)
	allowed := newAllowSet()

	ta := textarea.New()
	ta.Placeholder = "Ask gophermind to do something…"
	ta.Prompt = "› "
	ta.ShowLineNumbers = false
	ta.SetHeight(1)
	ta.CharLimit = 0
	ta.Focus()
	// Only the first visual row of a (possibly multi-line, auto-grown) input
	// shows "› "; continuation rows are blank but still reserve the same
	// column width so wrapped/typed text stays aligned. bubbles v1.0.0
	// supports this cleanly via SetPromptFunc, keyed by display row (post
	// wrap), so it also blanks the padding rows below the last line.
	ta.SetPromptFunc(2, func(displayRow int) string {
		if displayRow == 0 {
			return "› "
		}
		return ""
	})

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	r, _ := glamour.NewTermRenderer(glamour.WithStandardStyle(glamourStyle), glamour.WithWordWrap(80))

	ag := buildAgent(sub, allowed)

	// hist backs recall/markov completion and is best-effort: if it fails to
	// load (e.g. an unreadable history file), fall back to a non-nil empty
	// Store rather than propagating the error — completion degrades to no
	// recall/markov suggestions instead of the TUI failing to start.
	hist, err := prompthistory.New()
	if err != nil {
		hist = &prompthistory.Store{}
	}
	ng := ngram.New()
	ng.TrainAll(hist.All())

	cm := bubblecomplete.New(
		bubblecomplete.WithGhostStyle(lipgloss.NewStyle().Faint(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#475569", Dark: "#9CA3AF"})),
		bubblecomplete.WithMenuStyle(
			lipgloss.NewStyle().Border(lipgloss.RoundedBorder()),
			lipgloss.NewStyle().Reverse(true).Bold(true),
			lipgloss.NewStyle().Faint(true),
		),
	)
	cm.SetProviders(
		newCommandProvider(),
		newFileProvider(),
		newRecallProvider(hist.All),
		newMarkovProvider(ng),
	)

	m := model{
		agent:        ag,
		sub:          sub,
		allowed:      allowed,
		model:        modelName,
		mode:         mode,
		input:        ta,
		viewport:     viewport.New(0, 0),
		spin:         sp,
		render:       r,
		st:           stateIdle,
		glamourStyle: glamourStyle,
		banner:       renderBanner(noBanner, noFortune),
		complete:     cm,
		hist:         hist,
		ngram:        ng,
	}
	// Mirror the client's startup sampling settings so /temp and /topp with no
	// argument report the truth even before the user changes anything.
	if ag != nil {
		m.temperature = ag.Temperature()
		m.topP = ag.TopP()
	}
	return m
}

// renderBanner returns the startup splash, or an empty string when suppressed
// (via --no-banner/--quiet). noFortune (--fortune off) keeps the banner but drops
// the fortune line.
func renderBanner(noBanner, noFortune bool) string {
	if noBanner {
		return ""
	}
	return banner.RenderWith(banner.Options{Fortune: !noFortune, Tip: true})
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
