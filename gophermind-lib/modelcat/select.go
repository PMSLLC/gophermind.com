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
//     This applies to the model already in use as well: an excluded
//     current model is moved off rather than kept, because a rule that
//     only blocks arrivals never takes effect on the one selection that
//     is already wrong. That check runs before rule 3, since the
//     exclusion list is a legal constraint and s.CycleOnCapacity is a
//     preference about capacity, which is a different question.
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

	// Rule 1, applied to the current selection: an excluded model that is
	// already in use has to be replaced, or the exclusion never bites.
	if currentExcluded(entries, excluded, currentProfile, currentModel) {
		return replaceExcludedCurrent(entries, s, excluded, current)
	}

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

	// Rules 1, 2, 5 and 6: preference order first, then the rest of the
	// catalogue, skipping anything excluded, unreachable or near capacity.
	if e, ok := firstAllowed(entries, s, excluded, true); ok {
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

// currentExcluded reports whether the model currently in use carries a term
// the user excluded. A current model absent from entries is not excluded:
// there is no terms data to judge it by, and inventing a verdict would move
// a user off a model for no evidence.
func currentExcluded(entries []Entry, excluded map[string]bool, currentProfile, currentModel string) bool {
	if len(excluded) == 0 {
		return false
	}
	for _, e := range entries {
		if e.Profile == currentProfile && e.ID == currentModel {
			return hasExcludedTerm(e, excluded)
		}
	}
	return false
}

// replaceExcludedCurrent picks what to move to when the current model
// carries an excluded term.
//
// It prefers a permitted model with capacity left, then accepts a permitted
// model that is near capacity: being near a quota costs at worst one 429,
// while staying on an excluded model breaks the constraint the list exists
// to enforce, so a full permitted model is the better of the two.
//
// If nothing permitted is reachable at all, the current model is kept, since
// Next never returns an empty choice and there is nothing to return instead.
// That case carries a Reason so the UI can say the exclusion could not be
// honored rather than leaving it looking applied.
func replaceExcludedCurrent(entries []Entry, s Settings, excluded map[string]bool, current Choice) Choice {
	for _, skipNearCapacity := range []bool{true, false} {
		if e, ok := firstAllowed(entries, s, excluded, skipNearCapacity); ok {
			return Choice{
				Profile:  e.Profile,
				Model:    e.ID,
				Switched: true,
				Reason:   "switched: the previous model's terms are on your excluded list",
			}
		}
	}
	current.Reason = "your current model's terms are on your excluded list, but no other model is reachable to move to"
	return current
}

// firstAllowed returns the first entry that may be selected, walking s.Order
// first and then the catalogue's own order for entries s.Order did not
// mention. An excluded or unreachable entry is never returned; a
// near-capacity one is skipped only when skipNearCapacity is set.
func firstAllowed(entries []Entry, s Settings, excluded map[string]bool, skipNearCapacity bool) (Entry, bool) {
	eligible := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if hasExcludedTerm(e, excluded) {
			continue
		}
		if !e.Reachable {
			continue
		}
		if skipNearCapacity && e.NearCapacity {
			continue
		}
		eligible = append(eligible, e)
	}

	inOrder := make(map[string]bool, len(s.Order))
	for _, key := range s.Order {
		inOrder[key] = true
	}
	for _, key := range s.Order {
		for _, e := range eligible {
			if entryKey(e) == key {
				return e, true
			}
		}
	}
	for _, e := range eligible {
		if !inOrder[entryKey(e)] {
			return e, true
		}
	}
	return Entry{}, false
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
