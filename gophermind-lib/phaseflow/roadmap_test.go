package phaseflow

import (
	"strings"
	"testing"
)

const sampleRoadmap = `# Roadmap: Demo Project

## Overview

A demo.

## Phases

- [x] **Phase 1: Foundation** - set up the base
- [ ] **Phase 2: Features** - build the features
- [ ] **Phase 2.1: Hotfix (INSERTED)** - urgent patch
- [ ] **Phase 3: Polish** - final polish

## Phase Details

### Phase 1: Foundation
**Goal**: A working skeleton
**Depends on**: Nothing (first phase)

Plans:
- [x] 01-01: scaffold
- [x] 01-02: config

### Phase 2: Features
**Goal**: Users can do things
**Depends on**: Phase 1

Plans:
- [ ] 02-01: feature A
- [x] 02-02: feature B

### Phase 2.1: Hotfix (INSERTED)
**Goal**: Fix the thing
**Depends on**: Phase 2

Plans:
- [ ] 02.1-01: patch

### Phase 3: Polish
**Goal**: Shiny
**Depends on**: Phase 2

Plans:
- [ ] 03-01: polish
`

func TestParseRoadmap(t *testing.T) {
	rm, err := ParseRoadmap(sampleRoadmap)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if rm.Title != "Demo Project" {
		t.Errorf("title = %q, want Demo Project", rm.Title)
	}
	if len(rm.Phases) != 4 {
		t.Fatalf("got %d phases, want 4", len(rm.Phases))
	}

	p1 := rm.Phase(PhaseNumber{Major: 1})
	if p1 == nil || !p1.Done || p1.Goal != "A working skeleton" || len(p1.Plans) != 2 {
		t.Errorf("phase 1 parsed wrong: %+v", p1)
	}
	if p1.Description != "set up the base" {
		t.Errorf("phase 1 description = %q", p1.Description)
	}

	p21 := rm.Phase(PhaseNumber{Major: 2, Minor: 1})
	if p21 == nil {
		t.Fatal("phase 2.1 missing")
	}
	if !p21.Inserted {
		t.Error("phase 2.1 should be marked inserted")
	}
	if p21.Name != "Hotfix" {
		t.Errorf("phase 2.1 name = %q, want Hotfix (INSERTED stripped)", p21.Name)
	}
}

func TestProgress(t *testing.T) {
	rm, _ := ParseRoadmap(sampleRoadmap)
	done, total := rm.TotalPlans()
	if total != 6 {
		t.Errorf("total plans = %d, want 6", total)
	}
	if done != 3 { // 01-01, 01-02, 02-02
		t.Errorf("done plans = %d, want 3", done)
	}
	if got := rm.Percent(); got != 50 {
		t.Errorf("percent = %d, want 50", got)
	}
}

func TestNextPhaseOrdering(t *testing.T) {
	rm, _ := ParseRoadmap(sampleRoadmap)
	next := rm.NextPhase()
	if next == nil {
		t.Fatal("expected an incomplete phase")
	}
	// Phase 1 is done; next incomplete in numeric order is Phase 2.
	if next.Number != (PhaseNumber{Major: 2}) {
		t.Errorf("next phase = %s, want 2", next.Number)
	}
}

func TestPhaseNumberOrdering(t *testing.T) {
	two := PhaseNumber{Major: 2}
	twoOne := PhaseNumber{Major: 2, Minor: 1}
	three := PhaseNumber{Major: 3}
	if !two.Less(twoOne) || !twoOne.Less(three) || !two.Less(three) {
		t.Error("expected 2 < 2.1 < 3")
	}
	if twoOne.String() != "2.1" || two.String() != "2" {
		t.Errorf("string forms wrong: %s %s", two, twoOne)
	}
}

func TestSetPlanDone(t *testing.T) {
	out, changed := SetPlanDone(sampleRoadmap, "02-01", true)
	if !changed {
		t.Fatal("expected a change")
	}
	rm, _ := ParseRoadmap(out)
	p2 := rm.Phase(PhaseNumber{Major: 2})
	for _, pl := range p2.Plans {
		if pl.ID == "02-01" && !pl.Done {
			t.Error("02-01 should be done after SetPlanDone")
		}
	}
	// Unrelated plan untouched.
	if _, changed := SetPlanDone(sampleRoadmap, "99-99", true); changed {
		t.Error("nonexistent plan should not change content")
	}
}

func TestSetCheckboxIgnoresMarkersInDescription(t *testing.T) {
	// A plan whose description text itself contains checkbox tokens must have
	// only its own (leftmost) checkbox flipped, never the description.
	const rm = "## Phase Details\n\n### Phase 1: X\n\nPlans:\n" +
		"- [x] 01-01: verify [ ] the box and [x] again\n"

	out, changed := SetPlanDone(rm, "01-01", false)
	if !changed {
		t.Fatal("expected the plan checkbox to change")
	}
	if !strings.Contains(out, "- [ ] 01-01: verify [ ] the box and [x] again") {
		t.Errorf("wrong marker flipped — description corrupted:\n%s", out)
	}

	// Marking it done again must likewise leave the description intact.
	out2, _ := SetPlanDone(out, "01-01", true)
	if !strings.Contains(out2, "- [x] 01-01: verify [ ] the box and [x] again") {
		t.Errorf("description not preserved on re-mark:\n%s", out2)
	}
}

func TestSetPhaseDone(t *testing.T) {
	out, changed := SetPhaseDone(sampleRoadmap, PhaseNumber{Major: 2}, true)
	if !changed {
		t.Fatal("expected a change")
	}
	rm, _ := ParseRoadmap(out)
	if !rm.Phase(PhaseNumber{Major: 2}).Done {
		t.Error("phase 2 should be done")
	}
	// Phase 2.1 must remain untouched (distinct number, similar prefix).
	if rm.Phase(PhaseNumber{Major: 2, Minor: 1}).Done {
		t.Error("phase 2.1 should not be affected by marking phase 2")
	}
}
