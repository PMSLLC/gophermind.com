package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gophermind/internal/llm"
)

// RepoMap builds a compact file/symbol map of the repository.
func RepoMap(root string) (string, error) {
	// List files up to a reasonable depth.
	cmd := exec.Command("find", root, "-type", "f", "-not", "-path", "*/.git/*", "-not", "-path", "*/node_modules/*", "-not", "-path", "*/vendor/*")
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("find: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		return "(no files)", nil
	}
	// Truncate to first 200 files for the map.
	maxFiles := 200
	if len(lines) > maxFiles {
		lines = lines[:maxFiles]
	}
	var b strings.Builder
	for _, line := range lines {
		rel, err := filepath.Rel(root, line)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "  %s\n", rel)
	}
	return b.String(), nil
}

// GitContext returns git-aware context: branch, status, recent diff.
func GitContext(root string) (string, error) {
	var b strings.Builder

	// Current branch.
	branchCmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	branchCmd.Dir = root
	branchOut, err := branchCmd.Output()
	if err == nil {
		fmt.Fprintf(&b, "branch: %s\n", strings.TrimSpace(string(branchOut)))
	}

	// Status summary.
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = root
	statusOut, err := statusCmd.Output()
	if err == nil {
		status := strings.TrimSpace(string(statusOut))
		if status != "" {
			lines := strings.Split(status, "\n")
			fmt.Fprintf(&b, "modified: %d files\n", len(lines))
			for _, line := range lines {
				fmt.Fprintf(&b, "  %s\n", line)
			}
		} else {
			fmt.Fprintf(&b, "status: clean\n")
		}
	}

	// Recent commits.
	logCmd := exec.Command("git", "log", "--oneline", "-5")
	logCmd.Dir = root
	logOut, err := logCmd.Output()
	if err == nil {
		fmt.Fprintf(&b, "recent commits:\n")
		fmt.Fprintf(&b, "%s", string(logOut))
	}

	return b.String(), nil
}

// LoadCLAUDEMD auto-loads CLAUDE.md or AGENTS.md from the repository root.
func LoadCLAUDEMD(root string) string {
	for _, name := range []string{"CLAUDE.md", "AGENTS.md", ".cursorrules"} {
		path := filepath.Join(root, name)
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			return string(data)
		}
	}
	return ""
}

// ContextBudget tracks token usage by category.
type ContextBudget struct {
	SystemTokens     int
	HistoryTokens    int
	ToolOutputTokens int
}

// EstimateBudget estimates token usage by category.
func EstimateBudget(msgs []llm.Message, toolResults []string) ContextBudget {
	budget := ContextBudget{}
	for _, m := range msgs {
		tokens := estimateMessageTokens(m)
		switch m.Role {
		case "system":
			budget.SystemTokens += tokens
		case "user", "assistant":
			budget.HistoryTokens += tokens
		case "tool":
			budget.ToolOutputTokens += tokens
		}
	}
	return budget
}

// ContextDashboard returns a formatted string showing token usage by category.
func ContextDashboard(budget ContextBudget, total int) string {
	return fmt.Sprintf("context budget: system=%d history=%d tool_output=%d total=%d",
		budget.SystemTokens, budget.HistoryTokens, budget.ToolOutputTokens, total)
}

// SelectivePruning removes specific turns from the conversation.
func SelectivePruning(msgs []llm.Message, indices []int) []llm.Message {
	keep := make(map[int]bool)
	for _, idx := range indices {
		keep[idx] = true
	}
	result := make([]llm.Message, 0, len(msgs))
	for i, m := range msgs {
		if !keep[i] {
			result = append(result, m)
		}
	}
	return result
}

// InstructionPriority handles precedence of instructions.
// Priority: user > project files (CLAUDE.md) > system prompt.
func InstructionPriority(systemPrompt, projectFile, userInstruction string) string {
	// User instructions take highest priority.
	if userInstruction != "" {
		return userInstruction
	}
	// Project files are next.
	if projectFile != "" {
		return projectFile
	}
	// System prompt is the fallback.
	return systemPrompt
}

// estimateMessageTokens roughly estimates a single message's token count.
func estimateMessageTokens(m llm.Message) int {
	return llm.EstimateMessagesTokens([]llm.Message{m})
}
