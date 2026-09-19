package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gophermind/gophermind-lib/llm"
	"gophermind/gophermind-lib/phaseflow"
	"gophermind/gophermind-lib/plantree"
	"gophermind/gophermind-lib/plantree/plan"
)

// This file implements "/project <name> <brief>": read the brief file, build
// the plan tree in .planning/plan with the two planning passes, ask the
// questions the passes raised in the round question_round.go hosts, then
// approve the plan and export it for /project-execute.
//
// It replaced the interview: the model no longer writes ROADMAP.md and
// assignments.json itself, so there is nothing to interview it into writing.
// See docs/superpowers/specs/2026-09-19-brief-workflow-design.md.

// projPhase is the step of the /project flow the model is in.
type projPhase int

const (
	projNone      projPhase = iota
	projAwaitName           // waiting for "<name> <brief path>"
	projRunning             // the planning passes are running
	projApprove             // every step is specified; waiting for approve/revise/cancel
)

// projectApproval classifies a projApprove input.
type projectApproval int

const (
	approvalApprove projectApproval = iota
	approvalCancel
	approvalRevise
)

// parseApproval interprets an approval input as approve, cancel, or a
// revision request (revise carries the requested change text).
func parseApproval(text string) (kind projectApproval, revise string) {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "y", "yes", "approve", "approved", "ok", "lgtm":
		return approvalApprove, ""
	case "cancel", "abort", "quit", "stop":
		return approvalCancel, ""
	}
	if r, ok := strings.CutPrefix(strings.TrimSpace(text), "revise:"); ok {
		return approvalRevise, strings.TrimSpace(r)
	}
	return approvalRevise, strings.TrimSpace(text)
}

// projectProgressMsg is one line from the planning goroutine.
type projectProgressMsg string

// projectPassesDoneMsg carries both finished passes and what the plan needs
// next, read from the tree after they ran.
type projectPassesDoneMsg struct {
	res1    plan.Result
	res2    plan.Result2
	actions plantree.Actions
	open    []plan.Question
}

// briefExtensions are the suffixes that make a trailing token look like a
// brief path even when no such file exists, so a typo is reported instead of
// silently becoming part of the project name.
var briefExtensions = []string{".md", ".markdown", ".txt", ".rst"}

// looksLikeBriefPath reports whether a token was meant to be a file path.
func looksLikeBriefPath(s string) bool {
	if strings.ContainsRune(s, '/') || strings.ContainsRune(s, filepath.Separator) {
		return true
	}
	lower := strings.ToLower(s)
	for _, ext := range briefExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// parseProjectCommand splits "/project <name> <brief-path>" into a name and
// the brief path. The last token is the brief when it is a real file; a lone
// token is always the name, because /project requires one.
//
// A last token that was clearly meant to be a path (it has a separator or a
// document extension) but is not a readable file is an error. It used to
// become part of the project name, so a mistyped brief produced a project
// named after the typo and an interview that had never read the brief.
func parseProjectCommand(text string) (name, briefPath string, err error) {
	fields := strings.Fields(text)
	if len(fields) <= 1 {
		return "", "", nil
	}
	rest := fields[1:]
	if len(rest) < 2 {
		return strings.TrimSpace(strings.Join(rest, " ")), "", nil
	}
	last := rest[len(rest)-1]
	switch info, statErr := os.Stat(last); {
	case statErr == nil && !info.IsDir():
		return strings.TrimSpace(strings.Join(rest[:len(rest)-1], " ")), last, nil
	case statErr == nil:
		return "", "", fmt.Errorf("the brief %q is a directory, not a file", last)
	case looksLikeBriefPath(last):
		return "", "", fmt.Errorf("no brief file at %q", last)
	}
	return strings.TrimSpace(strings.Join(rest, " ")), "", nil
}

// handleProjectCommand dispatches "/project [name] [brief-path]".
func (m model) handleProjectCommand(text string) (model, tea.Cmd) {
	name, briefPath, err := parseProjectCommand(text)
	if err != nil {
		return m.projectError(err.Error())
	}
	if name == "" {
		m.proj = projAwaitName
		m.appendLine(projectBannerStyle.Render("New project. Type: <name> <path to the brief file>"))
		m.appendLine("A name alone resumes a plan this directory already has.")
		m.sync()
		return m, nil
	}
	return m.startProject(name, briefPath)
}

// startProject reads the brief, scaffolds .planning/ if this is a new
// project, and starts the two planning passes. With no brief path it resumes
// the plan already in .planning/plan, which is what makes /project safe to
// re-run after an error or a cancel.
func (m model) startProject(name, briefPath string) (model, tea.Cmd) {
	root, err := os.Getwd()
	if err != nil {
		return m.projectError(err.Error())
	}
	repo := plantree.Open(phaseflow.PlanningDir(root))
	_, rootErr := repo.Get(plantree.RootID)
	switch {
	case rootErr == nil:
	case errors.Is(rootErr, plantree.ErrNotFound):
	default:
		return m.projectError("the plan in " + repo.Dir() + " cannot be read: " + rootErr.Error())
	}
	existing := rootErr == nil

	brief, err := briefFor(repo, briefPath, existing)
	if err != nil {
		return m.projectError(err.Error())
	}

	e := phaseflow.New(root)
	scaffolded := false
	if !e.Initialized() {
		if err := e.Init(name); err != nil {
			return m.projectError(err.Error())
		}
		scaffolded = true
	}
	m.projName = name
	if existing {
		m.appendLine(projectBannerStyle.Render("Resuming the plan for “" + name + "” in " + repo.Dir()))
	} else {
		m.appendLine(projectBannerStyle.Render("Planning “" + name + "” from " + briefPath))
	}
	if scaffolded {
		m.appendLine("scaffolded " + phaseflow.PlanningDirName + " with a placeholder ROADMAP.md and PROJECT.md; approving exports the real plan over them, so the export report lists them as replaced")
	}
	return m.startPlanning(repo, name, brief)
}

// briefFor returns the brief text to plan from: the file the owner named, or
// the one the existing run stored. Reading the stored brief is what makes a
// resume identical to the run it resumes, which is what RunPass1's cursor
// requires.
func briefFor(repo *plantree.Repo, briefPath string, existing bool) (string, error) {
	if briefPath == "" {
		if !existing {
			return "", errors.New("give a brief file: /project <name> <path to the brief>")
		}
		stored, err := plan.ReadBrief(repo)
		if err != nil {
			return "", fmt.Errorf("reading the stored brief: %w", err)
		}
		if strings.TrimSpace(stored) == "" {
			return "", errors.New("this plan has no stored brief; pass the brief file again")
		}
		return stored, nil
	}
	b, err := os.ReadFile(briefPath)
	if err != nil {
		return "", fmt.Errorf("reading the brief: %w", err)
	}
	if strings.TrimSpace(string(b)) == "" {
		return "", fmt.Errorf("the brief %s is empty", briefPath)
	}
	if !existing {
		return string(b), nil
	}
	// A resume must re-supply an identical brief: the pass-1 cursor is only
	// valid against the same chunk boundaries. Saying so here is clearer than
	// letting RunPass1 refuse after the run has apparently started.
	stored, err := plan.ReadBrief(repo)
	if err == nil && strings.TrimSpace(stored) != "" && stored != string(b) {
		return "", errors.New("a plan already exists here and was built from a different brief; run /project <name> with no brief to resume it, or remove " + repo.Dir() + " to start over")
	}
	return string(b), nil
}

// startPlanning runs both passes on a goroutine, under the plan's run lock so
// a second session cannot run passes over the same tree, and posts progress
// and the result back through m.sub. It is cancellable the same way an agent
// turn is (Esc or Ctrl-C), and both passes resume from the tree, so a
// cancelled run loses nothing already written.
func (m model) startPlanning(repo *plantree.Repo, name, brief string) (model, tea.Cmd) {
	c := m.planCompleter()
	if c == nil {
		return m.projectError("no active session, so nothing can be planned")
	}
	client, known := m.planClient(), m.planWindow
	m.proj = projRunning
	m.st = stateWorking
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	sub := m.sub
	go func() {
		// The result is sent after runPasses has returned, so the run lock it
		// holds is already free when the session reacts: a resume typed the
		// moment the transcript says the run stopped finds nothing held.
		sub <- runPasses(ctx, repo, c, client, known, name, brief, sub)
	}()
	m.sync()
	return m, nil
}

// planClient is this session's client, or nil when there is no agent (a test).
func (m model) planClient() *llm.Client {
	if m.agent == nil {
		return nil
	}
	return m.agent.LLM()
}

// windowOf is the context window planning is sized for: known when positive
// (a test hook), otherwise what the client's capability probe reports, which
// the client caches, so asking again costs nothing. Zero means unknown.
func windowOf(ctx context.Context, client *llm.Client, known int) int {
	if known > 0 {
		return known
	}
	if client != nil {
		return client.ProbeCapabilities(ctx).ContextWindow
	}
	return 0
}

// runPasses is the planning run itself: take the plan's run lock, size the
// passes for the model's context window, run pass 1 then pass 2, and report
// what the tree needs next. Progress goes to sub as it happens; the return
// value is the run's one terminal message.
//
// Both passes take the same run lock, which is re-entrant within a process,
// so the nesting is not a deadlock. Both also resume from the tree, so a
// cancelled or failed run loses nothing already written and /project with
// the same name continues it.
func runPasses(ctx context.Context, repo *plantree.Repo, c plan.Completer, client *llm.Client, known int, name, brief string, sub chan tea.Msg) tea.Msg {
	unlock, err := plan.AcquireRun(repo)
	if err != nil {
		return errMsg{err: errors.New(planBusyRefusal(err, name))}
	}
	defer unlock()

	window := windowOf(ctx, client, known)
	opt, opt2 := plan.SizesFor(window)
	opt.ProjectName = name
	sub <- projectProgressMsg(planningSizesLine(window, opt, opt2))
	if w := planningWarning(window); w != "" {
		sub <- projectProgressMsg(w)
	}
	opt.Progress = func(done, total int) {
		sub <- projectProgressMsg(fmt.Sprintf("planning chunk %d of %d", done, total))
	}

	res1, err := plan.RunPass1(ctx, repo, brief, c, opt)
	if err != nil {
		return errMsg{err: err}
	}
	sub <- projectProgressMsg(renderPass1Result(res1))

	waiting, err := plan.NeedsReplan(repo)
	if err != nil {
		return errMsg{err: err}
	}
	opt2.Reconcile = waiting > 0
	opt2.Progress = func(done, total int) {
		sub <- projectProgressMsg(fmt.Sprintf("specifying batch %d of %d", done, total))
	}
	res2, err := plan.RunPass2(ctx, repo, c, opt2)
	if err != nil {
		return errMsg{err: err}
	}
	actions, err := plan.NextActions(repo)
	if err != nil {
		return errMsg{err: err}
	}
	open, err := plan.OpenQuestions(repo)
	if err != nil {
		return errMsg{err: err}
	}
	return projectPassesDoneMsg{res1: res1, res2: res2, actions: actions, open: open}
}

// planBusyRefusal says in plain words why a planning run could not start
// because another run holds the plan, and what to do about it.
func planBusyRefusal(err error, name string) string {
	if errors.Is(err, plan.ErrRunBusy) {
		return "another planning run is working on this plan right now, so nothing was changed. Wait for it to finish (or stop it), then run /project " + name + " again. (" + err.Error() + ")"
	}
	return "the planning run could not start, and nothing was changed: " + err.Error()
}

// planningWarning is the line to show before a run when even the smallest
// sizes cannot fit the model's window, or "" when there is nothing to warn
// about. The run still goes ahead: the estimate is a guess, and the server's
// own error is the authority.
func planningWarning(window int) string {
	if plan.FitsWindow(window) {
		return ""
	}
	return fmt.Sprintf("warning: no setting is safe for a %d token context window, so the run may fail with a context error; it will try the smallest sizes anyway", window)
}

// planningSizesLine says what the passes were sized for, so an overflow is
// diagnosable from the transcript rather than only from the server's error.
func planningSizesLine(window int, o plan.Options, o2 plan.Options2) string {
	where := fmt.Sprintf("a %d token context window", window)
	if window <= 0 {
		where = "an unknown context window, so the safe defaults"
	}
	o, o2 = o.WithDefaults(), o2.WithDefaults()
	return fmt.Sprintf("reading the brief: %s, %d byte chunks, %d byte excerpts, %d step(s) per pass",
		where, o.ChunkBytes, o2.BriefBytes, o2.StepsPerPass)
}

// renderPass1Result is the transcript line for a finished skeleton pass.
func renderPass1Result(r plan.Result) string {
	line := fmt.Sprintf("skeleton: %d phase(s), %d task(s), %d step(s) from %d of %d brief part(s)",
		r.Created.Phases, r.Created.Tasks, r.Created.Steps, r.Processed, r.Chunks)
	if r.Questions > 0 {
		line += fmt.Sprintf(", %d question(s) raised", r.Questions)
	}
	return line + "; specifying every step now"
}

// handleProjectInput routes an input line while a /project flow is active.
// It reports handled=false when the flow is not active, so the caller
// proceeds normally. There is no projRunning case: while the passes run the
// session is not idle, and handleSubmit never routes input here then.
func (m model) handleProjectInput(text string) (model, tea.Cmd, bool) {
	// A slash command always wins over a prompt that is waiting for a plain
	// answer: the approval prompt points the owner at "/questions change",
	// which it would otherwise swallow as a revision request.
	if (m.proj == projAwaitName || m.proj == projApprove) && strings.HasPrefix(text, "/") {
		if m.proj == projApprove {
			m.appendLine("Plan left unapproved. /project " + m.projName + " brings the approval prompt back.")
		}
		m.proj = projNone
		return m, nil, false
	}
	switch m.proj {
	case projAwaitName:
		m.proj = projNone
		name, briefPath, err := parseProjectCommand("/project " + strings.TrimSpace(text))
		if err != nil {
			nm, cmd := m.projectError(err.Error())
			return nm, cmd, true
		}
		if name == "" {
			nm, cmd := m.projectError("a project needs a name")
			return nm, cmd, true
		}
		nm, cmd := m.startProject(name, briefPath)
		return nm, cmd, true

	case projApprove:
		return m.handleProjectApproval(text)
	}
	return m, nil, false
}

// projectError reports a refusal and leaves the flow.
func (m model) projectError(detail string) (model, tea.Cmd) {
	m.appendLine("project: " + detail)
	m.proj = projNone
	m.sync()
	return m, nil
}

var (
	projectBannerStyle = lipgloss.NewStyle().Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#A78BFA"})
	projectDoneStyle = lipgloss.NewStyle().Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#059669", Dark: "#34D399"})
	projectDialogStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#A78BFA"}).
				Padding(0, 1)
)

// projectDialogText is the instruction shown in the /project dialog panel.
func projectDialogText(p projPhase, name string) string {
	switch p {
	case projAwaitName:
		return "new project · type a name and the path to its brief"
	case projRunning:
		return name + " · planning · esc or ctrl-c to stop, /project " + name + " resumes"
	case projApprove:
		return name + " · review · y to approve · revise · cancel"
	}
	return ""
}
