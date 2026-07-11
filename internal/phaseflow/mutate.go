package phaseflow

import (
	"fmt"
	"os"
	"strings"
)

// ROADMAP.md is hand-authored prose, so completing a plan edits its checkbox in
// place rather than parsing the file and re-rendering it — round-tripping would
// silently reflow the author's formatting, comments, and section ordering. Every
// mutation here is therefore surgical: it touches exactly the one marker it must.

// setCheckbox flips the "[ ]"/"[x]" marker on the first line for which match
// reports true, leaving the rest of that line — and the whole rest of the file —
// byte-for-byte intact. The returned bool reports whether such a line was found
// (not whether its value differed), so callers can turn a miss into a clear
// "no such plan/phase" error instead of silently writing nothing.
func setCheckbox(content string, done bool, match func(line string) bool) (string, bool) {
	want := "[ ]"
	if done {
		want = "[x]"
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if !match(line) {
			continue
		}
		for _, marker := range []string{"[ ]", "[x]", "[X]"} {
			if idx := strings.Index(line, marker); idx >= 0 {
				lines[i] = line[:idx] + want + line[idx+len(marker):]
				return strings.Join(lines, "\n"), true
			}
		}
	}
	return content, false
}

// SetPlanDone toggles the checkbox for plan planID in ROADMAP.md content.
func SetPlanDone(content, planID string, done bool) (string, bool) {
	return setCheckbox(content, done, func(line string) bool {
		m := rePlan.FindStringSubmatch(line)
		return m != nil && m[2] == planID
	})
}

// SetPhaseDone toggles the summary checkbox for the phase in ROADMAP.md content.
func SetPhaseDone(content string, num PhaseNumber, done bool) (string, bool) {
	return setCheckbox(content, done, func(line string) bool {
		m := rePhaseSummary.FindStringSubmatch(line)
		if m == nil {
			return false
		}
		n, err := ParsePhaseNumber(m[2])
		return err == nil && n == num
	})
}

// updateRoadmapFile applies fn to the ROADMAP.md content and writes it back only
// when fn reports a change. It returns an error if the file is missing, fn made
// no change (so callers can surface "plan not found"), or the write fails.
func updateRoadmapFile(root string, fn func(content string) (string, bool)) error {
	path := RoadmapPath(root)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated, changed := fn(string(data))
	if !changed {
		return errNoChange
	}
	return os.WriteFile(path, []byte(updated), 0o644)
}

// errNoChange signals that a mutation matched nothing in the roadmap.
var errNoChange = fmt.Errorf("no matching entry in roadmap")

// MarkPlan sets a plan's done state in ROADMAP.md on disk.
func MarkPlan(root, planID string, done bool) error {
	err := updateRoadmapFile(root, func(c string) (string, bool) {
		return SetPlanDone(c, planID, done)
	})
	if err == errNoChange {
		return fmt.Errorf("plan %q not found in roadmap", planID)
	}
	return err
}

// MarkPhase sets a phase's summary done state in ROADMAP.md on disk.
func MarkPhase(root string, num PhaseNumber, done bool) error {
	err := updateRoadmapFile(root, func(c string) (string, bool) {
		return SetPhaseDone(c, num, done)
	})
	if err == errNoChange {
		return fmt.Errorf("phase %s not found in roadmap", num)
	}
	return err
}
