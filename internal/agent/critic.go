package agent

import "strings"

// riskyShellPatterns are substrings in a shell command that warrant a critique
// before the call runs (destructive, remote-exec, or privilege escalation).
var riskyShellPatterns = []string{"rm -rf", "mkfs", ":(){", "dd if=", "| sh", "|sh", "| bash", "curl", "wget", "sudo", "chmod 777"}

// sensitivePathHints mark a write/delete target as sensitive.
var sensitivePathHints = []string{".env", ".git/", "credentials", "id_rsa", ".ssh/", "secrets"}

// critiqueToolCall returns a short warning when a proposed tool call looks risky
// (destructive shell command, or a write/delete to a sensitive path), or "" when
// it looks fine. It is a lightweight heuristic advisory, not a hard block.
func critiqueToolCall(name, args string) string {
	la := strings.ToLower(args)
	switch name {
	case "run_shell":
		for _, p := range riskyShellPatterns {
			if strings.Contains(la, p) {
				return "shell command contains a risky pattern (" + p + ") — review before running"
			}
		}
	case "write_file", "delete_file", "move_file", "apply_patch":
		for _, h := range sensitivePathHints {
			if strings.Contains(la, h) {
				return "operation targets a sensitive path (" + strings.TrimSuffix(h, "/") + ") — review before running"
			}
		}
	}
	return ""
}

// SetToolCritic enables (or disables) the heuristic tool-use critic, which emits
// an advisory warning event before a risky tool call runs.
func (a *Agent) SetToolCritic(on bool) { a.toolCritic = on }
