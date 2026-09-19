package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"gophermind/gophermind-lib/phaseflow"
	"gophermind/gophermind-lib/plantree"
	"gophermind/gophermind-lib/plantree/export"
	"gophermind/gophermind-lib/plantree/plan"
)

// This file is the end of the /project flow: the summary the owner approves,
// and what approving does. Both entrances reach it, "/project" when its
// passes leave nothing but approval, and "/questions" when the round it ran
// does, so there is one approval prompt rather than two.

// afterProjectPasses acts on a finished pair of planning passes: it reports
// what they did, then either enters the question round, offers approval, or
// says what is still outstanding.
func (m model) afterProjectPasses(msg projectPassesDoneMsg) (tea.Model, tea.Cmd) {
	m.appendLine(renderQuestionsResult(msg.res2))
	m.st = stateIdle
	m.cancel = nil
	if len(msg.open) > 0 {
		// The round owns the session from here; the flow returns through
		// questionsDoneMsg, which comes back to offerApproval.
		m.proj = projNone
		m.appendLine(fmt.Sprintf("%d question(s) need an answer before this plan can be approved.", len(msg.open)))
		nm, cmd := m.handleQuestionsCommand("/questions")
		return nm, tea.Batch(cmd, m.beginAttention(), waitFor(m.sub))
	}
	nm := m.offerApproval(msg.actions)
	return nm, tea.Batch(nm.beginAttention(), waitFor(m.sub))
}

// offerApproval shows the plan's summary and asks for approval when approval
// is the only thing left, and otherwise says what is outstanding. It is what
// both /project and /questions end at, so the plan is only ever approved
// through one prompt.
func (m model) offerApproval(actions plantree.Actions) model {
	already := false
	if !onlyApprovalIsLeft(actions) {
		// A plan whose steps are all approved offers no approve action, so
		// "nothing left" is ambiguous: it is also what a plan looks like when
		// approving succeeded and the export after it failed. That plan must
		// still reach the prompt, or the owner can never finish the export.
		if !(len(actions.Blocked) == 0 && len(actions.Runnable) == 0 && planIsApproved()) {
			m.appendLine(renderNextActions(actions))
			m.proj = projNone
			m.sync()
			return m
		}
		already = true
	}
	repo, err := planRepo()
	if err != nil {
		m.appendLine("project: " + err.Error())
		m.proj = projNone
		m.sync()
		return m
	}
	name := m.projName
	if name == "" {
		if root, err := repo.Get(plantree.RootID); err == nil {
			name = root.Title
		} else {
			name = "this plan"
		}
	}
	m.projName = name
	if already {
		// Approved and exported: nothing to do, and re-exporting could only
		// replace files a run may already be using.
		if root, err := os.Getwd(); err == nil && phaseflow.New(root).Approved() {
			m.appendLine(projectDoneStyle.Render("This plan is already approved and exported. Run /project-execute to build it."))
			m.proj = projNone
			m.sync()
			return m
		}
		m.appendLine(projectBannerStyle.Render(planSummary(repo, name)))
		m.appendLine("This plan is already approved, but it has not been exported, so /project-execute cannot run yet. Export it now? y to export, or \"cancel\".")
		m.proj = projApprove
		m.sync()
		return m
	}
	m.appendLine(projectBannerStyle.Render(planSummary(repo, name)))
	m.appendLine("Approve this plan? y to approve, \"revise\" to change a decision first, or \"cancel\".")
	m.proj = projApprove
	m.sync()
	return m
}

// planIsApproved reports whether every step of the plan in the working
// directory is already approved and reviewed.
func planIsApproved() bool {
	repo, err := planRepo()
	if err != nil {
		return false
	}
	sum, err := repo.Summarize(plantree.RootID)
	return err == nil && sum.Leaves > 0 && sum.Status == plantree.StatusReviewed
}

// onlyApprovalIsLeft reports whether the plan's one outstanding action is
// approving it.
func onlyApprovalIsLeft(a plantree.Actions) bool {
	return len(a.Blocked) == 0 && len(a.Runnable) == 1 && a.Runnable[0].Kind == plantree.ActionApprove
}

// planSummary is the line the owner approves against: how big the plan is,
// and what the repository facts behind it say.
func planSummary(repo *plantree.Repo, name string) string {
	phases, tasks, steps := 0, 0, 0
	if err := repo.Walk(func(n plantree.Node) error {
		switch n.Kind() {
		case plantree.KindPhase:
			phases++
		case plantree.KindTask:
			tasks++
		case plantree.KindStep:
			steps++
		}
		return nil
	}); err != nil {
		return "plan for " + name + ": " + err.Error()
	}
	line := fmt.Sprintf("Plan for %s: %d phase(s), %d task(s), %d step(s), every step specified.", name, phases, tasks, steps)
	facts, err := plan.ReadFacts(repo)
	if err == nil && strings.TrimSpace(facts) != "" {
		line += "\nRepository facts behind it: " + oneLine(facts)
	} else {
		line += "\nNo repository facts were recorded, so every test command in it came from the brief alone."
	}
	return line
}

// handleProjectApproval processes an approve, revise or cancel input.
//
// Approving marks every step approved and reviewed, exports the legacy
// planning files, and says that /project-execute can run. Revising does NOT
// re-plan from free text: nothing in this milestone turns a sentence into a
// changed plan, and pretending otherwise would be worse than saying so. It
// points at /questions change, which does have a real re-planning path, and
// leaves the plan unapproved.
func (m model) handleProjectApproval(text string) (model, tea.Cmd, bool) {
	kind, revise := parseApproval(text)
	switch kind {
	case approvalCancel:
		m.appendLine("Plan left unapproved. /project " + m.projName + " brings this prompt back.")
		m.proj = projNone
		m.sync()
		return m, nil, true
	case approvalRevise:
		m.appendLine(renderUserPrompt(text))
		if revise != "" {
			m.appendLine("project: free-text revision is not wired to a re-planning pass, so nothing was changed by: " + oneLine(revise))
		}
		m.appendLine("To change the plan, run /questions change: changing an answer flags exactly the steps it invalidated and plans them again. Then /project " + m.projName + " returns here.")
		m.proj = projNone
		m.sync()
		return m, nil, true
	}
	return m.approveAndExport()
}

// approveAndExport is the approval itself: plan.Approve, then
// export.ExportLegacy. Each refusal is reported in plain words, and a failure
// of the export after a successful approval says the project is currently
// unapproved for execution and how to retry.
func (m model) approveAndExport() (model, tea.Cmd, bool) {
	root, err := os.Getwd()
	if err != nil {
		nm, cmd := m.projectError(err.Error())
		return nm, cmd, true
	}
	repo := plantree.Open(phaseflow.PlanningDir(root))
	got, err := plan.Approve(repo)
	if err != nil {
		nm, cmd := m.projectError(approveRefusal(err))
		return nm, cmd, true
	}
	line := fmt.Sprintf("approved: %d step(s) marked reviewed", got.Steps)
	if got.Already > 0 {
		line += fmt.Sprintf(" (%d were already)", got.Already)
	}
	m.appendLine(line)

	rep, err := export.ExportLegacy(repo, root)
	if err != nil {
		// The export removes the approval marker before it writes, so the
		// project is not approved for execution until an export completes.
		nm, cmd := m.projectError(exportRefusal(err) +
			"\nThe steps are approved, but the project is currently unapproved for execution: /project-execute will refuse until an export succeeds. Fix the cause above, then run /project " + m.projName + " and answer y to try again.")
		return nm, cmd, true
	}
	m.appendLine(renderExportReport(rep))
	m.appendLine(projectDoneStyle.Render("Plan approved and exported. Run /project-execute to build it."))
	m.proj = projNone
	m.sync()
	return m, nil, true
}

// approveRefusal says in plain words why plan.Approve refused.
func approveRefusal(err error) string {
	switch {
	case errors.Is(err, plan.ErrRunBusy):
		return "another planning run is working on this plan right now, so nothing was approved. Wait for it to finish (or stop it), then try again. (" + err.Error() + ")"
	case errors.Is(err, plan.ErrNotApprovable):
		return "the plan cannot be approved yet, and nothing was changed: " + err.Error()
	}
	return "the plan was not approved: " + err.Error()
}

// exportRefusal says in plain words why export.ExportLegacy refused.
func exportRefusal(err error) string {
	switch {
	case errors.Is(err, export.ErrNotApproved):
		return "export refused because the plan is not approved: " + err.Error()
	case errors.Is(err, export.ErrNoAgent):
		return "export refused because the agent catalog has no " + export.DefaultAgent + " agent to run the tasks: " + err.Error()
	case errors.Is(err, export.ErrExecutionStarted):
		return "export refused because execution of the existing plan has already started, and replacing it would discard its record: " + err.Error()
	case errors.Is(err, export.ErrNotValid):
		return "export refused because the plan it generated failed validation: " + err.Error()
	case errors.Is(err, plan.ErrRunBusy):
		return "export refused because another planning run is working on this plan right now: " + err.Error()
	}
	return "export failed: " + err.Error()
}

// renderExportReport is the transcript summary of a finished export.
func renderExportReport(r export.Report) string {
	line := fmt.Sprintf("exported %d phase(s) and %d task(s) covering %d step(s) to %s",
		r.Phases, r.Tasks, r.Steps, phaseflow.PlanningDirName)
	if r.SeededAgents > 0 {
		line += fmt.Sprintf(", and seeded %d agent(s) into the catalog", r.SeededAgents)
	}
	line += "\nevery task runs on agent " + export.DefaultAgent + ", model " + export.DefaultModel +
		", with no dependencies, so they run one at a time"
	for _, p := range r.Paths {
		line += "\n  wrote " + p
	}
	if len(r.Replaced) > 0 {
		line += fmt.Sprintf("\nreplaced %d file(s) that already existed:", len(r.Replaced))
		for _, p := range r.Replaced {
			line += "\n  replaced " + p
		}
	}
	return line
}
