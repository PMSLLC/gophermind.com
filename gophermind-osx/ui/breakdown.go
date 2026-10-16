package ui

import "fmt"

// BreakdownSeedPrompt builds the first message that starts a "New Project"
// flow (.planning/tasks/04-06.json: "pick brief -> seed session ->
// interview -> plan"): given the project name and a brief file's raw
// content, it asks the agent to interview the user one question at a time
// and then write the plan into three files under .planning/ (SPEC.md,
// ROADMAP.md, assignments.json). Since the unified pipeline the server's
// pipeline endpoints no longer read these files: they show the run folder of
// `gophermind brief` (docs/pipeline-contract.md). The seed is kept as the
// "Start Breakdown" interview, and its output is a plain planning aid.
//
// This is a standalone prompt: the TUI's old interview prompts (the v1
// planner's) were removed with that planner, so nothing is shared with the
// TUI.
func BreakdownSeedPrompt(projectName, briefContent string) string {
	return fmt.Sprintf(`You are scoping a new software project called %q for a spec-driven workflow.

Here is the brief the user provided:

%s

Ask exactly one question at a time -- the single most useful thing you
still need to know -- until you have enough to write a complete plan.
Do not ask more than one question per turn.

Once you have enough, write the complete project plan into the
.planning/ directory using your file tools:

1. SPEC.md -- a comprehensive spec: overview, goals, users, scope,
   non-goals, constraints, requirements, and measurable success criteria.
2. ROADMAP.md -- phases and plans. Every plan id has the form NN-MM
   (e.g. 01-01).
3. assignments.json -- exactly one entry per ROADMAP plan id, in this
   JSON shape:
   {"tasks":[{"id":"01-01","phase":"1","title":"...","description":"...","acceptance_criteria":["..."],"agent":"executor","model":"strong","status":"pending","depends_on":[]}]}

depends_on lists the ids of tasks that must finish before this one starts.
Tasks that do not depend on each other should be able to run concurrently;
do not chain every task to the previous one unless the work is genuinely
sequential.
`, projectName, briefContent)
}
