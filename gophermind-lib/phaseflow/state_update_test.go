package phaseflow

import (
	"os"
	"strings"
	"testing"
)

// writeSampleProject scaffolds a project and overwrites its roadmap with the
// shared sampleRoadmap fixture, returning the root.
func writeSampleProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	e := New(root)
	if err := e.Init("Demo"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(RoadmapPath(root), []byte(sampleRoadmap), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestDerivePosition(t *testing.T) {
	rm, _ := ParseRoadmap(sampleRoadmap)
	p := derivePosition(rm)
	// Phase 1 done; current is phase 2 (index 2 of 4). Phase 2 has 1/2 plans.
	if p.PhaseIndex != 2 || p.PhaseCount != 4 {
		t.Errorf("phase index/count = %d/%d, want 2/4", p.PhaseIndex, p.PhaseCount)
	}
	if p.PlansDone != 1 || p.PlansTotal != 2 {
		t.Errorf("plans = %d/%d, want 1/2", p.PlansDone, p.PlansTotal)
	}
	if p.Status != "In progress" {
		t.Errorf("status = %q, want In progress", p.Status)
	}
	if p.Percent != 50 {
		t.Errorf("percent = %d, want 50", p.Percent)
	}
}

func TestSyncStateRewritesPosition(t *testing.T) {
	root := writeSampleProject(t)
	if err := New(root).SyncState("completed plan 02-02"); err != nil {
		t.Fatalf("sync: %v", err)
	}
	data, _ := os.ReadFile(StatePath(root))
	s := string(data)
	if !strings.Contains(s, "Phase: 2 of 4 (Features)") {
		t.Errorf("state missing phase line:\n%s", s)
	}
	if !strings.Contains(s, "Plan: 1 of 2 in current phase") {
		t.Errorf("state missing plan line:\n%s", s)
	}
	if !strings.Contains(s, "Status: In progress") {
		t.Errorf("state missing status line:\n%s", s)
	}
	if !strings.Contains(s, "50%") {
		t.Errorf("state missing progress percent:\n%s", s)
	}
	if !strings.Contains(s, "completed plan 02-02") {
		t.Errorf("state missing activity:\n%s", s)
	}
}

func TestCompletePlanMarksAndSyncs(t *testing.T) {
	root := writeSampleProject(t)
	// Completing 02-01 leaves phase 2 with 02-02 already done → phase 2 done.
	if err := New(root).CompletePlan("02-01"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	rm, _ := LoadRoadmap(root)
	p2 := rm.Phase(PhaseNumber{Major: 2})
	if !p2.Done {
		t.Error("phase 2 summary should be ticked once all its plans are done")
	}
	// State should now point at phase 2.1 (next incomplete).
	data, _ := os.ReadFile(StatePath(root))
	if !strings.Contains(string(data), "Phase: 3 of 4 (Hotfix)") {
		t.Errorf("state should advance to phase 2.1 (3rd of 4):\n%s", data)
	}
}

func TestCompletePlanUnknown(t *testing.T) {
	root := writeSampleProject(t)
	if err := New(root).CompletePlan("99-99"); err == nil {
		t.Error("completing an unknown plan should error")
	}
}
