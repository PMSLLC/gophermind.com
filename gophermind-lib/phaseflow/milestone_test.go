package phaseflow

import (
	"os"
	"strings"
	"testing"
)

// completedRoadmap is a roadmap whose every phase and plan is done.
const completedRoadmap = `# Roadmap: Ship It

## Phases

- [x] **Phase 1: Foundation** - base
- [x] **Phase 2: Features** - the goods

## Phase Details

### Phase 1: Foundation
**Goal**: A working skeleton

Plans:
- [x] 01-01: scaffold

### Phase 2: Features
**Goal**: Users can do things

Plans:
- [x] 02-01: feature A
`

func TestArchiveMilestone(t *testing.T) {
	root := t.TempDir()
	e := New(root)
	if err := e.Init("Ship It"); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(RoadmapPath(root), []byte(completedRoadmap), 0o644)

	summary, err := e.ArchiveMilestone("v1.0", "MVP")
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if !strings.Contains(summary, "v1.0") {
		t.Errorf("summary should name the version: %q", summary)
	}
	// Roadmap snapshot exists.
	if _, err := os.Stat(MilestonesDir(root) + "/v1.0-ROADMAP.md"); err != nil {
		t.Errorf("roadmap snapshot missing: %v", err)
	}
	// Ledger records the milestone with stats and accomplishments.
	ledger, err := os.ReadFile(MilestonesPath(root))
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	l := string(ledger)
	for _, want := range []string{"v1.0 — MVP", "2 phases, 2/2 plans", "A working skeleton"} {
		if !strings.Contains(l, want) {
			t.Errorf("ledger missing %q:\n%s", want, l)
		}
	}
}

func TestArchiveMilestoneRefusesIncomplete(t *testing.T) {
	root := writeSampleProject(t) // sampleRoadmap has incomplete phases
	if _, err := New(root).ArchiveMilestone("v1.0", ""); err == nil {
		t.Error("archiving with incomplete phases should error")
	}
}

func TestArchiveMilestoneRequiresVersion(t *testing.T) {
	root := t.TempDir()
	_ = New(root).Init("X")
	if _, err := New(root).ArchiveMilestone("  ", ""); err == nil {
		t.Error("missing version should error")
	}
}

func TestArchiveMilestoneRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	e := New(root)
	_ = e.Init("Ship It")
	_ = os.WriteFile(RoadmapPath(root), []byte(completedRoadmap), 0o644)

	// Each of these would escape .planning/milestones/ if interpolated raw.
	for _, bad := range []string{
		"../../evil",
		"../../../../tmp/pwned",
		"..",
		"a/b",
		`a\b`,
		"foo/../bar",
	} {
		if _, err := e.ArchiveMilestone(bad, ""); err == nil {
			t.Errorf("version %q should be rejected as unsafe", bad)
		}
	}
	// Nothing should have been written outside the milestones dir.
	if _, err := os.Stat(root + "/evil-ROADMAP.md"); err == nil {
		t.Fatal("a traversal write escaped the milestones directory")
	}
	// A normal version still works.
	if _, err := e.ArchiveMilestone("v1.0", "MVP"); err != nil {
		t.Errorf("valid version rejected: %v", err)
	}
}

func TestArchiveMilestoneLedgerAppends(t *testing.T) {
	root := t.TempDir()
	e := New(root)
	_ = e.Init("Ship It")
	_ = os.WriteFile(RoadmapPath(root), []byte(completedRoadmap), 0o644)
	if _, err := e.ArchiveMilestone("v1.0", "MVP"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ArchiveMilestone("v1.1", "Next"); err != nil {
		t.Fatal(err)
	}
	ledger, _ := os.ReadFile(MilestonesPath(root))
	l := string(ledger)
	if !strings.Contains(l, "v1.0 — MVP") || !strings.Contains(l, "v1.1 — Next") {
		t.Errorf("ledger should retain both milestones:\n%s", l)
	}
	if strings.Count(l, "# Milestones") != 1 {
		t.Errorf("ledger header should appear exactly once:\n%s", l)
	}
}
