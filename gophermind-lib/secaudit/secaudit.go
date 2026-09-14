// Package secaudit is a security bug-hunt for a repository: a deterministic
// static scan plus an optional LLM review that verifies findings and reasons
// about logic flaws. It reports what it finds and is explicit that a clean run
// means "nothing found by these checks", never "provably secure".
package secaudit

import "sort"

// Severity ranks a finding. Ordered so higher constants are worse, which lets
// threshold comparisons ("fail on High or worse") be plain integer checks.
type Severity int

const (
	Info Severity = iota
	Low
	Medium
	High
	Critical
)

// String renders a severity for reports and CLI output.
func (s Severity) String() string {
	switch s {
	case Critical:
		return "critical"
	case High:
		return "high"
	case Medium:
		return "medium"
	case Low:
		return "low"
	default:
		return "info"
	}
}

// ParseSeverity maps a name (case-insensitive) to a Severity. Unknown names
// return Info, false.
func ParseSeverity(name string) (Severity, bool) {
	switch name {
	case "critical", "Critical", "CRITICAL":
		return Critical, true
	case "high", "High", "HIGH":
		return High, true
	case "medium", "Medium", "MEDIUM":
		return Medium, true
	case "low", "Low", "LOW":
		return Low, true
	case "info", "Info", "INFO":
		return Info, true
	}
	return Info, false
}

// Finding is one potential vulnerability at a location.
type Finding struct {
	RuleID      string // stable, CWE-tagged id, e.g. "hardcoded-secret (CWE-798)"
	Severity    Severity
	Category    string // short human category, e.g. "secrets", "injection"
	File        string // repo-relative path
	Line        int    // 1-based
	Snippet     string // the offending line, trimmed
	Message     string // what is wrong
	Remediation string // how to fix it
	// Verified is set by the LLM review: true = confirmed exploitable, false
	// = not yet reviewed OR refuted (see VerifyNote to distinguish).
	Verified   bool
	VerifyNote string
}

// Report is the outcome of an audit.
type Report struct {
	Target   string
	Findings []Finding
}

// Counts returns the number of findings at each severity.
func (r *Report) Counts() map[Severity]int {
	m := map[Severity]int{}
	for _, f := range r.Findings {
		m[f.Severity]++
	}
	return m
}

// Max returns the worst severity present, and false if there are no findings.
func (r *Report) Max() (Severity, bool) {
	if len(r.Findings) == 0 {
		return Info, false
	}
	worst := Info
	for _, f := range r.Findings {
		if f.Severity > worst {
			worst = f.Severity
		}
	}
	return worst, true
}

// MeetsThreshold reports whether any finding is at or above threshold — the
// basis for a non-zero exit that can gate CI.
func (r *Report) MeetsThreshold(threshold Severity) bool {
	for _, f := range r.Findings {
		if f.Severity >= threshold {
			return true
		}
	}
	return false
}

// sortFindings orders findings worst-first, then by file and line, so a report
// is stable and reads top-down by urgency.
func sortFindings(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].Severity != fs[j].Severity {
			return fs[i].Severity > fs[j].Severity
		}
		if fs[i].File != fs[j].File {
			return fs[i].File < fs[j].File
		}
		return fs[i].Line < fs[j].Line
	})
}
