package ui

import (
	"sync"

	"gophermind/gophermind-lib/modelcat"
)

// ModelKey returns e's "profile/model" identity, matching the key format
// modelcat.Settings.Order documents for its own entries.
func ModelKey(e modelcat.Entry) string {
	return e.Profile + "/" + e.ID
}

// ModelPickerState is the model picker's plain-Go state (.planning/tasks/
// 04-04.json): the current catalogue, the persisted modelcat.Settings, the
// currently-pinned model, and two filters (provider, modality) that
// modelcat.Settings has no field for and so are session-local rather than
// persisted -- ExcludedTerms/FilterReachable/FilterHasCapacity already come
// from Settings and round-trip through PatchModelSettings (see
// gophermind-osx/modelpicker.go, the cgo widget layer that owns that
// client call).
//
// Same split as PanelState/ApprovalTracker: plain Go here, no cgo, no
// direct dependency on gophermind-osx/client (the widget layer injects
// results via SetEntries/SetCurrentKey rather than this type fetching
// anything itself).
type ModelPickerState struct {
	mu             sync.Mutex
	entries        []modelcat.Entry
	settings       modelcat.Settings
	currentKey     string
	providerFilter string
	modalityFilter string
	onChange       func()
}

// NewModelPickerState returns a picker state seeded with entries (the
// catalogue, e.g. from client.Catalogue) and settings (e.g. from
// client.ModelSettings).
func NewModelPickerState(entries []modelcat.Entry, settings modelcat.Settings) *ModelPickerState {
	return &ModelPickerState{entries: entries, settings: settings}
}

// OnChange registers f to be called after every mutation. Same
// non-blocking, non-reentrant contract as PanelState.OnChange.
func (s *ModelPickerState) OnChange(f func()) {
	s.mu.Lock()
	s.onChange = f
	s.mu.Unlock()
}

func (s *ModelPickerState) notify() {
	s.mu.Lock()
	f := s.onChange
	s.mu.Unlock()
	if f != nil {
		f()
	}
}

// SetEntries replaces the catalogue, e.g. after a fresh client.Catalogue
// call.
func (s *ModelPickerState) SetEntries(entries []modelcat.Entry) {
	s.mu.Lock()
	s.entries = entries
	s.mu.Unlock()
	s.notify()
}

// SetCurrentKey records which model is currently pinned to the session, so
// FilteredEntries' caller can highlight it (see IsCurrent).
func (s *ModelPickerState) SetCurrentKey(key string) {
	s.mu.Lock()
	s.currentKey = key
	s.mu.Unlock()
	s.notify()
}

// IsCurrent reports whether e is the currently-pinned model.
func (s *ModelPickerState) IsCurrent(e modelcat.Entry) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentKey != "" && s.currentKey == ModelKey(e)
}

// SetProviderFilter restricts FilteredEntries to entries whose Provider
// equals provider; "" clears the filter.
func (s *ModelPickerState) SetProviderFilter(provider string) {
	s.mu.Lock()
	s.providerFilter = provider
	s.mu.Unlock()
	s.notify()
}

// SetModalityFilter restricts FilteredEntries to entries whose Modality
// equals modality; "" clears the filter.
func (s *ModelPickerState) SetModalityFilter(modality string) {
	s.mu.Lock()
	s.modalityFilter = modality
	s.mu.Unlock()
	s.notify()
}

// FilteredEntries applies every active filter to the catalogue: the
// persisted Settings.FilterReachable/FilterHasCapacity/ExcludedTerms, plus
// this state's own provider/modality filters.
func (s *ModelPickerState) FilteredEntries() []modelcat.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	excluded := make(map[string]bool, len(s.settings.ExcludedTerms))
	for _, t := range s.settings.ExcludedTerms {
		excluded[t] = true
	}
	var out []modelcat.Entry
	for _, e := range s.entries {
		if s.settings.FilterReachable && !e.Reachable {
			continue
		}
		if s.settings.FilterHasCapacity && e.NearCapacity {
			continue
		}
		if s.providerFilter != "" && e.Provider != s.providerFilter {
			continue
		}
		if s.modalityFilter != "" && e.Modality != s.modalityFilter {
			continue
		}
		if hasExcludedTerm(e.Terms, excluded) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func hasExcludedTerm(terms []string, excluded map[string]bool) bool {
	for _, t := range terms {
		if excluded[t] {
			return true
		}
	}
	return false
}

// Order returns the preference order (Settings.Order), oldest-preferred
// first, as it currently stands.
func (s *ModelPickerState) Order() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.settings.Order))
	copy(out, s.settings.Order)
	return out
}

// AddToOrder appends key to the preference order, unless it's already
// present (a duplicate would just be dead weight in the list, never
// reachable ahead of its first occurrence).
func (s *ModelPickerState) AddToOrder(key string) {
	s.mu.Lock()
	for _, k := range s.settings.Order {
		if k == key {
			s.mu.Unlock()
			return
		}
	}
	s.settings.Order = append(s.settings.Order, key)
	s.mu.Unlock()
	s.notify()
}

// RemoveFromOrder removes key from the preference order, if present.
func (s *ModelPickerState) RemoveFromOrder(key string) {
	s.mu.Lock()
	order := s.settings.Order
	for i, k := range order {
		if k == key {
			s.settings.Order = append(order[:i], order[i+1:]...)
			break
		}
	}
	s.mu.Unlock()
	s.notify()
}

// MoveUp moves key one position earlier in the preference order. A no-op
// if key is missing or already first.
func (s *ModelPickerState) MoveUp(key string) {
	s.mu.Lock()
	i := indexOf(s.settings.Order, key)
	if i > 0 {
		s.settings.Order[i-1], s.settings.Order[i] = s.settings.Order[i], s.settings.Order[i-1]
	}
	s.mu.Unlock()
	s.notify()
}

// MoveDown moves key one position later in the preference order. A no-op
// if key is missing or already last.
func (s *ModelPickerState) MoveDown(key string) {
	s.mu.Lock()
	i := indexOf(s.settings.Order, key)
	if i >= 0 && i < len(s.settings.Order)-1 {
		s.settings.Order[i+1], s.settings.Order[i] = s.settings.Order[i], s.settings.Order[i+1]
	}
	s.mu.Unlock()
	s.notify()
}

func indexOf(order []string, key string) int {
	for i, k := range order {
		if k == key {
			return i
		}
	}
	return -1
}

// FilterReachable reports the persisted Settings.FilterReachable value.
func (s *ModelPickerState) FilterReachable() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings.FilterReachable
}

// SetFilterReachable sets Settings.FilterReachable, e.g. from the picker's
// "Reachable only" checkbox.
func (s *ModelPickerState) SetFilterReachable(v bool) {
	s.mu.Lock()
	s.settings.FilterReachable = v
	s.mu.Unlock()
	s.notify()
}

// FilterHasCapacity reports the persisted Settings.FilterHasCapacity value.
func (s *ModelPickerState) FilterHasCapacity() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings.FilterHasCapacity
}

// SetFilterHasCapacity sets Settings.FilterHasCapacity, e.g. from the
// picker's "Has capacity only" checkbox.
func (s *ModelPickerState) SetFilterHasCapacity(v bool) {
	s.mu.Lock()
	s.settings.FilterHasCapacity = v
	s.mu.Unlock()
	s.notify()
}

// AutoCyclingEnabled reports whether auto-cycling on capacity is on
// (Settings.CycleOnCapacity), for the picker's auto-cycling indicator.
func (s *ModelPickerState) AutoCyclingEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings.CycleOnCapacity
}

// SetAutoCycling sets Settings.CycleOnCapacity, e.g. from the settings
// panel's auto-cycling checkbox (.planning/tasks/04-07.json).
func (s *ModelPickerState) SetAutoCycling(v bool) {
	s.mu.Lock()
	s.settings.CycleOnCapacity = v
	s.mu.Unlock()
	s.notify()
}

// CapacityPercent reports Settings.CapacityPercent.
func (s *ModelPickerState) CapacityPercent() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings.CapacityPercent
}

// SetCapacityPercent sets Settings.CapacityPercent.
func (s *ModelPickerState) SetCapacityPercent(p int) {
	s.mu.Lock()
	s.settings.CapacityPercent = p
	s.mu.Unlock()
	s.notify()
}

// WhenAllFull reports Settings.WhenAllFull ("stay" or "ask").
func (s *ModelPickerState) WhenAllFull() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings.WhenAllFull
}

// SetWhenAllFull sets Settings.WhenAllFull.
func (s *ModelPickerState) SetWhenAllFull(v string) {
	s.mu.Lock()
	s.settings.WhenAllFull = v
	s.mu.Unlock()
	s.notify()
}

// ExcludedTerms returns a snapshot copy of Settings.ExcludedTerms.
func (s *ModelPickerState) ExcludedTerms() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.settings.ExcludedTerms))
	copy(out, s.settings.ExcludedTerms)
	return out
}

// SetExcludedTerms replaces Settings.ExcludedTerms wholesale, e.g. from the
// settings panel's excluded-terms editor.
func (s *ModelPickerState) SetExcludedTerms(terms []string) {
	s.mu.Lock()
	s.settings.ExcludedTerms = append([]string(nil), terms...)
	s.mu.Unlock()
	s.notify()
}

// CustomLinks returns a snapshot copy of Settings.CustomLinks.
func (s *ModelPickerState) CustomLinks() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.settings.CustomLinks))
	for k, v := range s.settings.CustomLinks {
		out[k] = v
	}
	return out
}

// SetCustomLink sets (or replaces) the custom link for key.
func (s *ModelPickerState) SetCustomLink(key, url string) {
	s.mu.Lock()
	if s.settings.CustomLinks == nil {
		s.settings.CustomLinks = make(map[string]string)
	}
	s.settings.CustomLinks[key] = url
	s.mu.Unlock()
	s.notify()
}

// RemoveCustomLink removes the custom link for key, if present.
func (s *ModelPickerState) RemoveCustomLink(key string) {
	s.mu.Lock()
	delete(s.settings.CustomLinks, key)
	s.mu.Unlock()
	s.notify()
}

// Settings returns a snapshot copy of the current modelcat.Settings, e.g.
// for the settings panel to pass whole to client.PatchModelSettings after
// an edit.
func (s *ModelPickerState) Settings() modelcat.Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.settings
	out.Order = append([]string(nil), s.settings.Order...)
	out.ExcludedTerms = append([]string(nil), s.settings.ExcludedTerms...)
	out.CustomLinks = make(map[string]string, len(s.settings.CustomLinks))
	for k, v := range s.settings.CustomLinks {
		out.CustomLinks[k] = v
	}
	return out
}
