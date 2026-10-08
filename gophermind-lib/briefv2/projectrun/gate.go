package projectrun

import (
	"context"
	"errors"
	"fmt"

	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/planner"
)

// ErrUnattended is returned by the unattended gate for a question or a
// confirmation: with nobody to ask, reaching the gate is a defect.
var ErrUnattended = errors.New("projectrun: no human is available in an unattended run")

type unattendedGate struct{ runDir func() (string, error) }

func newUnattendedGate(runDir func() (string, error)) human.Gate { return unattendedGate{runDir} }

func (unattendedGate) Ask(context.Context, []human.Question) ([]human.Answer, error) {
	return nil, ErrUnattended
}

// Confirm fails loudly. An unattended run confirms the understanding by the
// planner's own rule and records the hash; it never approves through the gate.
func (unattendedGate) Confirm(context.Context, human.Understanding) (human.Decision, error) {
	return human.Decision{}, ErrUnattended
}

// Approve approves a plan only when every requirement of the brief appears in
// coverage.json. It reads the two JSON files and parses no markdown.
func (g unattendedGate) Approve(_ context.Context, plan human.PlanSummary) (human.Decision, error) {
	dir, err := g.runDir()
	if err != nil {
		return human.Decision{}, err
	}
	refuse := func(note string) (human.Decision, error) {
		return human.Decision{By: "unattended", Note: note}, nil
	}
	reqs, err := planner.ReadRequirements(dir)
	if err != nil {
		return human.Decision{}, err
	}
	cov, err := planner.ReadCoverage(dir)
	if err != nil {
		return refuse("no coverage file")
	}
	covered := map[string]bool{}
	for _, c := range cov.Covered {
		covered[c.Requirement] = true
	}
	n := 0
	for _, r := range reqs {
		if covered[r.ID] {
			n++
		}
	}
	if len(reqs) == 0 || n != len(reqs) {
		return refuse(fmt.Sprintf("coverage %d of %d", n, len(reqs)))
	}
	h := plan.Hash
	if h == "" {
		return refuse("plan has no hash")
	}
	if len(h) > 12 {
		h = h[:12]
	}
	return human.Decision{Approved: true, By: "unattended", Note: "plan " + h}, nil
}

func (unattendedGate) Escalate(context.Context, human.Escalation) (human.Resolution, error) {
	return human.Resolution{Action: human.ActionStop, AnsweredBy: human.AnsweredByUnattended}, nil
}
