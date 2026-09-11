package modelcat

// Choice is the outcome of applying the user's preferences to the
// catalogue. Model and Profile are never both empty when Next is given a
// non-empty currentModel: Next always returns a usable choice, keeping the
// current model when nothing qualifies to replace it.
type Choice struct {
	// Profile is the gophermind profile of the chosen model, or empty when
	// the model is served by the user's own configured endpoint.
	Profile string
	// Model is the chosen model's id.
	Model string
	// Switched reports whether this choice differs from the model that was
	// active before Next ran.
	Switched bool
	// Reason explains why, for the UI to show. Empty when nothing changed.
	Reason string
}

// Next decides which model should serve the next turn. It is a pure
// function: no I/O, no clock, no globals. Everything it needs arrives in
// its arguments, so it never needs a server or a browser to test.
//
// The rules apply in this order:
//
//  1. Any entry whose Terms intersect s.ExcludedTerms is removed from
//     consideration entirely, before anything else. It can never be
//     selected however preferred, and automatic cycling never reaches it.
//  2. An unreachable entry is never selected.
//  3. If s.CycleOnCapacity is false, keep the current model. Never switch.
//  4. If the current model is not near capacity, keep it.
//  5. Otherwise walk s.Order and pick the first entry that is reachable,
//     not excluded, and not near capacity.
//  6. If s.Order yields nothing, walk the remaining catalogue in its
//     natural order under the same conditions.
//  7. If nothing qualifies, honor s.WhenAllFull: "stay" keeps the current
//     model; "ask" returns the current model with Switched false and a
//     Reason saying every option is full.
//
// Next never returns an empty Choice. Refusing to run is worse than one
// 429, so when nothing qualifies to replace it, the current model is kept.
func Next(entries []Entry, s Settings, currentProfile, currentModel string) Choice {
	current := Choice{Profile: currentProfile, Model: currentModel}

	excluded := excludedTermSet(s.ExcludedTerms)

	// Rule 3: cycling off means never switch, regardless of anything else.
	if !s.CycleOnCapacity {
		return current
	}

	// Rule 4: a current model that is not near capacity is kept. A current
	// model absent from entries is treated the same way: there is no
	// evidence it needs to move, and keeping it is always safe.
	if !currentNearCapacity(entries, currentProfile, currentModel) {
		return current
	}

	// Rules 1, 2 and the "not near capacity" half of 5 and 6: build the
	// set of entries that could ever be switched to.
	eligible := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if hasExcludedTerm(e, excluded) {
			continue
		}
		if !e.Reachable {
			continue
		}
		if e.NearCapacity {
			continue
		}
		eligible = append(eligible, e)
	}

	// Rule 5: preference order first.
	inOrder := make(map[string]bool, len(s.Order))
	for _, key := range s.Order {
		inOrder[key] = true
	}
	for _, key := range s.Order {
		for _, e := range eligible {
			if entryKey(e) == key {
				return Choice{
					Profile:  e.Profile,
					Model:    e.ID,
					Switched: true,
					Reason:   "switched: the previous model was near its capacity limit",
				}
			}
		}
	}

	// Rule 6: the rest of the catalogue, in its natural order, for entries
	// s.Order did not mention.
	for _, e := range eligible {
		if inOrder[entryKey(e)] {
			continue
		}
		return Choice{
			Profile:  e.Profile,
			Model:    e.ID,
			Switched: true,
			Reason:   "switched: the previous model was near its capacity limit",
		}
	}

	// Rule 7: nothing qualifies. Honor s.WhenAllFull, defaulting to "stay".
	if s.WhenAllFull == "ask" {
		return Choice{
			Profile: currentProfile,
			Model:   currentModel,
			Reason:  "every model in your preferences is near capacity",
		}
	}
	return current
}

// OrderedCandidates returns the model id of every reachable, non-excluded
// entry, in the user's preference order (Settings.Order) followed by the
// rest of the catalogue in its natural order for anything Order did not
// mention. It is the ordered counterpart to Next: where Next picks the
// single next model to switch to, OrderedCandidates lists every model worth
// trying in sequence, for a task's candidate model fallback list.
//
// The same exclusion rule as Next applies, and for the same reason: an
// entry whose Terms intersect s.ExcludedTerms is never included, however
// preferred, because that setting exists for legal reasons. Unlike Next,
// NearCapacity is not filtered here - a candidate near capacity is still
// worth trying in a fallback list, since the point of trying it is finding
// out whether it actually fails.
func OrderedCandidates(entries []Entry, s Settings) []string {
	excluded := excludedTermSet(s.ExcludedTerms)

	eligible := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if hasExcludedTerm(e, excluded) {
			continue
		}
		if !e.Reachable {
			continue
		}
		eligible = append(eligible, e)
	}

	inOrder := make(map[string]bool, len(s.Order))
	for _, key := range s.Order {
		inOrder[key] = true
	}

	var out []string
	for _, key := range s.Order {
		for _, e := range eligible {
			if entryKey(e) == key {
				out = append(out, e.ID)
			}
		}
	}
	for _, e := range eligible {
		if inOrder[entryKey(e)] {
			continue
		}
		out = append(out, e.ID)
	}
	return out
}

// entryKey is the "profile/model" key an Entry is addressed by in
// Settings.Order and Settings.CustomLinks.
func entryKey(e Entry) string {
	return e.Profile + "/" + e.ID
}

// currentNearCapacity reports whether the model currently in use is marked
// NearCapacity in entries. A model not found in entries is treated as not
// near capacity: there is no evidence it needs to move.
func currentNearCapacity(entries []Entry, currentProfile, currentModel string) bool {
	for _, e := range entries {
		if e.Profile == currentProfile && e.ID == currentModel {
			return e.NearCapacity
		}
	}
	return false
}

// excludedTermSet turns a term list into a lookup set.
func excludedTermSet(terms []string) map[string]bool {
	if len(terms) == 0 {
		return nil
	}
	set := make(map[string]bool, len(terms))
	for _, t := range terms {
		set[t] = true
	}
	return set
}

// hasExcludedTerm reports whether any of e.Terms is in excluded.
func hasExcludedTerm(e Entry, excluded map[string]bool) bool {
	for _, t := range e.Terms {
		if excluded[t] {
			return true
		}
	}
	return false
}
