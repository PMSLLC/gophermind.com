package orchestrate

import (
	"context"
	"fmt"

	"gophermind/internal/agent"
	"gophermind/internal/llm"
	"gophermind/internal/phaseflow"
	"gophermind/internal/safety"
	"gophermind/internal/tools"
)

// Runner implements phaseflow.TaskRunner: it runs each task in a FRESH agent
// (a clean conversation, so context never leaks between tasks), on the
// model resolved from the task's tier, with a system prompt assembled from
// the agent catalog, and verifies the result against the task's acceptance
// criteria with one correction round. Task agents run in auto approval mode
// (unattended).
type Runner struct {
	client      *llm.Client
	reg         *tools.Registry
	root        string
	speedModel  string
	strongModel string
	maxIter     int
}

// NewRunner builds a Runner. client and reg are shared across tasks (each
// task still gets its own fresh agent.Agent / conversation via agent.New);
// root is the project root the agent catalog is loaded from.
func NewRunner(client *llm.Client, reg *tools.Registry, root, speedModel, strongModel string, maxIter int) *Runner {
	return &Runner{
		client:      client,
		reg:         reg,
		root:        root,
		speedModel:  speedModel,
		strongModel: strongModel,
		maxIter:     maxIter,
	}
}

// Run implements phaseflow.TaskRunner.
func (r *Runner) Run(ctx context.Context, t phaseflow.Task) (status, detail string, err error) {
	if t.Agent == "" {
		return phaseflow.StatusFailed, "no agent assigned", nil
	}

	agents, _, err := phaseflow.LoadCatalog(r.root)
	if err != nil {
		return "", "", fmt.Errorf("orchestrate: load catalog: %w", err)
	}
	var body string
	found := false
	for _, ca := range agents {
		if ca.Name == t.Agent {
			body = ca.Body
			found = true
			break
		}
	}
	if !found {
		return phaseflow.StatusFailed, fmt.Sprintf("catalog agent %q not found", t.Agent), nil
	}

	model := resolveModel(t.Model, r.speedModel, r.strongModel)
	system, user := buildTaskPromptsWithContext(t, body, r.root)

	ag := agent.New(r.client, r.reg, r.maxIter, safety.Auto, nil)
	ag.SetModel(model)
	ag.SetApprovalMode("auto")
	ag.SetSystemPrompt(system)

	verify := func(ctx context.Context, task, answer string) (bool, string, error) {
		ok, feedback := ag.Verify(ctx, task, answer)
		return ok, feedback, nil
	}

	status, detail = runWithVerify(ctx, ag.Send, verify, user, t.AcceptanceCriteria)
	return status, detail, nil
}
