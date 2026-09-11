package modelcat

import "testing"

func TestNextKeepsCurrentModelWhenCyclingOff(t *testing.T) {
	entries := []Entry{
		{Profile: "free-a", ID: "m1", Reachable: true, NearCapacity: true},
	}
	s := Settings{CycleOnCapacity: false}
	got := Next(entries, s, "free-a", "m1")
	if got.Switched || got.Profile != "free-a" || got.Model != "m1" {
		t.Fatalf("cycling off: got %+v, want current model kept", got)
	}
}

func TestNextKeepsCurrentModelWhenNotNearCapacity(t *testing.T) {
	entries := []Entry{
		{Profile: "free-a", ID: "m1", Reachable: true, NearCapacity: false},
		{Profile: "free-b", ID: "m2", Reachable: true, NearCapacity: false},
	}
	s := Settings{CycleOnCapacity: true, Order: []string{"free-b/m2", "free-a/m1"}}
	got := Next(entries, s, "free-a", "m1")
	if got.Switched || got.Profile != "free-a" || got.Model != "m1" {
		t.Fatalf("current not near capacity: got %+v, want current model kept", got)
	}
}

func TestNextMovesToFirstPreferredEntryWhenCurrentIsFull(t *testing.T) {
	entries := []Entry{
		{Profile: "free-a", ID: "m1", Reachable: true, NearCapacity: true},
		{Profile: "free-b", ID: "m2", Reachable: true, NearCapacity: false},
	}
	s := Settings{CycleOnCapacity: true, Order: []string{"free-b/m2", "free-a/m1"}}
	got := Next(entries, s, "free-a", "m1")
	if !got.Switched || got.Profile != "free-b" || got.Model != "m2" {
		t.Fatalf("current full: got %+v, want switch to free-b/m2", got)
	}
	if got.Reason == "" {
		t.Error("a switch should carry a non-empty reason")
	}
}

func TestNextHonorsPreferenceOrderOverCatalogueOrder(t *testing.T) {
	// m3 appears first in the catalogue slice but is not in Order; m2 is
	// second in the catalogue but first in Order. Order must win.
	entries := []Entry{
		{Profile: "free-a", ID: "m1", Reachable: true, NearCapacity: true},
		{Profile: "free-c", ID: "m3", Reachable: true, NearCapacity: false},
		{Profile: "free-b", ID: "m2", Reachable: true, NearCapacity: false},
	}
	s := Settings{CycleOnCapacity: true, Order: []string{"free-b/m2"}}
	got := Next(entries, s, "free-a", "m1")
	if got.Profile != "free-b" || got.Model != "m2" {
		t.Fatalf("got %+v, want free-b/m2 preferred over catalogue-order free-c/m3", got)
	}
}

func TestNextSkipsUnreachableEntryEvenWhenFirstInOrder(t *testing.T) {
	entries := []Entry{
		{Profile: "free-a", ID: "m1", Reachable: true, NearCapacity: true},
		{Profile: "free-b", ID: "m2", Reachable: false, NearCapacity: false},
		{Profile: "free-d", ID: "m4", Reachable: true, NearCapacity: false},
	}
	s := Settings{CycleOnCapacity: true, Order: []string{"free-b/m2", "free-d/m4"}}
	got := Next(entries, s, "free-a", "m1")
	if got.Profile != "free-d" || got.Model != "m4" {
		t.Fatalf("got %+v, want the unreachable free-b/m2 skipped for free-d/m4", got)
	}
}

// TestNextNeverSelectsAnExcludedEntryEvenWhenPreferredAndEverythingElseFull
// is the rule 1 test: a user who excludes "non-commercial" must not have
// automatic cycling quietly select Cohere for their client work. The
// excluded entry here is otherwise perfect (reachable, not near capacity,
// first in Order); it must still be refused, even though the only other
// candidate is full.
func TestNextNeverSelectsAnExcludedEntryEvenWhenPreferredAndEverythingElseFull(t *testing.T) {
	entries := []Entry{
		{Profile: "free-a", ID: "m1", Reachable: true, NearCapacity: true},
		{Profile: "free-cohere", ID: "command-a", Reachable: true, NearCapacity: false, Terms: []string{"non-commercial"}},
	}
	s := Settings{
		CycleOnCapacity: true,
		Order:           []string{"free-cohere/command-a", "free-a/m1"},
		ExcludedTerms:   []string{"non-commercial"},
		WhenAllFull:     "stay",
	}
	got := Next(entries, s, "free-a", "m1")
	if got.Switched || got.Profile != "free-a" || got.Model != "m1" {
		t.Fatalf("excluded entry was selected: got %+v, want current model kept", got)
	}
}

func TestNextWhenAllFullStayKeepsCurrent(t *testing.T) {
	entries := []Entry{
		{Profile: "free-a", ID: "m1", Reachable: true, NearCapacity: true},
		{Profile: "free-b", ID: "m2", Reachable: true, NearCapacity: true},
	}
	s := Settings{CycleOnCapacity: true, Order: []string{"free-b/m2", "free-a/m1"}, WhenAllFull: "stay"}
	got := Next(entries, s, "free-a", "m1")
	if got.Switched || got.Profile != "free-a" || got.Model != "m1" {
		t.Fatalf("got %+v, want current model kept when everything is full", got)
	}
}

func TestNextWhenAllFullAskKeepsCurrentWithReason(t *testing.T) {
	entries := []Entry{
		{Profile: "free-a", ID: "m1", Reachable: true, NearCapacity: true},
		{Profile: "free-b", ID: "m2", Reachable: true, NearCapacity: true},
	}
	s := Settings{CycleOnCapacity: true, Order: []string{"free-b/m2", "free-a/m1"}, WhenAllFull: "ask"}
	got := Next(entries, s, "free-a", "m1")
	if got.Switched {
		t.Errorf("ask should not switch, got Switched=true")
	}
	if got.Profile != "free-a" || got.Model != "m1" {
		t.Fatalf("got %+v, want current model retained", got)
	}
	if got.Reason == "" {
		t.Error("ask with everything full should carry a non-empty reason")
	}
}

// TestNextNeverTreatsANoQuotaModelAsNearCapacity documents the phase 2
// invariant Next relies on: an entry with no published quota always carries
// NearCapacity false (there is no line to cross), so rule 4 keeps it
// forever even with cycling on.
func TestNextNeverTreatsANoQuotaModelAsNearCapacity(t *testing.T) {
	entries := []Entry{
		{Profile: "free-a", ID: "m1", Reachable: true, Quota: 0, NearCapacity: false},
		{Profile: "free-b", ID: "m2", Reachable: true, NearCapacity: false},
	}
	s := Settings{CycleOnCapacity: true, Order: []string{"free-b/m2", "free-a/m1"}}
	got := Next(entries, s, "free-a", "m1")
	if got.Switched || got.Profile != "free-a" || got.Model != "m1" {
		t.Fatalf("a no-quota current model should never be switched away from: got %+v", got)
	}
}

func TestNextOnEmptyCatalogueReturnsCurrentModel(t *testing.T) {
	s := Settings{CycleOnCapacity: true, WhenAllFull: "stay"}
	got := Next(nil, s, "free-a", "m1")
	if got.Switched || got.Profile != "free-a" || got.Model != "m1" {
		t.Fatalf("empty catalogue: got %+v, want current model kept", got)
	}
}
