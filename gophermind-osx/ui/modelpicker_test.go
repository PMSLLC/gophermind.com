package ui

import (
	"testing"

	"gophermind/gophermind-lib/modelcat"
)

func entry(id, provider, profile, modality string, reachable, nearCapacity bool, terms ...string) modelcat.Entry {
	return modelcat.Entry{
		ID: id, Provider: provider, Profile: profile, Modality: modality,
		Reachable: reachable, NearCapacity: nearCapacity, Terms: terms,
	}
}

func TestModelKey(t *testing.T) {
	e := entry("gpt-x", "OpenAI", "free-openai", "text", true, false)
	if got := ModelKey(e); got != "free-openai/gpt-x" {
		t.Errorf("ModelKey = %q, want %q", got, "free-openai/gpt-x")
	}
}

func TestModelPickerState_FilteredEntries_NoFiltersReturnsAll(t *testing.T) {
	entries := []modelcat.Entry{
		entry("a", "P1", "p1", "text", true, false),
		entry("b", "P2", "p2", "text", false, false),
	}
	s := NewModelPickerState(entries, modelcat.Settings{})
	if got := len(s.FilteredEntries()); got != 2 {
		t.Errorf("FilteredEntries() len = %d, want 2", got)
	}
}

func TestModelPickerState_FilteredEntries_ReachabilityFilter(t *testing.T) {
	entries := []modelcat.Entry{
		entry("a", "P1", "p1", "text", true, false),
		entry("b", "P2", "p2", "text", false, false),
	}
	s := NewModelPickerState(entries, modelcat.Settings{FilterReachable: true})
	got := s.FilteredEntries()
	if len(got) != 1 || got[0].ID != "a" {
		t.Errorf("FilteredEntries() = %+v, want only the reachable entry", got)
	}
}

func TestModelPickerState_FilteredEntries_CapacityFilter(t *testing.T) {
	entries := []modelcat.Entry{
		entry("a", "P1", "p1", "text", true, false),
		entry("b", "P2", "p2", "text", true, true), // near capacity
	}
	s := NewModelPickerState(entries, modelcat.Settings{FilterHasCapacity: true})
	got := s.FilteredEntries()
	if len(got) != 1 || got[0].ID != "a" {
		t.Errorf("FilteredEntries() = %+v, want only the entry with remaining capacity", got)
	}
}

func TestModelPickerState_FilteredEntries_ExcludedTermsFilter(t *testing.T) {
	entries := []modelcat.Entry{
		entry("a", "P1", "p1", "text", true, false),
		entry("b", "P2", "p2", "text", true, false, "non-commercial"),
	}
	s := NewModelPickerState(entries, modelcat.Settings{ExcludedTerms: []string{"non-commercial"}})
	got := s.FilteredEntries()
	if len(got) != 1 || got[0].ID != "a" {
		t.Errorf("FilteredEntries() = %+v, want the non-commercial entry excluded", got)
	}
}

func TestModelPickerState_FilteredEntries_ProviderFilter(t *testing.T) {
	entries := []modelcat.Entry{
		entry("a", "P1", "p1", "text", true, false),
		entry("b", "P2", "p2", "text", true, false),
	}
	s := NewModelPickerState(entries, modelcat.Settings{})
	s.SetProviderFilter("P1")
	got := s.FilteredEntries()
	if len(got) != 1 || got[0].ID != "a" {
		t.Errorf("FilteredEntries() with provider filter = %+v, want only P1's entry", got)
	}
}

func TestModelPickerState_FilteredEntries_ModalityFilter(t *testing.T) {
	entries := []modelcat.Entry{
		entry("a", "P1", "p1", "text", true, false),
		entry("b", "P2", "p2", "vision", true, false),
	}
	s := NewModelPickerState(entries, modelcat.Settings{})
	s.SetModalityFilter("vision")
	got := s.FilteredEntries()
	if len(got) != 1 || got[0].ID != "b" {
		t.Errorf("FilteredEntries() with modality filter = %+v, want only the vision entry", got)
	}
}

func TestModelPickerState_CurrentKey_IsCurrent(t *testing.T) {
	entries := []modelcat.Entry{entry("a", "P1", "p1", "text", true, false)}
	s := NewModelPickerState(entries, modelcat.Settings{})
	s.SetCurrentKey("p1/a")
	if !s.IsCurrent(entries[0]) {
		t.Error("IsCurrent should be true for the entry matching CurrentKey")
	}
	if s.IsCurrent(entry("z", "PZ", "pz", "text", true, false)) {
		t.Error("IsCurrent should be false for an unrelated entry")
	}
}

func TestModelPickerState_Order_AddMoveRemove(t *testing.T) {
	s := NewModelPickerState(nil, modelcat.Settings{})
	s.AddToOrder("p1/a")
	s.AddToOrder("p2/b")
	s.AddToOrder("p3/c")
	if got := s.Order(); len(got) != 3 || got[0] != "p1/a" || got[2] != "p3/c" {
		t.Fatalf("Order after adds = %v", got)
	}

	s.MoveUp("p3/c")
	if got := s.Order(); got[1] != "p3/c" {
		t.Errorf("Order after MoveUp = %v, want p3/c at index 1", got)
	}

	s.MoveDown("p1/a")
	if got := s.Order(); got[1] != "p1/a" {
		t.Errorf("Order after MoveDown = %v, want p1/a at index 1", got)
	}

	s.RemoveFromOrder("p2/b")
	got := s.Order()
	for _, k := range got {
		if k == "p2/b" {
			t.Errorf("Order after RemoveFromOrder still contains p2/b: %v", got)
		}
	}
	if len(got) != 2 {
		t.Errorf("Order after RemoveFromOrder len = %d, want 2", len(got))
	}
}

func TestModelPickerState_Order_AddDuplicateIsNoOp(t *testing.T) {
	s := NewModelPickerState(nil, modelcat.Settings{})
	s.AddToOrder("p1/a")
	s.AddToOrder("p1/a")
	if got := s.Order(); len(got) != 1 {
		t.Errorf("Order = %v, want no duplicate", got)
	}
}

func TestModelPickerState_Order_MoveUpAtTopIsNoOp(t *testing.T) {
	s := NewModelPickerState(nil, modelcat.Settings{})
	s.AddToOrder("p1/a")
	s.AddToOrder("p2/b")
	s.MoveUp("p1/a") // already at top
	if got := s.Order(); got[0] != "p1/a" || got[1] != "p2/b" {
		t.Errorf("Order = %v, want unchanged", got)
	}
}

func TestModelPickerState_Order_MoveDownAtBottomIsNoOp(t *testing.T) {
	s := NewModelPickerState(nil, modelcat.Settings{})
	s.AddToOrder("p1/a")
	s.AddToOrder("p2/b")
	s.MoveDown("p2/b") // already at bottom
	if got := s.Order(); got[0] != "p1/a" || got[1] != "p2/b" {
		t.Errorf("Order = %v, want unchanged", got)
	}
}

func TestModelPickerState_AutoCycling(t *testing.T) {
	s := NewModelPickerState(nil, modelcat.Settings{CycleOnCapacity: true})
	if !s.AutoCyclingEnabled() {
		t.Error("AutoCyclingEnabled should reflect Settings.CycleOnCapacity")
	}
}

func TestModelPickerState_OnChangeFiresOnMutation(t *testing.T) {
	s := NewModelPickerState(nil, modelcat.Settings{})
	calls := 0
	s.OnChange(func() { calls++ })
	s.AddToOrder("p1/a")
	s.SetCurrentKey("p1/a")
	s.SetProviderFilter("P1")
	s.SetModalityFilter("text")
	s.RemoveFromOrder("p1/a")
	if calls != 5 {
		t.Errorf("OnChange called %d times, want 5", calls)
	}
}

func TestModelPickerState_SetEntriesReplacesCatalogue(t *testing.T) {
	s := NewModelPickerState(nil, modelcat.Settings{})
	if got := len(s.FilteredEntries()); got != 0 {
		t.Fatalf("FilteredEntries() on empty state = %d, want 0", got)
	}
	s.SetEntries([]modelcat.Entry{entry("a", "P1", "p1", "text", true, false)})
	if got := len(s.FilteredEntries()); got != 1 {
		t.Errorf("FilteredEntries() after SetEntries = %d, want 1", got)
	}
}

func TestModelPickerState_FilterReachable_GetSet(t *testing.T) {
	s := NewModelPickerState(nil, modelcat.Settings{FilterReachable: true})
	if !s.FilterReachable() {
		t.Error("FilterReachable() should reflect seeded Settings")
	}
	calls := 0
	s.OnChange(func() { calls++ })
	s.SetFilterReachable(false)
	if s.FilterReachable() {
		t.Error("FilterReachable() should be false after SetFilterReachable(false)")
	}
	if calls != 1 {
		t.Errorf("OnChange called %d times, want 1", calls)
	}
}

func TestModelPickerState_FilterHasCapacity_GetSet(t *testing.T) {
	s := NewModelPickerState(nil, modelcat.Settings{})
	s.SetFilterHasCapacity(true)
	if !s.FilterHasCapacity() {
		t.Error("FilterHasCapacity() should be true after SetFilterHasCapacity(true)")
	}
}
