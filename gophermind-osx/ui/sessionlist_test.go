package ui

import (
	"testing"

	"gophermind/gophermind-lib/session"
)

func TestSessionListState_SetEntriesAndEntries(t *testing.T) {
	s := NewSessionListState()
	entries := []SessionEntry{
		{Info: session.Info{ID: "s1", Title: "first", Messages: 3}, Backend: "local"},
		{Info: session.Info{ID: "s2", Title: "second", Messages: 1}, Backend: "remote"},
	}
	s.SetEntries(entries)
	got := s.Entries()
	if len(got) != 2 || got[0].Info.ID != "s1" || got[1].Backend != "remote" {
		t.Errorf("Entries() = %+v", got)
	}
}

func TestSessionListState_SelectRecordsSelectedIDAndClearsConfig(t *testing.T) {
	s := NewSessionListState()
	s.SetConfig(SessionConfig{Model: "m1"})
	s.Select("s1")
	if s.SelectedID() != "s1" {
		t.Errorf("SelectedID() = %q, want s1", s.SelectedID())
	}
	if s.Config() != (SessionConfig{}) {
		t.Errorf("Config() = %+v, want cleared on Select", s.Config())
	}
}

func TestSessionListState_SetConfig(t *testing.T) {
	s := NewSessionListState()
	s.SetConfig(SessionConfig{Model: "m1", Mode: "coding", Root: "/tmp/proj"})
	want := SessionConfig{Model: "m1", Mode: "coding", Root: "/tmp/proj"}
	if s.Config() != want {
		t.Errorf("Config() = %+v, want %+v", s.Config(), want)
	}
}

func TestSessionListState_RenameLocalUpdatesMatchingEntry(t *testing.T) {
	s := NewSessionListState()
	s.SetEntries([]SessionEntry{{Info: session.Info{ID: "s1", Name: ""}}})
	s.RenameLocal("s1", "My renamed session")
	got := s.Entries()
	if got[0].Info.Name != "My renamed session" {
		t.Errorf("RenameLocal did not update Name: %+v", got[0])
	}
}

func TestSessionListState_RemoveLocalDropsEntryAndClearsSelection(t *testing.T) {
	s := NewSessionListState()
	s.SetEntries([]SessionEntry{
		{Info: session.Info{ID: "s1"}},
		{Info: session.Info{ID: "s2"}},
	})
	s.Select("s1")
	s.RemoveLocal("s1")

	got := s.Entries()
	if len(got) != 1 || got[0].Info.ID != "s2" {
		t.Errorf("Entries() after RemoveLocal = %+v", got)
	}
	if s.SelectedID() != "" {
		t.Errorf("SelectedID() = %q, want cleared after removing the selected session", s.SelectedID())
	}
}

func TestSessionListState_RemoveLocalOfNonSelectedLeavesSelectionIntact(t *testing.T) {
	s := NewSessionListState()
	s.SetEntries([]SessionEntry{
		{Info: session.Info{ID: "s1"}},
		{Info: session.Info{ID: "s2"}},
	})
	s.Select("s1")
	s.RemoveLocal("s2")
	if s.SelectedID() != "s1" {
		t.Errorf("SelectedID() = %q, want s1 to remain selected", s.SelectedID())
	}
}

func TestSessionListState_NewRootAndNewMode(t *testing.T) {
	s := NewSessionListState()
	if s.NewMode() != SessionModes[0] {
		t.Errorf("NewMode() default = %q, want %q", s.NewMode(), SessionModes[0])
	}
	s.SetNewRoot("/Users/me/project")
	s.SetNewMode("architect")
	if s.NewRoot() != "/Users/me/project" {
		t.Errorf("NewRoot() = %q", s.NewRoot())
	}
	if s.NewMode() != "architect" {
		t.Errorf("NewMode() = %q", s.NewMode())
	}
}

func TestSessionListState_OnChangeFiresOnMutations(t *testing.T) {
	s := NewSessionListState()
	calls := 0
	s.OnChange(func() { calls++ })
	s.SetEntries(nil)
	s.Select("s1")
	s.SetConfig(SessionConfig{})
	s.RenameLocal("s1", "x")
	s.RemoveLocal("s1")
	s.SetNewRoot("/tmp")
	s.SetNewMode("tester")
	if calls != 7 {
		t.Errorf("OnChange called %d times, want 7", calls)
	}
}

func TestSessionModes_FixedFiveValues(t *testing.T) {
	want := []string{"coding", "conversational", "reviewer", "architect", "tester"}
	if len(SessionModes) != len(want) {
		t.Fatalf("SessionModes = %v, want %v", SessionModes, want)
	}
	for i, m := range want {
		if SessionModes[i] != m {
			t.Errorf("SessionModes[%d] = %q, want %q", i, SessionModes[i], m)
		}
	}
}
