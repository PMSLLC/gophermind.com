package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gophermind/internal/phaseflow"
)

// This file implements the `/project` guided new-project flow: a dialog-driven
// state machine that scaffolds .planning/, interviews the user with the LLM to
// build a comprehensive spec, generates a validated plan (ROADMAP + per-task
// agent/model assignments), and requires approval before the project is marked
// ready. See docs/superpowers/specs/2026-07-13-project-planning-design.md.

// projPhase is the step of the /project flow the model is in.
type projPhase int

const (
	projNone       projPhase = iota
	projAwaitName            // waiting for the project name
	projInterview            // interviewing: waiting for the user's answer
	projGenerating           // an agent turn is drafting/validating the plan
	projReview               // plan generated; waiting for approve/revise
)

// specReadySentinel is what the interviewing agent emits when it has gathered
// enough to write a comprehensive spec.
const specReadySentinel = "[[SPEC-READY]]"

// maxPlanRetries bounds how many times the agent is auto-asked to fix an
// incomplete plan before the flow hands control back to the user.
const maxPlanRetries = 3

// isSpecReady reports whether the agent signaled it is ready to generate.
func isSpecReady(s string) bool { return strings.Contains(s, specReadySentinel) }

// projectApproval classifies a projReview input.
type projectApproval int

const (
	approvalApprove projectApproval = iota
	approvalCancel
	approvalRevise
)

// parseApproval interprets a projReview input as approve, cancel, or a revision
// request (revise carries the requested change text).
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

// interviewPrompt seeds the interview turn.
func interviewPrompt(name string) string {
	return fmt.Sprintf(`You are scoping a new software project called %q for a spec-driven workflow.
Interview me to build a comprehensive spec. Ask focused clarifying questions a FEW at a time —
goals, target users, core features, scope, explicit non-goals, constraints, and measurable success
criteria — and ask follow-ups until you could write a thorough spec. Do NOT write any files yet.
Only when you have enough, end your message with the token %s on its own line.`, name, specReadySentinel)
}

// generationPrompt instructs the agent to write the plan files using the catalog.
func generationPrompt(name string, catalog []phaseflow.CatalogAgent) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Now WRITE the complete project plan for %q into the %s/ directory using your file tools:\n\n",
		name, phaseflow.PlanningDirName)
	b.WriteString("1. SPEC.md — a comprehensive spec: overview, goals, users, scope, non-goals, constraints, requirements, and measurable success criteria.\n")
	b.WriteString("2. ROADMAP.md — phases and plans. Every phase needs a **Goal**, **Success Criteria**, and a Plans list; every plan id has the form NN-MM (e.g. 01-01). Use NO placeholder tokens — no [brackets], no TBD.\n")
	b.WriteString("3. assignments.json — exactly one entry per ROADMAP plan id. JSON shape:\n")
	b.WriteString("   {\"tasks\":[{\"id\":\"01-01\",\"phase\":\"1\",\"title\":\"...\",\"description\":\"...\",\"acceptance_criteria\":[\"...\"],\"agent\":\"<catalog name>\",\"agent_addendum\":\"task-specific guidance\",\"model\":\"speed|strong\",\"status\":\"pending\"}]}\n\n")
	b.WriteString("Agent catalog — assign each task the best-fit agent; the model defaults to the agent's but override to speed/strong when a task is unusually simple or hard:\n")
	for _, a := range catalog {
		fmt.Fprintf(&b, "- %s (default %s): %s\n", a.Name, a.DefaultModel, a.Description)
	}
	b.WriteString("\nEvery task MUST have at least one acceptance criterion, a catalog agent, a model, and status \"pending\". When done, give a one-line summary of the plan.")
	return b.String()
}

// startTurn spawns an agent turn, marking whether it belongs to the /project
// flow. The goroutine posts the result to m.sub, which the always-pending
// waitFor delivers back to Update. It returns the updated model (state set to
// working); callers issue the tea.Cmd.
func (m model) startTurn(sendText string, project bool) model {
	if m.agent == nil {
		return m
	}
	m.st = stateWorking
	m.projTurn = project
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	sub, ag := m.sub, m.agent
	go func() {
		ans, err := ag.Send(ctx, sendText)
		if err != nil {
			sub <- errMsg{err: err}
		} else {
			sub <- doneMsg{answer: ans}
		}
	}()
	m.sync()
	return m
}

// handleProjectCommand dispatches "/project [name]". With a name it scaffolds
// and starts the interview; without one it asks for the name.
func (m model) handleProjectCommand(text string) (model, tea.Cmd) {
	if m.agent == nil {
		m.appendLine("project: no active session")
		m.sync()
		return m, nil
	}
	name := strings.TrimSpace(strings.TrimPrefix(strings.Fields(text)[0], "/project"))
	if fields := strings.Fields(text); len(fields) > 1 {
		name = strings.TrimSpace(strings.Join(fields[1:], " "))
	} else {
		name = ""
	}
	if name == "" {
		m.proj = projAwaitName
		m.appendLine(projectBannerStyle.Render("New project — what should it be called?"))
		m.sync()
		return m, nil
	}
	return m.startProject(name)
}

// startProject scaffolds the project and kicks off the interview.
func (m model) startProject(name string) (model, tea.Cmd) {
	root, err := os.Getwd()
	if err != nil {
		m.appendLine("project: " + err.Error())
		m.proj = projNone
		m.sync()
		return m, nil
	}
	e := phaseflow.New(root)
	if !e.Initialized() {
		if err := e.Init(name); err != nil {
			m.appendLine("project: " + err.Error())
			m.proj = projNone
			m.sync()
			return m, nil
		}
	}
	if _, err := phaseflow.SeedCatalog(root); err != nil {
		m.appendLine("project: seed catalog: " + err.Error())
	}
	m.projName = name
	m.projRetries = 0
	m.appendLine(projectBannerStyle.Render("Scoping “" + name + "” — answer the interview questions; type /generate when ready."))
	m.proj = projInterview
	m.sync()
	return m.startTurn(interviewPrompt(name), true), nil
}

// handleProjectInput routes an input line while a /project flow is active.
// Returns handled=false when the flow is not active so the caller proceeds
// normally.
func (m model) handleProjectInput(text string) (model, tea.Cmd, bool) {
	switch m.proj {
	case projAwaitName:
		nm, _ := m.startProject(strings.TrimSpace(text))
		return nm, nil, true

	case projInterview:
		m.appendLine(renderUserPrompt(text))
		if strings.EqualFold(strings.TrimSpace(text), "/generate") {
			return m.beginGeneration(""), nil, true
		}
		return m.startTurn(text, true), nil, true

	case projGenerating:
		m.appendLine("(still working on the plan…)")
		m.sync()
		return m, nil, true

	case projReview:
		return m.handleProjectReview(text)
	}
	return m, nil, false
}

// beginGeneration starts a plan-generation turn. revise, when non-empty, is a
// requested change appended to the instruction.
func (m model) beginGeneration(revise string) model {
	root, _ := os.Getwd()
	catalog, _, _ := phaseflow.LoadCatalog(root)
	prompt := generationPrompt(m.projName, catalog)
	if revise != "" {
		prompt += "\n\nRevision requested: " + revise
	}
	m.proj = projGenerating
	m.appendLine(projectBannerStyle.Render("Generating the plan…"))
	m.sync()
	return m.startTurn(prompt, true)
}

// handleProjectReview processes an approve/revise/cancel input in projReview.
func (m model) handleProjectReview(text string) (model, tea.Cmd, bool) {
	root, _ := os.Getwd()
	e := phaseflow.New(root)
	kind, revise := parseApproval(text)
	switch kind {
	case approvalApprove:
		if err := e.Approve(); err != nil {
			m.appendLine("project: approve: " + err.Error())
		} else {
			m.appendLine(projectDoneStyle.Render("✓ Project approved — you can now /phase plan 1 or /phase execute 1"))
		}
		m.proj = projNone
		m.sync()
		return m, nil, true
	case approvalCancel:
		m.appendLine("Project setup cancelled (not approved).")
		m.proj = projNone
		m.sync()
		return m, nil, true
	default: // revise
		m.appendLine(renderUserPrompt(text))
		m.projRetries = 0
		return m.beginGeneration(revise), nil, true
	}
}

// afterProjectTurn post-processes a completed /project agent turn. It advances
// the interview, validates a generated plan (auto-fixing up to maxPlanRetries),
// or moves to review. It always re-arms the sub listener.
func (m model) afterProjectTurn(answer string) (tea.Model, tea.Cmd) {
	switch m.proj {
	case projInterview:
		if isSpecReady(answer) {
			return m.beginGeneration(""), waitFor(m.sub)
		}
		// Otherwise the agent asked more questions (already shown); wait for input.
		return m, waitFor(m.sub)

	case projGenerating:
		root, _ := os.Getwd()
		rep, err := phaseflow.New(root).ValidatePlan()
		if err != nil {
			m.appendLine("project: validate: " + err.Error())
			m.proj = projReview // let the user decide
			m.sync()
			return m, waitFor(m.sub)
		}
		if rep.Complete {
			m.appendLine(projectBannerStyle.Render(fmt.Sprintf(
				"Plan ready: %d phases, %d tasks. Approve? (y to approve, or type changes / \"cancel\")",
				rep.Phases, rep.Tasks)))
			m.proj = projReview
			m.sync()
			return m, waitFor(m.sub)
		}
		// Incomplete — auto-ask the agent to fix, bounded.
		if m.projRetries < maxPlanRetries {
			m.projRetries++
			m.appendLine("Plan incomplete — fixing:\n  - " + strings.Join(rep.Issues, "\n  - "))
			fix := "The plan is incomplete. Fix these issues, then rewrite the affected files:\n- " +
				strings.Join(rep.Issues, "\n- ")
			m.sync()
			return m.startTurn(fix, true), waitFor(m.sub)
		}
		m.appendLine("Plan still incomplete after retries:\n  - " + strings.Join(rep.Issues, "\n  - ") +
			"\nType changes to try again, or \"cancel\".")
		m.proj = projReview
		m.sync()
		return m, waitFor(m.sub)
	}
	return m, waitFor(m.sub)
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
		return "🆕 New project · type a name"
	case projInterview:
		return "🆕 " + name + " · interview · answer above, or /generate when ready"
	case projGenerating:
		return "🆕 " + name + " · generating the plan…"
	case projReview:
		return "🆕 " + name + " · review · y to approve · type changes · cancel"
	}
	return ""
}
