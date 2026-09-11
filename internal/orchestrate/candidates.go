package orchestrate

import (
	"time"

	"gophermind/internal/freellm"
	"gophermind/internal/modelcat"
	"gophermind/internal/phaseflow"
)

// DefaultCandidates builds a phaseflow.FallbackRunner.Candidates function
// from the model picker's catalogue and settings: when a task already
// carries CandidateModels, those are used unchanged. Otherwise the
// candidate list is every reachable, non-excluded model from the
// catalogue, in the user's preference order (see
// modelcat.OrderedCandidates) - a term the user excluded can never appear
// in it, for the same reason it can never be auto-selected by
// modelcat.Next. If the catalogue yields nothing (no reachable providers,
// or none configured), t.Model is used as a single-candidate list, so a
// plan written before candidate models existed still runs unchanged.
//
// The catalogue is built once, when this function is called, not per task.
func DefaultCandidates() func(t phaseflow.Task) []string {
	o, _ := freellm.LoadOdometer(freellm.OdometerPath())
	s, _ := modelcat.LoadSettings(modelcat.SettingsPath())
	entries := modelcat.Build(o, s, nil, time.Now())

	return func(t phaseflow.Task) []string {
		if len(t.CandidateModels) > 0 {
			return t.CandidateModels
		}
		if cands := modelcat.OrderedCandidates(entries, s); len(cands) > 0 {
			return cands
		}
		if t.Model != "" {
			return []string{t.Model}
		}
		return nil
	}
}
