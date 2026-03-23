package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"forge/internal/tools"
)

type Session struct {
	RootDir       string   `json:"root_dir"`
	Model         string   `json:"model"`
	UserGoal      string   `json:"user_goal,omitempty"`
	RepoSummary   string   `json:"repo_summary,omitempty"`
	SelectedFiles []string `json:"selected_files,omitempty"`
	LastPlan      string   `json:"last_plan,omitempty"`
	LastDiff      string   `json:"last_diff,omitempty"`
	LastCommand   string   `json:"last_command,omitempty"`
	LastOutput    string   `json:"last_output,omitempty"`
}

type Plan struct {
	Goal        string   `json:"goal"`
	Steps       []string `json:"steps"`
	FilesNeeded []string `json:"files_needed"`
	Notes       []string `json:"notes,omitempty"`
}

type Action struct {
	Type    string `json:"type"`
	Path    string `json:"path,omitempty"`
	Content string `json:"content,omitempty"`
	Find    string `json:"find,omitempty"`
	Replace string `json:"replace,omitempty"`
	Command string `json:"command,omitempty"`
}

type AgentResponse struct {
	Summary string   `json:"summary"`
	Actions []Action `json:"actions"`
}

type EditResult struct {
	Summary       string                `json:"summary"`
	Applied       bool                  `json:"applied"`
	ApprovalMode  string                `json:"approval_mode"`
	ProposedDiffs []string              `json:"proposed_diffs,omitempty"`
	Commands      []tools.CommandResult `json:"commands,omitempty"`
	Errors        []string              `json:"errors,omitempty"`
}

type TestResult struct {
	Summary  string                `json:"summary"`
	Commands []tools.CommandResult `json:"commands"`
}

func (p Plan) Pretty() string {
	b, _ := json.MarshalIndent(p, "", "  ")
	return string(b)
}

func (r EditResult) Pretty() string {
	b, _ := json.MarshalIndent(r, "", "  ")
	return string(b)
}

func (t TestResult) Pretty() string {
	b, _ := json.MarshalIndent(t, "", "  ")
	return string(b)
}

func parsePlan(raw string) (Plan, error) {
	clean := extractJSON(raw)
	var plan Plan
	if err := json.Unmarshal([]byte(clean), &plan); err != nil {
		return Plan{}, fmt.Errorf("parse plan json: %w; raw=%s", err, raw)
	}
	return plan, nil
}

func parseAgentResponse(raw string) (AgentResponse, error) {
	clean := extractJSON(raw)
	var resp AgentResponse
	if err := json.Unmarshal([]byte(clean), &resp); err != nil {
		return AgentResponse{}, fmt.Errorf("parse agent response json: %w; raw=%s", err, raw)
	}
	return resp, nil
}

func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}
