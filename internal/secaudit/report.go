package secaudit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReportFileName is the default report written into the audited repo.
const ReportFileName = "SECURITY-AUDIT.md"

// disclaimer opens every report. A security scan can show that issues exist; it
// cannot show that none do. Saying so plainly is a correctness requirement, not
// boilerplate: a report that reads as a clean bill of health would be misused.
const disclaimer = "> **This does not prove or guarantee security.** This audit reports issues " +
	"it can detect with static pattern checks and an optional model review. A clean " +
	"run means *nothing was found by these checks* — not that the code is free of " +
	"vulnerabilities. It does not replace dedicated SAST/DAST tools or a human " +
	"security review."

// severityOrder is worst-first for grouping in the report.
var severityOrder = []Severity{Critical, High, Medium, Low, Info}

// Render produces the Markdown report body.
func Render(r *Report) string {
	var b strings.Builder
	b.WriteString("# Security audit\n\n")
	fmt.Fprintf(&b, "Target: `%s`\n\n", r.Target)
	b.WriteString(disclaimer)
	b.WriteString("\n\n")

	counts := r.Counts()
	total := len(r.Findings)

	if total == 0 {
		b.WriteString("## Summary\n\nNo findings from these checks. (See the disclaimer above — this is not a guarantee.)\n")
		return b.String()
	}

	b.WriteString("## Summary\n\n")
	fmt.Fprintf(&b, "%d finding(s):\n\n", total)
	for _, sev := range severityOrder {
		if n := counts[sev]; n > 0 {
			fmt.Fprintf(&b, "- **%s**: %d\n", sev, n)
		}
	}
	b.WriteString("\n")

	// Findings, grouped worst-first; sortFindings already ordered r.Findings.
	fs := append([]Finding(nil), r.Findings...)
	sortFindings(fs)

	b.WriteString("## Findings\n")
	for _, f := range fs {
		conf := "unverified"
		if f.Verified {
			conf = "verified"
		}
		fmt.Fprintf(&b, "\n### [%s] %s\n\n", strings.ToUpper(f.Severity.String()), f.RuleID)
		fmt.Fprintf(&b, "- **Location:** `%s:%d`\n", f.File, f.Line)
		fmt.Fprintf(&b, "- **Category:** %s\n", f.Category)
		fmt.Fprintf(&b, "- **Confidence:** %s", conf)
		if f.VerifyNote != "" {
			fmt.Fprintf(&b, " — %s", f.VerifyNote)
		}
		b.WriteString("\n")
		fmt.Fprintf(&b, "- **Issue:** %s\n", f.Message)
		if f.Snippet != "" {
			fmt.Fprintf(&b, "- **Evidence:** `%s`\n", f.Snippet)
		}
		fmt.Fprintf(&b, "- **Fix:** %s\n", f.Remediation)
	}
	return b.String()
}

// WriteReport renders and writes the report to path via a temp file + rename so
// an interrupted write cannot leave a truncated report.
func WriteReport(r *Report, path string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".secaudit-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.WriteString(Render(r)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return err
	}
	return os.Rename(name, path)
}
