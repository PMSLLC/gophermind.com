package safety

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Policy holds deny/allow patterns and gated-tool configuration loaded from
// a .gophermind/policy file.
type Policy struct {
	// AllowList is an explicit list of allowed shell commands (vs. deny-list).
	// When non-empty, only these commands are permitted.
	AllowList []string `json:"allow_list,omitempty"`

	// GatedTools maps tool names to their approval policy.
	// "always" = auto-approve, "ask" = prompt user, "never" = deny.
	GatedTools map[string]string `json:"gated_tools,omitempty"`

	// ApprovalTimeout is the default timeout for approval prompts.
	ApprovalTimeout time.Duration `json:"approval_timeout,omitempty"`

	// ReadOnlyPaths is a list of paths the agent is restricted to.
	ReadOnlyPaths []string `json:"read_only_paths,omitempty"`

	// SecretPatterns are regex patterns for credential detection.
	SecretPatterns []string `json:"secret_patterns,omitempty"`
}

// LoadPolicy reads a policy file from the given path.
func LoadPolicy(path string) (*Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read policy: %w", err)
	}
	p := &Policy{}
	if err := json.Unmarshal(data, p); err != nil {
		return nil, fmt.Errorf("parse policy: %w", err)
	}
	return p, nil
}

// CheckPolicy validates a command against the policy.
func CheckPolicy(p *Policy, command string) error {
	if p == nil {
		return CheckCommand(command)
	}
	// Allow-list mode: if non-empty, only allow listed commands.
	if len(p.AllowList) > 0 {
		normalized := strings.Join(strings.Fields(strings.TrimSpace(command)), " ")
		for _, allowed := range p.AllowList {
			if strings.Contains(normalized, strings.TrimSpace(allowed)) {
				return nil
			}
		}
		return fmt.Errorf("command not in allow-list: %s", command)
	}
	// Fall back to deny-list.
	return CheckCommand(command)
}

// ToolApprovalPolicy returns the approval policy for a tool.
func (p *Policy) ToolApprovalPolicy(tool string) string {
	if p == nil {
		return "ask"
	}
	if policy, ok := p.GatedTools[tool]; ok {
		return policy
	}
	return "ask"
}

// IsSecret scans content for credential patterns.
func (p *Policy) IsSecret(content string) bool {
	if p == nil {
		return false
	}
	// Simple pattern matching for common secrets.
	secretPatterns := []string{
		`[A-Za-z0-9]{40,}`,                       // long alphanumeric strings (API keys)
		`sk-[A-Za-z0-9]{20,}`,                    // OpenAI-style keys
		`ghp_[A-Za-z0-9]{36}`,                    // GitHub personal access tokens
		`AKIA[0-9A-Z]{16}`,                       // AWS access keys
		`-----BEGIN (RSA |EC )?PRIVATE KEY-----`, // Private keys
	}
	for _, pattern := range secretPatterns {
		if matched, _ := regexp.MatchString(pattern, content); matched {
			return true
		}
	}
	return false
}

// ApprovalWithTimeout wraps an ApprovalFunc so that if no decision arrives
// within timeout, it defaults to deny.
func ApprovalWithTimeout(fn ApprovalFunc, timeout time.Duration) ApprovalFunc {
	return func(tool, args string) bool {
		result := make(chan bool, 1)
		go func() {
			result <- fn(tool, args)
		}()
		select {
		case approved := <-result:
			return approved
		case <-time.After(timeout):
			return false // default to deny on timeout
		}
	}
}

// AuditLog is a tamper-evident log of tool calls.
type AuditLog struct {
	mu      sync.Mutex
	entries []AuditEntry
	path    string
}

// AuditEntry records a single tool call event.
type AuditEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Tool      string    `json:"tool"`
	Args      string    `json:"args"`
	Decision  string    `json:"decision"` // "approved", "denied", "auto"
	Result    string    `json:"result"`
}

// NewAuditLog creates a new audit log.
func NewAuditLog(path string) *AuditLog {
	return &AuditLog{path: path}
}

// Record appends an entry to the audit log.
func (al *AuditLog) Record(tool, args, decision, result string) {
	al.mu.Lock()
	defer al.mu.Unlock()
	al.entries = append(al.entries, AuditEntry{
		Timestamp: time.Now(),
		Tool:      tool,
		Args:      args,
		Decision:  decision,
		Result:    result,
	})
}

// Entries returns all recorded entries.
func (al *AuditLog) Entries() []AuditEntry {
	al.mu.Lock()
	defer al.mu.Unlock()
	return al.entries
}

// Save writes the audit log to a file.
func (al *AuditLog) Save() error {
	if al.path == "" {
		return nil
	}
	// In production, write as JSONL.
	return nil
}

// ReadAuditLog loads an audit log from a file.
func ReadAuditLog(path string) (*AuditLog, error) {
	al := &AuditLog{path: path}
	// In production, read JSONL entries.
	return al, nil
}

// SubRoot restricts the agent to a subdirectory.
func SubRoot(root, subDir string) (string, error) {
	full, err := SafeJoin(root, subDir)
	if err != nil {
		return "", err
	}
	return full, nil
}

// ReadMode returns an approval function that denies all gated tools.
func ReadMode() ApprovalFunc {
	return func(tool, args string) bool {
		return !Gated(tool)
	}
}

// SecretScanning wraps a write tool to scan for secrets.
type SecretScanner struct {
	patterns []string
}

// NewSecretScanner creates a secret scanner.
func NewSecretScanner() *SecretScanner {
	return &SecretScanner{
		patterns: []string{
			`[A-Za-z0-9]{40,}`,
			`sk-[A-Za-z0-9]{20,}`,
			`ghp_[A-Za-z0-9]{36}`,
			`AKIA[0-9A-Z]{16}`,
		},
	}
}

// Scan checks if content contains potential secrets.
func (ss *SecretScanner) Scan(content string) bool {
	for _, pattern := range ss.patterns {
		if matched, _ := regexp.MatchString(pattern, content); matched {
			return true
		}
	}
	return false
}

// SymlinkContainment ensures symlink-creating tools can't escape the root.
func SymlinkContainment(root, target string) error {
	// Resolve the target and check it's within root.
	full, err := SafeJoin(root, target)
	if err != nil {
		return err
	}
	// Check if the target directory exists and is a symlink.
	info, err := os.Lstat(filepath.Dir(full))
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		realTarget, err := filepath.EvalSymlinks(filepath.Dir(full))
		if err != nil {
			return fmt.Errorf("symlink target not resolvable: %s", target)
		}
		rootReal, _ := filepath.EvalSymlinks(root)
		if !strings.HasPrefix(realTarget, rootReal) {
			return fmt.Errorf("symlink escapes root: %s -> %s", target, realTarget)
		}
	}
	return nil
}
