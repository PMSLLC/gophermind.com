// Package phaseflow is gophermind's native port of PhaseFlow — a spec-driven
// development orchestration workflow: every project runs a Roadmap → Phases →
// Plan → Execute → Verify → Milestone loop, with the workflow state persisted on
// disk under .planning/ so any session can resume where the last one stopped.
//
// The on-disk model mirrors upstream PhaseFlow (github.com/jbrahy/metaphaseflow):
//
//	.planning/
//	  ROADMAP.md    — phases, plans, and the progress table
//	  STATE.md      — the project's living memory (current position, metrics)
//	  PROJECT.md    — core value, requirements, and key decisions
//	  config.json   — workflow configuration and gates
//	  phases/       — per-phase plan and summary artifacts
//	  milestones/   — archived, shipped milestones
//
// This package owns the state model and the command engine; the prompt artifacts
// that drive each step (the phase commands and phase agents) are embedded from
// upstream under assets/ so gophermind can run the loop without an external
// install.
package phaseflow

import "path/filepath"

// Directory and file names under the project root. These match upstream
// PhaseFlow so a .planning/ tree is interchangeable between the two tools.
const (
	// PlanningDirName is the workflow root, relative to the project root.
	PlanningDirName = ".planning"

	roadmapFile = "ROADMAP.md"
	stateFile   = "STATE.md"
	projectFile = "PROJECT.md"
	configFile  = "config.json"

	phasesDirName     = "phases"
	milestonesDirName = "milestones"
)

// PlanningDir returns the .planning directory for a project root.
func PlanningDir(root string) string { return filepath.Join(root, PlanningDirName) }

// RoadmapPath returns the path to ROADMAP.md for a project root.
func RoadmapPath(root string) string { return filepath.Join(PlanningDir(root), roadmapFile) }

// StatePath returns the path to STATE.md for a project root.
func StatePath(root string) string { return filepath.Join(PlanningDir(root), stateFile) }

// ProjectPath returns the path to PROJECT.md for a project root.
func ProjectPath(root string) string { return filepath.Join(PlanningDir(root), projectFile) }

// ConfigPath returns the path to config.json for a project root.
func ConfigPath(root string) string { return filepath.Join(PlanningDir(root), configFile) }

// PhasesDir returns the phases directory for a project root.
func PhasesDir(root string) string { return filepath.Join(PlanningDir(root), phasesDirName) }

// MilestonesDir returns the milestones archive directory for a project root.
func MilestonesDir(root string) string { return filepath.Join(PlanningDir(root), milestonesDirName) }
