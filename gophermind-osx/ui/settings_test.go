package ui

import (
	"strings"
	"testing"
)

func TestBackendListState_AddAppendsProfile(t *testing.T) {
	b := NewBackendListState()
	b.Add(BackendProfile{Name: "home", Mode: "local"})

	got := b.Profiles()
	if len(got) != 1 || got[0].Name != "home" {
		t.Errorf("Profiles() = %+v", got)
	}
}

func TestBackendListState_AddReplacesExistingNameInPlace(t *testing.T) {
	b := NewBackendListState()
	b.Add(BackendProfile{Name: "home", Mode: "local"})
	b.Add(BackendProfile{Name: "home", Mode: "remote", ServerURL: "https://x"})

	got := b.Profiles()
	if len(got) != 1 || got[0].Mode != "remote" || got[0].ServerURL != "https://x" {
		t.Errorf("Profiles() = %+v, want one updated entry", got)
	}
}

func TestBackendListState_RemoveDropsProfile(t *testing.T) {
	b := NewBackendListState()
	b.Add(BackendProfile{Name: "home"})
	b.Add(BackendProfile{Name: "work"})

	b.Remove("home")

	got := b.Profiles()
	if len(got) != 1 || got[0].Name != "work" {
		t.Errorf("Profiles() = %+v, want only work", got)
	}
}

func TestBackendListState_SetStatusAndStatus(t *testing.T) {
	b := NewBackendListState()
	b.Add(BackendProfile{Name: "home"})

	if got := b.Status("home"); got != "disconnected" {
		t.Errorf("Status() before SetStatus = %q, want disconnected default", got)
	}

	b.SetStatus("home", "connected")
	if got := b.Status("home"); got != "connected" {
		t.Errorf("Status() = %q, want connected", got)
	}
}

func TestBackendListState_ActiveDefaultsEmptyAndIsSettable(t *testing.T) {
	b := NewBackendListState()
	if b.Active() != "" {
		t.Errorf("Active() = %q, want empty by default", b.Active())
	}
	b.Add(BackendProfile{Name: "home"})
	b.SetActive("home")
	if b.Active() != "home" {
		t.Errorf("Active() = %q, want home", b.Active())
	}
}

func TestBackendListState_OnChangeFiresOnMutation(t *testing.T) {
	b := NewBackendListState()
	calls := 0
	b.OnChange(func() { calls++ })

	b.Add(BackendProfile{Name: "home"})
	b.SetStatus("home", "connected")
	b.SetActive("home")
	b.Remove("home")

	if calls != 4 {
		t.Errorf("OnChange called %d times, want 4", calls)
	}
}

func TestCacheHistorySettings_DefaultsAndRoundTrip(t *testing.T) {
	d := DefaultCacheHistorySettings()
	if d.MaxHistoryMessages <= 0 {
		t.Errorf("DefaultCacheHistorySettings().MaxHistoryMessages = %d, want > 0", d.MaxHistoryMessages)
	}
	if !d.PersistHistory {
		t.Error("DefaultCacheHistorySettings().PersistHistory should default true")
	}
}

func TestCacheHistorySettings_SaveLoadRoundTrips(t *testing.T) {
	var buf strings.Builder
	s := CacheHistorySettings{MaxHistoryMessages: 500, PersistHistory: false}
	if err := s.Save(&buf); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := LoadCacheHistorySettings(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatalf("LoadCacheHistorySettings: %v", err)
	}
	if loaded.MaxHistoryMessages != 500 || loaded.PersistHistory {
		t.Errorf("loaded = %+v, want {500 false}", loaded)
	}
}

func TestLoadCacheHistorySettings_EmptyReturnsDefault(t *testing.T) {
	loaded, err := LoadCacheHistorySettings(strings.NewReader(""))
	if err != nil {
		t.Fatalf("LoadCacheHistorySettings: %v", err)
	}
	if loaded != DefaultCacheHistorySettings() {
		t.Errorf("loaded = %+v, want defaults", loaded)
	}
}
