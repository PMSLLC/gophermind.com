// This file persists the configured backend list (.planning/tasks/
// 04-07.json's "remote backends: add/remove") across restarts: their
// non-secret connection metadata in backends.json, alongside every other
// *state.go file in this package (localstate.go), and each one's bearer
// token separately in the macOS Keychain via gophermind-osx/auth -- a
// bearer token is a credential, and credentials do not belong in a plain
// JSON file even under gophermind-osx's own private state directory.
package main

import (
	"encoding/json"
	"io"
	"path/filepath"

	"gophermind/gophermind-osx/auth"
	appui "gophermind/gophermind-osx/ui"
)

// backendProfileJSON is BackendProfile's on-disk shape in backends.json.
// Deliberately has no Token field: see this file's top doc comment.
type backendProfileJSON struct {
	Name         string `json:"name"`
	Mode         string `json:"mode"`
	ServerURL    string `json:"server_url"`
	GocloakRealm string `json:"gocloak_realm"`
}

// backendsFile returns the on-disk path for the persisted backend list.
func backendsFile() (string, error) {
	dir, err := osxStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "backends.json"), nil
}

// decodeBackendProfiles parses backends.json's contents into
// appui.BackendProfile values (Token always the zero value -- it never
// lives in this file).
func decodeBackendProfiles(r io.Reader) ([]appui.BackendProfile, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var raw []backendProfileJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := make([]appui.BackendProfile, len(raw))
	for i, p := range raw {
		out[i] = appui.BackendProfile{Name: p.Name, Mode: p.Mode, ServerURL: p.ServerURL, GocloakRealm: p.GocloakRealm}
	}
	return out, nil
}

// encodeBackendProfiles writes profiles as JSON, silently dropping each
// one's Token field -- the actual mechanism behind this file's "never
// writes a token" guarantee (there's no field in backendProfileJSON for
// one to end up in even if a caller forgets to check).
func encodeBackendProfiles(profiles []appui.BackendProfile) func(io.Writer) error {
	return func(w io.Writer) error {
		raw := make([]backendProfileJSON, len(profiles))
		for i, p := range profiles {
			raw[i] = backendProfileJSON{Name: p.Name, Mode: p.Mode, ServerURL: p.ServerURL, GocloakRealm: p.GocloakRealm}
		}
		return json.NewEncoder(w).Encode(raw)
	}
}

// loadBackendProfilesFrom reads the backend list from path, returning nil
// (no configured backends) for a missing file or any read/parse error --
// same first-run/corrupt-file tolerance as loadPanelStateFrom.
func loadBackendProfilesFrom(path string) []appui.BackendProfile {
	return loadState(path, decodeBackendProfiles, nil)
}

// saveBackendProfilesTo writes profiles to path, creating parent
// directories as needed. Best-effort, same tolerance as savePanelStateTo.
func saveBackendProfilesTo(path string, profiles []appui.BackendProfile) {
	saveState(path, encodeBackendProfiles(profiles))
}

// loadBackendProfiles and saveBackendProfiles are
// loadBackendProfilesFrom/saveBackendProfilesTo against the real default
// path, for NewChatWindow and settingspanel.go to use.

func loadBackendProfiles() []appui.BackendProfile {
	path, err := backendsFile()
	if err != nil {
		return nil
	}
	return loadBackendProfilesFrom(path)
}

func saveBackendProfiles(profiles []appui.BackendProfile) {
	path, err := backendsFile()
	if err != nil {
		return
	}
	saveBackendProfilesTo(path, profiles)
}

// --- bearer tokens: Keychain-backed, never touch backends.json ---

// saveBackendTokenTo stores name's bearer token in store under
// auth.BackendTokenKey(name), or deletes that entry entirely when token
// is empty (clearing a token must not leave a stale Keychain item behind
// that a later, unrelated Load could resurrect).
func saveBackendTokenTo(store auth.TokenStore, name, token string) {
	if token == "" {
		store.Delete(auth.BackendTokenKey(name))
		return
	}
	store.Save(auth.BackendTokenKey(name), []byte(token))
}

// loadBackendTokenFrom reads name's bearer token from store, returning ""
// (no token configured) for a never-saved backend or any store error --
// same first-run tolerance as every other loader in this package.
func loadBackendTokenFrom(store auth.TokenStore, name string) string {
	data, err := store.Load(auth.BackendTokenKey(name))
	if err != nil {
		return ""
	}
	return string(data)
}

// saveBackendToken and loadBackendToken are
// saveBackendTokenTo/loadBackendTokenFrom against the real Keychain, for
// NewChatWindow and settingspanel.go to use.

func saveBackendToken(name, token string) {
	saveBackendTokenTo(auth.NewKeychainStore(), name, token)
}

func loadBackendToken(name string) string {
	return loadBackendTokenFrom(auth.NewKeychainStore(), name)
}
