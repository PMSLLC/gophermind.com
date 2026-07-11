package phaseflow

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

// STATE.md is PhaseFlow's "read-first" file — the digest a session opens with to
// learn where the project stands. Upstream keeps its Current Position block
// current by spending an agent turn (or a Node SDK call) after every change;
// this file makes that block a pure function of the roadmap instead, so it costs
// no tokens and can never disagree with the checkboxes it claims to summarize.

// position is the fully-derived snapshot of where the project stands. Every
// field comes from the roadmap's phases and plan checkboxes — nothing here is
// read back from STATE.md, so a stale or hand-edited STATE.md cannot poison it.
type position struct {
	PhaseIndex int    // 1-based ordinal of the current phase among all phases
	PhaseCount int    // total number of phases
	PhaseName  string // current phase name (empty when all complete)
	PlansDone  int    // completed plans in the current phase
	PlansTotal int    // total plans in the current phase
	Status     string // Ready to plan | In progress | Phase complete | Complete
	Percent    int    // overall completion across all plans
}

// derivePosition computes the current position from a parsed roadmap. The
// "current" phase is the first incomplete one; when every phase is done the
// position points past the end and reports overall completion.
func derivePosition(rm *Roadmap) position {
	p := position{PhaseCount: len(rm.Phases), Percent: rm.Percent()}
	next := rm.NextPhase()
	if next == nil {
		p.PhaseIndex = len(rm.Phases)
		p.Status = "Complete"
		return p
	}
	for i := range rm.Phases {
		if rm.Phases[i].Number == next.Number {
			p.PhaseIndex = i + 1
			break
		}
	}
	p.PhaseName = next.Name
	for _, pl := range next.Plans {
		p.PlansTotal++
		if pl.Done {
			p.PlansDone++
		}
	}
	switch {
	case p.PlansTotal > 0 && p.PlansDone == p.PlansTotal:
		p.Status = "Phase complete"
	case p.PlansDone > 0:
		p.Status = "In progress"
	default:
		p.Status = "Ready to plan"
	}
	return p
}

// Regexes that target the individual "Field: value" lines of STATE.md's Current
// Position block. Each is anchored to the line start so only that field is
// rewritten and the surrounding document is left untouched.
var (
	reStatePhase    = regexp.MustCompile(`(?m)^Phase:.*$`)
	reStatePlan     = regexp.MustCompile(`(?m)^Plan:.*$`)
	reStateStatus   = regexp.MustCompile(`(?m)^Status:.*$`)
	reStateActivity = regexp.MustCompile(`(?m)^Last activity:.*$`)
	reStateProgress = regexp.MustCompile(`(?m)^Progress:.*$`)
)

// applyPosition rewrites STATE.md content in place with a freshly computed
// position and an activity note stamped with today's date. Any field line that
// is absent is simply left as-is rather than inserted, so a hand-trimmed
// STATE.md never grows fields the author removed.
func applyPosition(content string, p position, activity string) string {
	today := time.Now().Format("2006-01-02")
	repl := func(re *regexp.Regexp, line string) {
		if re.MatchString(content) {
			content = re.ReplaceAllLiteralString(content, line)
		}
	}
	repl(reStatePhase, fmt.Sprintf("Phase: %d of %d (%s)", p.PhaseIndex, p.PhaseCount, orDash(p.PhaseName)))
	repl(reStatePlan, fmt.Sprintf("Plan: %d of %d in current phase", p.PlansDone, p.PlansTotal))
	repl(reStateStatus, "Status: "+p.Status)
	repl(reStateActivity, fmt.Sprintf("Last activity: %s — %s", today, activity))
	repl(reStateProgress, fmt.Sprintf("Progress: %s %d%%", progressBar(p.Percent), p.Percent))
	return content
}

// orDash returns s, or "—" when s is empty, for fields that have no value.
func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// SyncState recomputes STATE.md's Current Position and progress from the current
// roadmap and records activity as the latest action. It is a no-op-safe way to
// keep the project's living memory consistent after any change to the roadmap;
// callers pass a short activity note (e.g. "completed plan 02-01"). A missing
// STATE.md is created from the roadmap title so sync never fails on a partially
// scaffolded project.
func (e *Engine) SyncState(activity string) error {
	rm, err := LoadRoadmap(e.Root)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(StatePath(e.Root))
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		data = []byte(starterState(rm.Title))
	}
	updated := applyPosition(string(data), derivePosition(rm), strings.TrimSpace(activity))
	return os.WriteFile(StatePath(e.Root), []byte(updated), 0o644)
}

// CompletePlan marks a plan done in ROADMAP.md and refreshes STATE.md to match,
// entirely in Go — the deterministic bookkeeping that would otherwise be spent
// as agent work. When marking the plan completes its phase, the phase's summary
// checkbox is ticked too.
func (e *Engine) CompletePlan(planID string) error {
	if err := MarkPlan(e.Root, planID, true); err != nil {
		return err
	}
	// If that was the phase's last open plan, tick the phase checkbox as well.
	if rm, err := LoadRoadmap(e.Root); err == nil {
		if ph := owningPhase(rm, planID); ph != nil && allPlansDone(ph) {
			_ = MarkPhase(e.Root, ph.Number, true)
		}
	}
	return e.SyncState("completed plan " + planID)
}

// owningPhase returns the phase that contains planID, or nil.
func owningPhase(rm *Roadmap, planID string) *Phase {
	for i := range rm.Phases {
		for _, pl := range rm.Phases[i].Plans {
			if pl.ID == planID {
				return &rm.Phases[i]
			}
		}
	}
	return nil
}

// allPlansDone reports whether every plan in the phase is complete. A phase with
// no plans is not considered done (there is nothing to have completed).
func allPlansDone(p *Phase) bool {
	if len(p.Plans) == 0 {
		return false
	}
	for _, pl := range p.Plans {
		if !pl.Done {
			return false
		}
	}
	return true
}
