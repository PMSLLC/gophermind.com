package modelcat

// MaxFallbacks bounds how many models one request will try after its primary.
//
// Every entry is a full attempt, with its own retry budget, against a provider
// that has already refused once. Four is enough to ride out a per-model rate
// limit (OVHcloud's anonymous tier caps at 2 requests per minute PER MODEL, so
// a sibling model has its own budget) without turning one slow turn into a
// minutes-long one.
const MaxFallbacks = 4

// FallbackModels returns the models llm.Client should try if the current one
// fails with something fallback-eligible, most notably a 429.
//
// They all belong to ONE provider, and that is not a preference. llm.Client
// swaps only the model string when it falls back, keeping the same BaseURL and
// API key, so another provider's model id sent to this endpoint is a 404
// rather than a fallback. Moving between providers needs a different client,
// which is a decision for the caller between turns, not something a single
// request can do.
//
// currentProfile empty means the user's own endpoint, which has no catalogue
// siblings to offer, so the result is empty and behaviour is unchanged.
//
// Reachability and the user's excluded terms are both honoured. A fallback is
// exactly the kind of quiet path that would otherwise defeat an exclusion the
// user set for legal reasons.
func FallbackModels(entries []Entry, s Settings, currentProfile, currentModel string) []string {
	if currentProfile == "" {
		return nil
	}
	excluded := excludedTermSet(s.ExcludedTerms)

	out := make([]string, 0, MaxFallbacks)
	// OrderedCandidates already applies the user's preference order, so a
	// fallback lands on the model they would have picked next.
	for _, id := range OrderedCandidates(entries, s) {
		if len(out) >= MaxFallbacks {
			break
		}
		if id == currentModel {
			// llm.Client tries Model first and then these, so repeating it
			// spends an attempt on the endpoint that just refused.
			continue
		}
		for _, e := range entries {
			if e.ID != id || e.Profile != currentProfile || !e.Reachable {
				continue
			}
			if hasExcludedTerm(e, excluded) {
				break
			}
			out = append(out, id)
			break
		}
	}
	return out
}
