package phaseflow

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// reMilestoneVersion constrains a milestone version to a safe, single path
// segment. It is deliberately strict: the version is interpolated into a
// filename (<version>-ROADMAP.md), so anything containing a path separator or a
// ".." sequence is rejected before it can escape the milestones directory.
var reMilestoneVersion = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// This file is the Go port of the deterministic file bookkeeping in upstream
// PhaseFlow's milestone.cjs cmdMilestoneComplete: archiving the shipped
// roadmap, tallying what was delivered, and appending to the milestone ledger.
// The judgement-heavy parts of shipping (writing the narrative summary, cutting
// a PR) remain the agent's job; the archiving is pure and lives here.

// MilestonesPath returns the path to the milestone ledger, .planning/MILESTONES.md.
func MilestonesPath(root string) string {
	return filepath.Join(PlanningDir(root), "MILESTONES.md")
}

// ArchiveMilestone records a shipped milestone: it snapshots the current
// ROADMAP.md into .planning/milestones/<version>-ROADMAP.md, appends a dated
// entry (with delivery stats) to .planning/MILESTONES.md, and refreshes
// STATE.md. It requires every phase to be complete so a milestone is only ever
// stamped over finished work; name defaults to version when empty.
//
// It returns a short human-readable summary of what was archived.
func (e *Engine) ArchiveMilestone(version, name string) (string, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return "", fmt.Errorf("a version is required (e.g. v1.0)")
	}
	// The version becomes part of a filename, so reject anything that could
	// traverse out of the milestones directory (path separators, "..", etc.).
	if !reMilestoneVersion.MatchString(version) || strings.Contains(version, "..") {
		return "", fmt.Errorf("invalid version %q: use only letters, digits, '.', '_' and '-' (no path separators)", version)
	}
	if name = strings.TrimSpace(name); name == "" {
		name = version
	}
	rm, err := LoadRoadmap(e.Root)
	if err != nil {
		return "", err
	}
	if next := rm.NextPhase(); next != nil {
		return "", fmt.Errorf("phase %s (%s) is not complete — finish all phases before shipping %s",
			next.Number, next.Name, version)
	}
	if len(rm.Phases) == 0 {
		return "", fmt.Errorf("roadmap has no phases to ship")
	}

	if err := os.MkdirAll(MilestonesDir(e.Root), 0o755); err != nil {
		return "", err
	}

	// Snapshot the shipped roadmap.
	roadmap, err := os.ReadFile(RoadmapPath(e.Root))
	if err != nil {
		return "", err
	}
	archivePath := filepath.Join(MilestonesDir(e.Root), version+"-ROADMAP.md")
	if err := os.WriteFile(archivePath, roadmap, 0o644); err != nil {
		return "", err
	}

	// Append a dated, stat-bearing entry to the milestone ledger.
	done, total := rm.TotalPlans()
	entry := renderMilestoneEntry(rm, version, name, done, total)
	if err := appendToLedger(MilestonesPath(e.Root), entry); err != nil {
		return "", err
	}

	if err := e.SyncState("shipped " + version + " (" + name + ")"); err != nil {
		return "", err
	}
	return fmt.Sprintf("archived %s (%s): %d phases, %d/%d plans → %s",
		version, name, len(rm.Phases), done, total, archivePath), nil
}

// renderMilestoneEntry builds the MILESTONES.md section for a shipped milestone,
// listing each phase's goal as an accomplishment.
func renderMilestoneEntry(rm *Roadmap, version, name string, done, total int) string {
	today := time.Now().Format("2006-01-02")
	var b strings.Builder
	fmt.Fprintf(&b, "## %s — %s\n\n", version, name)
	fmt.Fprintf(&b, "**Shipped:** %s  \n", today)
	fmt.Fprintf(&b, "**Delivered:** %d phases, %d/%d plans\n\n", len(rm.Phases), done, total)
	b.WriteString("**Accomplishments:**\n\n")
	for i := range rm.Phases {
		p := &rm.Phases[i]
		line := p.Goal
		if line == "" {
			line = p.Description
		}
		if line == "" {
			line = p.Name
		}
		fmt.Fprintf(&b, "- Phase %s (%s): %s\n", p.Number, p.Name, line)
	}
	b.WriteString("\n")
	return b.String()
}

// appendToLedger appends entry to the milestone ledger, creating it with a
// header the first time. Newest entries are appended after the header so the
// file reads as an append-only changelog of shipped milestones.
func appendToLedger(path, entry string) error {
	const header = "# Milestones\n\nShipped milestones, most recent last.\n\n"
	existing, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		return os.WriteFile(path, []byte(header+entry), 0o644)
	}
	body := string(existing)
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return os.WriteFile(path, []byte(body+entry), 0o644)
}
