package phaseflow

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// starterRoadmap is the skeleton ROADMAP.md written at init. It is intentionally
// minimal: the `phase roadmap` step (agentic) fills in real phases. The
// structure matches the upstream roadmap template so the parser and both tools
// read it identically.
func starterRoadmap(project string) string {
	return fmt.Sprintf(`# Roadmap: %s

## Overview

[One paragraph describing the journey from start to finish. Populate with
"phase roadmap".]

## Phases

- [ ] **Phase 1: [Name]** - [One-line description]

## Phase Details

### Phase 1: [Name]
**Goal**: [What this phase delivers]
**Depends on**: Nothing (first phase)
**Success Criteria** (what must be TRUE):
  1. [Observable behavior from the user's perspective]

Plans:
- [ ] 01-01: [Brief description of first plan]

## Progress

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. [Name] | 0/1 | Not started | - |
`, project)
}

// starterState is the skeleton STATE.md — the project's living memory.
func starterState(project string) string {
	today := time.Now().Format("2006-01-02")
	return fmt.Sprintf(`# Project State

## Project Reference

See: .planning/PROJECT.md (updated %s)

**Core value:** [One-liner from PROJECT.md]
**Current focus:** Phase 1

## Current Position

Phase: 1 of 1 ([Phase name])
Plan: 0 of 1 in current phase
Status: Ready to plan
Last activity: %s — project initialized

Progress: [░░░░░░░░░░] 0%%

## Accumulated Context

### Decisions

None yet.

### Blockers/Concerns

None yet.

## Session Continuity

Last session: %s
Stopped at: project initialized
Resume file: None
`, today, today, today)
}

// starterProject is the skeleton PROJECT.md.
func starterProject(project string) string {
	return fmt.Sprintf(`# Project: %s

## Core Value

[The ONE thing this project must deliver.]

## Requirements

- [ ] REQ-01: [Requirement]

## Constraints

- [Technical, time, or scope constraints]

## Key Decisions

| Date | Decision | Rationale |
|------|----------|-----------|
| - | - | - |
`, project)
}

// Init scaffolds .planning/ for a new project: config.json, ROADMAP.md,
// STATE.md, PROJECT.md, and the phases/ directory. It refuses to overwrite an
// existing ROADMAP.md so re-running init never clobbers real work.
func (e *Engine) Init(project string) error {
	project = strings.TrimSpace(project)
	if project == "" {
		return fmt.Errorf("project name is required")
	}
	if _, err := os.Stat(RoadmapPath(e.Root)); err == nil {
		return fmt.Errorf("%s already exists — project is already initialized", RoadmapPath(e.Root))
	}
	if err := os.MkdirAll(PhasesDir(e.Root), 0o755); err != nil {
		return err
	}
	if err := DefaultConfig().Save(e.Root); err != nil {
		return err
	}
	files := map[string]string{
		RoadmapPath(e.Root): starterRoadmap(project),
		StatePath(e.Root):   starterState(project),
		ProjectPath(e.Root): starterProject(project),
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}
