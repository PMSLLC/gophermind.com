package orchestrate

import (
	"time"

	"gophermind/internal/freellm"
	"gophermind/internal/modelcat"
	"gophermind/internal/phaseflow"
)

// DefaultCandidates builds a phaseflow.FallbackRunner.Candidates function for
// a runner whose client is pointed at baseURL.
//
// A task carrying its own CandidateModels is authoritative and those are used
// unchanged. Otherwise the list is the task's own model first, followed by the
// other models the SAME endpoint can serve, in the user's preference order and
// with excluded terms removed.
//
// The endpoint restriction is not a policy choice, it is a correctness one.
// FallbackRunner sets Task.Model to each candidate and runs the task, and
// Runner.newTaskAgent resolves that by cloning the one configured client. Every
// candidate therefore goes to baseURL with baseURL's key. A model id belonging
// to another provider is not a fallback; it is a request for a model this
// endpoint has never heard of, and it spends one of the task's attempts to
// learn that. Cross-provider fallback would need the runner to swap clients per
// candidate, which it cannot do today.
//
// When baseURL is the user's own endpoint (no free provider matches it), the
// catalogue knows no models for it and the list is simply the task's own model:
// the same single-model behaviour plans had before candidates existed.
func DefaultCandidates(baseURL string) func(t phaseflow.Task) []string {
	o, _ := freellm.LoadOdometer(freellm.OdometerPath())
	// A settings read failure cannot fail here: this builds a function, and
	// a plan must still run. What it must not do is widen the candidate list
	// using exclusions that are not the user's, so on a failed read the list
	// collapses to the task's own model and no automatic expansion happens.
	s, err := modelcat.LoadSettings(modelcat.SettingsPath())
	settingsKnown := err == nil
	entries := modelcat.Build(o, s, nil, time.Now())

	// The profile whose endpoint this runner is actually pointed at. Empty
	// means the user's own endpoint, and empty matches no catalogue entry,
	// which is what leaves such a runner with just the task's own model.
	profile := ""
	if c, ok := freellm.CompatForBaseURL(baseURL); ok {
		profile = c.Profile
	}

	return func(t phaseflow.Task) []string {
		if len(t.CandidateModels) > 0 {
			return t.CandidateModels
		}

		out := make([]string, 0, 4)
		seen := make(map[string]bool, 4)
		add := func(id string) {
			if id == "" || seen[id] {
				return
			}
			seen[id] = true
			out = append(out, id)
		}

		// The task's own model leads: it is what the plan asked for, and on
		// an unrecognised endpoint it is the only thing that can work.
		add(t.Model)

		if profile != "" && settingsKnown {
			for _, id := range modelcat.OrderedCandidates(entries, s) {
				if sameProfile(entries, id, profile) {
					add(id)
				}
			}
		}
		return out
	}
}

// sameProfile reports whether the catalogue entry for id belongs to profile.
func sameProfile(entries []modelcat.Entry, id, profile string) bool {
	for _, e := range entries {
		if e.ID == id {
			return e.Profile == profile
		}
	}
	return false
}
