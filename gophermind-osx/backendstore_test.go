package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	appui "gophermind/gophermind-osx/ui"
)

func TestLoadBackendProfilesFrom_MissingFileReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	got := loadBackendProfilesFrom(path)
	if len(got) != 0 {
		t.Errorf("loadBackendProfilesFrom on a missing file = %+v, want empty", got)
	}
}

func TestSaveLoadBackendProfilesTo_RoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backends.json")
	profiles := []appui.BackendProfile{
		{Name: "office", Mode: "remote", ServerURL: "10.0.0.5:8090", GocloakRealm: "gophermind"},
		{Name: "laptop", Mode: "local", ServerURL: "auto"},
	}

	saveBackendProfilesTo(path, profiles)
	got := loadBackendProfilesFrom(path)

	if len(got) != len(profiles) {
		t.Fatalf("loaded %d profiles, want %d", len(got), len(profiles))
	}
	for i, want := range profiles {
		if got[i] != want {
			t.Errorf("profile %d = %+v, want %+v", i, got[i], want)
		}
	}
}

// TestSaveBackendProfilesTo_NeverWritesToken is the actual security
// property this feature exists for: a bearer token must never land in
// the plain backends.json file, even if the caller passes a
// BackendProfile whose Token field is set (e.g. straight from
// BackendListState.Profiles(), which does carry it in memory).
func TestSaveBackendProfilesTo_NeverWritesToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backends.json")
	saveBackendProfilesTo(path, []appui.BackendProfile{
		{Name: "office", Mode: "remote", ServerURL: "10.0.0.5:8090", Token: "super-secret-bearer-token"},
	})

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "super-secret-bearer-token") {
		t.Errorf("backends.json contains the bearer token in plaintext: %s", raw)
	}

	// And loading it back must not fabricate a token from thin air either.
	got := loadBackendProfilesFrom(path)
	if len(got) != 1 || got[0].Token != "" {
		t.Errorf("loaded profile = %+v, want Token empty (tokens come from the Keychain, not this file)", got)
	}
}

// memTokenStore is an in-memory auth.TokenStore, standing in for the real
// Keychain the same way auth/manager_test.go's memStore does -- so these
// tests never risk the real Keychain's first-use interactive-access
// prompt hanging an automated test.
type memTokenStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMemTokenStore() *memTokenStore { return &memTokenStore{data: make(map[string][]byte)} }

func (s *memTokenStore) Save(key string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = append([]byte(nil), data...)
	return nil
}

func (s *memTokenStore) Load(key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.data[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return d, nil
}

func (s *memTokenStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

func TestSaveLoadBackendTokenTo_RoundTrips(t *testing.T) {
	store := newMemTokenStore()
	saveBackendTokenTo(store, "office", "abc123")

	got := loadBackendTokenFrom(store, "office")
	if got != "abc123" {
		t.Errorf("loadBackendTokenFrom = %q, want %q", got, "abc123")
	}
}

func TestLoadBackendTokenFrom_NeverSavedReturnsEmpty(t *testing.T) {
	store := newMemTokenStore()
	if got := loadBackendTokenFrom(store, "never-configured"); got != "" {
		t.Errorf("loadBackendTokenFrom for a never-saved backend = %q, want empty", got)
	}
}

// TestSaveBackendTokenTo_EmptyTokenDeletesEntry covers clearing a
// backend's token (e.g. the user blanks the token field and saves): it
// must actually remove the Keychain entry, not store an empty string that
// a later Load would treat as "no token" anyway but that still leaves a
// stale item behind.
func TestSaveBackendTokenTo_EmptyTokenDeletesEntry(t *testing.T) {
	store := newMemTokenStore()
	saveBackendTokenTo(store, "office", "abc123")
	saveBackendTokenTo(store, "office", "")

	if _, ok := store.data["bearer:office"]; ok {
		t.Error("saveBackendTokenTo with an empty token left a Keychain entry behind")
	}
}
