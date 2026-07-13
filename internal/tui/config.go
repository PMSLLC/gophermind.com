package tui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
	"gophermind/internal/config"
	"gophermind/internal/setup"
)

// handleConfigCommand launches the interactive configuration wizard. The wizard
// is line-based and needs sole control of the terminal, so it runs via tea.Exec:
// Bubble Tea pauses the program, restores cooked mode, hands the wizard the real
// stdin/stdout, and resumes once it finishes — which avoids the two readers
// (wizard vs. Bubble Tea's input loop) fighting over stdin.
func (m model) handleConfigCommand() (model, tea.Cmd) {
	if m.agent == nil {
		m.appendLine("config: no active session")
		m.sync()
		return m, nil
	}
	// Pre-fill the current values. Endpoint/model/max-iter come from the agent;
	// the approval mode is what the TUI is authoritatively displaying.
	cur := m.agent.Config()
	w := &configWizard{defaults: setup.Result{
		BaseURL:      cur.BaseURL,
		Model:        cur.Model,
		ApprovalMode: m.mode,
		MaxIter:      cur.MaxIter,
	}}

	cmd := tea.Exec(w, func(err error) tea.Msg {
		if err != nil {
			return errMsg{err: fmt.Errorf("config wizard: %w", err)}
		}
		return configDoneMsg{result: w.result}
	})
	return m, cmd
}

// configWizard adapts the line-based wizard to Bubble Tea's ExecCommand, so it
// can run with full terminal control while the program is paused.
type configWizard struct {
	defaults setup.Result
	result   setup.Result
	stdin    io.Reader
	stdout   io.Writer
}

func (w *configWizard) SetStdin(r io.Reader)  { w.stdin = r }
func (w *configWizard) SetStdout(o io.Writer) { w.stdout = o }
func (w *configWizard) SetStderr(io.Writer)   {}

func (w *configWizard) Run() error {
	in, out := w.stdin, w.stdout
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	res, err := runConfigWizardIO(in, out, w.defaults)
	if err != nil {
		return err
	}
	w.result = res
	return nil
}

// runConfigWizardIO drives the interactive setup wizard, reading answers from in
// and writing prompts to out. The API key is read without echo straight from the
// controlling terminal. It returns the captured result.
func runConfigWizardIO(in io.Reader, out io.Writer, defaults setup.Result) (setup.Result, error) {
	r := bufio.NewReader(in)
	readLine := func() (string, error) {
		s, err := r.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}
		return strings.TrimRight(s, "\r\n"), nil
	}
	readSecret := func() (string, error) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(out) // ReadPassword swallows the echoed newline
		return strings.TrimSpace(string(b)), err
	}

	// 1) Endpoint: a built-in profile or a custom URL.
	fmt.Fprintln(out, "Endpoint:")
	profiles := config.BuiltinProfileNames()
	for i, p := range profiles {
		fmt.Fprintf(out, "  %d) %s (%s)\n", i+1, p[0], p[1])
	}
	fmt.Fprintf(out, "  %d) custom URL\n", len(profiles)+1)
	fmt.Fprint(out, "Choose [1]: ")
	choiceLine, err := readLine()
	if err != nil {
		return setup.Result{}, err
	}
	choice := parseIntOr(choiceLine, 1)

	var baseURL string
	if choice >= 1 && choice <= len(profiles) {
		baseURL = profiles[choice-1][1]
	} else {
		fmt.Fprintf(out, "Base URL%s: ", defaultHint(defaults.BaseURL))
		line, err := readLine()
		if err != nil {
			return setup.Result{}, err
		}
		baseURL = firstNonEmpty(strings.TrimSpace(line), defaults.BaseURL)
	}

	// 2) API key (read without echo; blank keeps none).
	fmt.Fprint(out, "API key (blank = none): ")
	apiKey, err := readSecret()
	if err != nil {
		return setup.Result{}, err
	}

	// 3) Model (free-text; blank = auto-discover).
	fmt.Fprintf(out, "Model (blank = auto-discover)%s: ", defaultHint(defaults.Model))
	line, err := readLine()
	if err != nil {
		return setup.Result{}, err
	}
	model := firstNonEmpty(strings.TrimSpace(line), defaults.Model)

	// 4) Approval mode.
	fmt.Fprintf(out, "Approval mode ask/auto%s: ", defaultHint(defaults.ApprovalMode))
	line, err = readLine()
	if err != nil {
		return setup.Result{}, err
	}
	mode := firstNonEmpty(strings.ToLower(strings.TrimSpace(line)), defaults.ApprovalMode, "ask")
	if mode != "auto" {
		mode = "ask"
	}

	// 5) Max iterations per turn.
	maxDefault := defaults.MaxIter
	if maxDefault < 1 {
		maxDefault = 25
	}
	fmt.Fprintf(out, "Max iterations per turn [%d]: ", maxDefault)
	line, err = readLine()
	if err != nil {
		return setup.Result{}, err
	}
	maxIter := parseIntOr(line, maxDefault)
	if maxIter < 1 {
		maxIter = maxDefault
	}

	// 6) Optional integration credentials (blank to skip).
	fmt.Fprint(out, "Brave Search API key (optional): ")
	brave, err := readLine()
	if err != nil {
		return setup.Result{}, err
	}
	fmt.Fprint(out, "GitHub token (optional): ")
	ghToken, err := readLine()
	if err != nil {
		return setup.Result{}, err
	}
	fmt.Fprint(out, "Slack/Discord notify webhook URL (optional): ")
	notify, err := readLine()
	if err != nil {
		return setup.Result{}, err
	}

	return setup.Result{
		BaseURL: baseURL, APIKey: apiKey, Model: model, ApprovalMode: mode, MaxIter: maxIter,
		BraveAPIKey:   strings.TrimSpace(brave),
		GitHubToken:   strings.TrimSpace(ghToken),
		NotifyWebhook: strings.TrimSpace(notify),
	}, nil
}

// configDoneMsg carries the wizard's result back into the TUI after tea.Exec.
type configDoneMsg struct {
	result setup.Result
}

var (
	configSavedStyle = lipgloss.NewStyle().Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#059669", Dark: "#34D399"})
	configInfoStyle = lipgloss.NewStyle().Faint(true)
)

// handleConfigDone persists the wizard result, applies what can change live, and
// prints a summary of what actually changed (compared against the pre-wizard
// values — the comparison must happen before the setters run).
func (m *model) handleConfigDone(msg configDoneMsg) {
	res := msg.result

	p, err := config.ConfigFilePath()
	if err != nil {
		m.appendLine("error: could not resolve config path: " + err.Error())
		m.st = stateIdle
		m.sync()
		return
	}
	if err := setup.WriteEnv(p, res.Pairs()); err != nil {
		m.appendLine("error saving config: " + err.Error())
		m.st = stateIdle
		m.sync()
		return
	}

	before := m.agent.Config()
	beforeMode := m.mode

	// Apply live. Endpoint/model/key/max-iter take effect for the next request;
	// approval mode only fully applies to "auto" now (see agent.SetApprovalMode).
	var changes []string
	if res.BaseURL != "" && res.BaseURL != before.BaseURL {
		m.agent.SetBaseURL(res.BaseURL)
		changes = append(changes, "endpoint")
	}
	if res.Model != "" && res.Model != before.Model {
		m.agent.SetModel(res.Model)
		m.model = res.Model
		changes = append(changes, "model")
	}
	if res.ApprovalMode != "" && res.ApprovalMode != beforeMode {
		m.agent.SetApprovalMode(res.ApprovalMode)
		m.mode = res.ApprovalMode
		changes = append(changes, "approval mode")
	}
	if res.MaxIter > 0 && res.MaxIter != before.MaxIter {
		m.agent.SetMaxIter(res.MaxIter)
		changes = append(changes, "max iterations")
	}
	if res.APIKey != "" {
		m.agent.SetAPIKey(res.APIKey)
		changes = append(changes, "API key")
	}

	m.appendLine(configSavedStyle.Render("✓ Config saved to " + p))
	if len(changes) > 0 {
		m.appendLine(configInfoStyle.Render("Updated: " + strings.Join(changes, ", ") + "."))
	} else {
		m.appendLine(configInfoStyle.Render("No changes (values unchanged)."))
	}
	if res.ApprovalMode == "ask" && beforeMode != "ask" {
		m.appendLine(configInfoStyle.Render("Note: switching to ask-mode fully applies on next launch."))
	}
	m.st = stateIdle
	m.sync()
}

// parseIntOr parses s as an int, returning fallback on empty or invalid input.
func parseIntOr(s string, fallback int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return fallback
	}
	return n
}

// firstNonEmpty returns the first non-empty string, or "".
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// defaultHint renders a bracketed default hint for a non-empty value.
func defaultHint(def string) string {
	if def == "" {
		return ""
	}
	return " [" + def + "]"
}
