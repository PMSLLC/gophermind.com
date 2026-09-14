package orchestrate

import (
	"context"
	"fmt"

	"gophermind/gophermind-lib/agent"
	"gophermind/gophermind-lib/llm"
	"gophermind/gophermind-lib/phaseflow"
	"gophermind/gophermind-lib/safety"
	"gophermind/gophermind-lib/tools"
)

// Runner implements phaseflow.TaskRunner: it runs each task in a FRESH agent
// (a clean conversation, so context never leaks between tasks), on the
// model resolved from the task's tier, with a system prompt assembled from
// the agent catalog, and verifies the result against the task's acceptance
// criteria with one correction round.
//
// Task agents run unattended, which means "no human is available to answer a
// prompt" — NOT "no policy applies". An unattended run is the one nobody is
// watching, so it needs the audit chain and the policy gate more than an
// interactive session does, not less. Callers supply both via WithApproval and
// WithAuditLog; absent an approval policy the runner falls back to safety.Auto
// so unattended execution still cannot block on a prompt.
type Runner struct {
	client      *llm.Client
	reg         *tools.Registry
	root        string
	speedModel  string
	strongModel string
	maxIter     int
	approve     safety.ApprovalFunc
	audit       *safety.AuditLog
}

// Option configures a Runner.
type Option func(*Runner)

// WithApproval sets the approval policy applied to every task agent's tool
// calls. Pass the same composed stack the interactive paths use (policy, RBAC,
// judge) with the interactive prompt replaced by a non-blocking fallback.
func WithApproval(fn safety.ApprovalFunc) Option {
	return func(r *Runner) { r.approve = fn }
}

// WithAuditLog attaches the tamper-evident audit log to every task agent, so
// unattended runs leave the same verifiable chain as interactive ones.
func WithAuditLog(al *safety.AuditLog) Option {
	return func(r *Runner) { r.audit = al }
}

// NewRunner builds a Runner. client and reg are shared across tasks (each
// task still gets its own fresh agent.Agent / conversation via agent.New);
// root is the project root the agent catalog is loaded from.
func NewRunner(client *llm.Client, reg *tools.Registry, root, speedModel, strongModel string, maxIter int, opts ...Option) *Runner {
	r := &Runner{
		client:      client,
		reg:         reg,
		root:        root,
		speedModel:  speedModel,
		strongModel: strongModel,
		maxIter:     maxIter,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// newTaskAgent builds the fresh, isolated agent one task runs in.
//
// It deliberately does NOT call SetApprovalMode("auto"): that setter assigns
// a.approve = safety.Auto, which would silently discard the policy stack passed
// to agent.New. The mode string is only used for display and the config wizard
// (see agent.Snapshot), neither of which applies to an ephemeral task agent.
func (r *Runner) newTaskAgent(model, system string, maxIter int) *agent.Agent {
	approve := r.approve
	if approve == nil {
		approve = safety.Auto
	}
	// Each task agent gets its OWN client. agent.New takes the client by
	// pointer and SetModel mutates it, so sharing one across a wave's
	// concurrent tasks meant the last SetModel won and a task issued its
	// request against a model a sibling had chosen. The attempt history still
	// named the model the task asked for, making it a record of intentions
	// rather than of what actually ran.
	ag := agent.New(r.client.CloneForModel(model), r.reg, maxIter, approve, nil)
	ag.SetSystemPrompt(system)
	if r.audit != nil {
		ag.SetAuditLog(r.audit)
	}
	return ag
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

	ag := r.newTaskAgent(model, system, r.maxIter)

	verify := func(ctx context.Context, task, answer string) (bool, string, error) {
		ok, feedback := ag.Verify(ctx, task, answer)
		return ok, feedback, nil
	}

	status, detail = runWithVerify(ctx, ag.Send, verify, user, t.AcceptanceCriteria)
	return status, detail, nil
}
