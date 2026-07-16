package bubblecomplete

import (
	"reflect"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Mode reports what, if anything, the completion Model is currently showing.
type Mode int

const (
	ModeNone Mode = iota
	ModeGhost
	ModeMenu
)

// Result is what Update returns to the host.
type Result struct {
	Accepted *Candidate // non-nil when the user just accepted a suggestion
	Consumed bool       // true if the completion model handled the key (host must NOT also process it)
}

// Model is the hybrid ghost-text / popup-menu completion controller. It is a
// Bubble Tea sub-model: the host owns the actual text input and asks Model to
// recompute suggestions (Query) and to handle navigation/accept keys (Update).
type Model struct {
	providers []Provider

	input  string
	cursor int

	mode       Mode
	candidates []Candidate
	selected   int
	ghost      string

	ghostStyle    lipgloss.Style
	borderStyle   lipgloss.Style
	selectedStyle lipgloss.Style
	descStyle     lipgloss.Style

	maxMenuRows int
}

// Option configures a Model at construction time.
type Option func(*Model)

// WithGhostStyle sets the style used for GhostStyle() (the host renders the
// inline ghost text with it).
func WithGhostStyle(s lipgloss.Style) Option {
	return func(m *Model) {
		m.ghostStyle = s
	}
}

// WithMenuStyle sets the popup menu's border, selected-row, and description
// styles. Any argument left as the zero lipgloss.Style{} keeps the default.
func WithMenuStyle(border, selected, desc lipgloss.Style) Option {
	return func(m *Model) {
		if !isZeroStyle(border) {
			m.borderStyle = border
		}
		if !isZeroStyle(selected) {
			m.selectedStyle = selected
		}
		if !isZeroStyle(desc) {
			m.descStyle = desc
		}
	}
}

// WithMaxMenuRows caps how many candidate rows the popup menu renders.
// Default is 8.
func WithMaxMenuRows(n int) Option {
	return func(m *Model) {
		m.maxMenuRows = n
	}
}

func isZeroStyle(s lipgloss.Style) bool {
	return reflect.DeepEqual(s, lipgloss.Style{})
}

// New constructs a Model with sensible default styling, overridable via
// Option.
func New(opts ...Option) Model {
	m := Model{
		ghostStyle:    lipgloss.NewStyle().Faint(true),
		borderStyle:   lipgloss.NewStyle().Border(lipgloss.RoundedBorder()),
		selectedStyle: lipgloss.NewStyle().Reverse(true).Bold(true),
		descStyle:     lipgloss.NewStyle().Faint(true),
		maxMenuRows:   8,
	}
	for _, opt := range opts {
		opt(&m)
	}
	return m
}

// SetProviders replaces the ordered list of providers Query walks.
func (m *Model) SetProviders(ps ...Provider) {
	m.providers = ps
}

// Query recomputes mode/candidates/ghost from the providers for the given
// input and cursor position. Providers are walked in priority order; the
// first one to return a non-empty result wins and later providers are
// ignored. Exactly one candidate selects ModeGhost, more than one selects
// ModeMenu (with selection reset to the first row), and zero selects
// ModeNone.
func (m Model) Query(input string, cursor int) Model {
	m.input = input
	m.cursor = cursor

	var winning []Candidate
	for _, p := range m.providers {
		cs := p.Suggest(input, cursor)
		if len(cs) > 0 {
			winning = cs
			break
		}
	}

	m.candidates = winning
	m.selected = 0

	switch len(winning) {
	case 0:
		m.mode = ModeNone
		m.ghost = ""
	case 1:
		m.mode = ModeGhost
		m.ghost = winning[0].Text
	default:
		m.mode = ModeMenu
		m.ghost = ""
	}

	return m
}

// Update handles navigation/accept keys, consuming a key only when the
// completion model is active. When Consumed is false the host must process
// the key itself.
func (m Model) Update(msg tea.Msg) (Model, Result) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, Result{}
	}

	if m.mode == ModeNone {
		return m, Result{}
	}

	switch keyMsg.Type {
	case tea.KeyTab:
		nm, cand := m.Accept()
		return nm, Result{Accepted: cand, Consumed: true}

	case tea.KeyRight:
		if m.mode == ModeGhost && m.cursor == utf8.RuneCountInString(m.input) {
			nm, cand := m.Accept()
			return nm, Result{Accepted: cand, Consumed: true}
		}
		return m, Result{Consumed: false}

	case tea.KeyUp:
		if m.mode != ModeMenu {
			return m, Result{Consumed: false}
		}
		nm := m
		if nm.selected > 0 {
			nm.selected--
		}
		return nm, Result{Consumed: true}

	case tea.KeyDown:
		if m.mode != ModeMenu {
			return m, Result{Consumed: false}
		}
		nm := m
		if nm.selected < len(nm.candidates)-1 {
			nm.selected++
		}
		return nm, Result{Consumed: true}

	case tea.KeyEsc:
		nm := m
		nm.mode = ModeNone
		nm.candidates = nil
		nm.ghost = ""
		nm.selected = 0
		return nm, Result{Consumed: true}

	default:
		return m, Result{Consumed: false}
	}
}

// Accept accepts the current ghost or selected menu candidate and dismisses
// the completion model, returning the accepted Candidate (nil if nothing was
// active). Hosts call this directly to implement "Enter accepts an open
// menu."
func (m Model) Accept() (Model, *Candidate) {
	var cand *Candidate
	switch m.mode {
	case ModeGhost:
		if len(m.candidates) > 0 {
			c := m.candidates[0]
			cand = &c
		}
	case ModeMenu:
		if m.selected >= 0 && m.selected < len(m.candidates) {
			c := m.candidates[m.selected]
			cand = &c
		}
	}

	nm := m
	nm.mode = ModeNone
	nm.candidates = nil
	nm.ghost = ""
	nm.selected = 0
	return nm, cand
}

// View renders the popup menu block when ModeMenu is active. It returns ""
// in every other mode; the host is responsible for drawing the ghost text
// inline in its own input widget (see Ghost/GhostStyle).
func (m Model) View() string {
	if m.mode != ModeMenu {
		return ""
	}

	rows := m.candidates
	if len(rows) > m.maxMenuRows {
		rows = rows[:m.maxMenuRows]
	}

	lines := make([]string, 0, len(rows))
	for i, c := range rows {
		display := c.Display
		if display == "" {
			display = c.Text
		}
		line := display
		if c.Desc != "" {
			line = display + "  " + m.descStyle.Render(c.Desc)
		}
		if i == m.selected {
			line = m.selectedStyle.Render(line)
		}
		lines = append(lines, line)
	}

	return m.borderStyle.Render(strings.Join(lines, "\n"))
}

// Ghost returns the inline ghost continuation text when ModeGhost is active,
// "" otherwise. The host renders this itself using GhostStyle.
func (m Model) Ghost() string {
	if m.mode != ModeGhost {
		return ""
	}
	return m.ghost
}

// GhostStyle is the style the host should use to render the inline ghost
// text returned by Ghost.
func (m Model) GhostStyle() lipgloss.Style {
	return m.ghostStyle
}

// Active reports whether the completion model currently has a suggestion
// (ghost or menu) to show.
func (m Model) Active() bool {
	return m.mode != ModeNone
}

// Mode reports the current display mode.
func (m Model) Mode() Mode {
	return m.mode
}
