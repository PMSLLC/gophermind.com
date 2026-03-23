package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"forge/internal/config"
	"forge/internal/llm"
	"forge/internal/patch"
	"forge/internal/repo"
	"forge/internal/tools"
)

type Runner struct {
	cfg     config.Config
	llm     *llm.Client
	session Session
}

func NewRunner(cfg config.Config) (*Runner, error) {
	s, err := loadSession(cfg.SessionPath)
	if err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}
	r := &Runner{
		cfg:     cfg,
		llm:     llm.NewClient(cfg.OllamaBaseURL, cfg.Model),
		session: s,
	}
	r.session.RootDir = cfg.RootDir
	r.session.Model = cfg.Model
	return r, nil
}

func (r *Runner) Ask(ctx context.Context, prompt string) (string, error) {
	summary, err := r.ensureRepoSummary(ctx)
	if err != nil {
		return "", err
	}
	tree, err := repo.Tree(r.cfg.RootDir, 300)
	if err != nil {
		return "", err
	}
	messages := []llm.Message{
		{Role: "system", Content: systemAskPrompt},
		{Role: "user", Content: "Repository summary:\n" + summary + "\n\nRepository tree:\n" + tree + "\n\nQuestion:\n" + prompt},
	}
	return r.llm.Chat(ctx, messages, nil)
}

func (r *Runner) RepoSummary(ctx context.Context) (string, error) {
	return r.ensureRepoSummary(ctx)
}

func (r *Runner) Plan(ctx context.Context, prompt string) (Plan, error) {
	summary, err := r.ensureRepoSummary(ctx)
	if err != nil {
		return Plan{}, err
	}
	tree, err := repo.Tree(r.cfg.RootDir, 300)
	if err != nil {
		return Plan{}, err
	}
	messages := []llm.Message{
		{Role: "system", Content: systemPlannerPrompt},
		{Role: "user", Content: "Repository summary:\n" + summary + "\n\nRepository tree:\n" + tree + "\n\nRequested change:\n" + prompt},
	}
	raw, err := r.llm.Chat(ctx, messages, nil)
	if err != nil {
		return Plan{}, err
	}
	plan, err := parsePlan(raw)
	if err != nil {
		return Plan{}, err
	}
	r.session.UserGoal = prompt
	r.session.LastPlan = plan.Pretty()
	if err := saveSession(r.cfg.SessionPath, r.session); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func (r *Runner) Edit(ctx context.Context, prompt string) (EditResult, error) {
	plan, err := r.Plan(ctx, prompt)
	if err != nil {
		return EditResult{}, err
	}
	files := map[string]string{}
	for _, rel := range dedupe(plan.FilesNeeded) {
		content, err := tools.ReadFile(r.cfg.RootDir, rel)
		if err != nil {
			continue
		}
		files[rel] = content
	}

	tree, err := repo.Tree(r.cfg.RootDir, 300)
	if err != nil {
		return EditResult{}, err
	}
	contextText := repo.BuildTaskContext(r.cfg.RootDir, r.session.RepoSummary, tree, files, prompt)

	messages := []llm.Message{
		{Role: "system", Content: systemEditorPrompt},
		{Role: "user", Content: contextText + "\n\nPlan:\n" + plan.Pretty()},
	}
	raw, err := r.llm.Chat(ctx, messages, nil)
	if err != nil {
		return EditResult{}, err
	}
	resp, err := parseAgentResponse(raw)
	if err != nil {
		return EditResult{}, err
	}
	return r.applyActions(ctx, resp)
}

func (r *Runner) Test(ctx context.Context) (TestResult, error) {
	commands := []string{"gofmt -w .", "go test ./..."}
	results := make([]tools.CommandResult, 0, len(commands))
	for _, cmd := range commands {
		result, err := tools.RunCommand(ctx, r.cfg.RootDir, cmd, r.cfg.CommandTimeout)
		if err != nil {
			return TestResult{}, err
		}
		results = append(results, result)
		r.session.LastCommand = cmd
		r.session.LastOutput = strings.TrimSpace(result.Stdout + "\n" + result.Stderr)
	}
	if err := saveSession(r.cfg.SessionPath, r.session); err != nil {
		return TestResult{}, err
	}
	return TestResult{Summary: summarizeTestResults(results), Commands: results}, nil
}

func (r *Runner) Fix(ctx context.Context) (EditResult, error) {
	lastOutput := strings.TrimSpace(r.session.LastOutput)
	if lastOutput == "" {
		testResult, err := r.Test(ctx)
		if err != nil {
			return EditResult{}, err
		}
		lastOutput = testResult.Pretty()
	}

	summary, err := r.ensureRepoSummary(ctx)
	if err != nil {
		return EditResult{}, err
	}
	tree, err := repo.Tree(r.cfg.RootDir, 300)
	if err != nil {
		return EditResult{}, err
	}

	messages := []llm.Message{
		{Role: "system", Content: systemFixPrompt},
		{Role: "user", Content: "Repository summary:\n" + summary + "\n\nRepository tree:\n" + tree + "\n\nRecent failure output:\n" + lastOutput},
	}
	raw, err := r.llm.Chat(ctx, messages, nil)
	if err != nil {
		return EditResult{}, err
	}
	resp, err := parseAgentResponse(raw)
	if err != nil {
		return EditResult{}, err
	}
	return r.applyActions(ctx, resp)
}

func (r *Runner) ensureRepoSummary(ctx context.Context) (string, error) {
	if strings.TrimSpace(r.session.RepoSummary) != "" {
		return r.session.RepoSummary, nil
	}
	summary, err := repo.BuildRepoSummary(ctx, r.cfg.RootDir, r.llm)
	if err != nil {
		return "", err
	}
	r.session.RepoSummary = summary
	if err := saveSession(r.cfg.SessionPath, r.session); err != nil {
		return "", err
	}
	return summary, nil
}

func (r *Runner) applyActions(ctx context.Context, resp AgentResponse) (EditResult, error) {
	result := EditResult{
		Summary:      resp.Summary,
		ApprovalMode: r.cfg.ApprovalMode,
	}

	var errors []string
	var commands []tools.CommandResult
	var diffs []string
	applied := false

	for _, action := range resp.Actions {
		switch action.Type {
		case "write_file":
			before, _ := tools.ReadFile(r.cfg.RootDir, action.Path)
			diffs = append(diffs, patch.UnifiedPreview(action.Path, before, action.Content))
			if r.cfg.ApprovalMode == "auto" {
				if err := patch.ApplyWrite(r.cfg.RootDir, action.Path, action.Content); err != nil {
					errors = append(errors, err.Error())
				} else {
					applied = true
				}
			}
		case "replace_in_file":
			before, err := tools.ReadFile(r.cfg.RootDir, action.Path)
			if err != nil {
				errors = append(errors, err.Error())
				continue
			}
			after := strings.Replace(before, action.Find, action.Replace, 1)
			diffs = append(diffs, patch.UnifiedPreview(action.Path, before, after))
			if r.cfg.ApprovalMode == "auto" {
				if err := patch.ApplyReplace(r.cfg.RootDir, action.Path, action.Find, action.Replace); err != nil {
					errors = append(errors, err.Error())
				} else {
					applied = true
				}
			}
		case "run_shell":
			cmdResult, err := tools.RunCommand(ctx, r.cfg.RootDir, action.Command, r.cfg.CommandTimeout)
			if err != nil {
				errors = append(errors, err.Error())
			} else {
				commands = append(commands, cmdResult)
				r.session.LastCommand = action.Command
				r.session.LastOutput = strings.TrimSpace(cmdResult.Stdout + "\n" + cmdResult.Stderr)
			}
		default:
			errors = append(errors, "unsupported action type: "+action.Type)
		}
	}

	if applied && r.cfg.ApprovalMode == "auto" {
		if fmtResult, err := tools.RunCommand(ctx, r.cfg.RootDir, "gofmt -w .", r.cfg.CommandTimeout); err == nil {
			commands = append(commands, fmtResult)
		}
		if testResult, err := tools.RunCommand(ctx, r.cfg.RootDir, "go test ./...", r.cfg.CommandTimeout); err == nil {
			commands = append(commands, testResult)
			r.session.LastCommand = testResult.Command
			r.session.LastOutput = strings.TrimSpace(testResult.Stdout + "\n" + testResult.Stderr)
		}
	}

	result.Applied = applied
	result.ProposedDiffs = diffs
	result.Commands = commands
	result.Errors = errors

	if len(diffs) > 0 {
		r.session.LastDiff = strings.Join(diffs, "\n\n")
	}
	if err := saveSession(r.cfg.SessionPath, r.session); err != nil {
		return EditResult{}, err
	}
	return result, nil
}

func dedupe(items []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func summarizeTestResults(results []tools.CommandResult) string {
	var failed []string
	for _, r := range results {
		if r.ExitCode != 0 {
			failed = append(failed, r.Command)
		}
	}
	if len(failed) == 0 {
		return "All validation commands passed."
	}
	return "Some validation commands failed: " + strings.Join(failed, ", ")
}
