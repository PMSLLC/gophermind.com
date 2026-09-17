// This file holds gophermind-osx's local-state plumbing shared by every
// *state.go file in this package (panelstate.go, windowstate.go,
// cachehistorystate.go, backendstore.go): where those files' bytes live
// on disk, and the open/create/best-effort-write logic each of them used
// to duplicate individually.
//
// State lives under gophermind-lib/config.Dir() (~/.gophermind by
// default, or $GOPHERMIND_CONFIG_DIR), not the OS's own per-app config
// directory (~/Library/Application Support/gophermind-osx on macOS): a
// reinstall or an app-bundle upgrade replaces everything under the .app
// itself and may not preserve or even know about the OS config
// directory, but it can never touch a user's home directory contents.
// Using the same directory the CLI and server already use also means one
// place to look for every gophermind tool's local state, not three.
package main

import (
	"io"
	"os"
	"path/filepath"

	"gophermind/gophermind-lib/config"
)

// osxStateDir returns the directory gophermind-osx keeps its own local
// state in: an "osx" subdirectory of gophermind-lib/config.Dir(), so this
// app's files (window.json, panel-state.json, ...) don't mix with the
// CLI/server's own (config.json, sessions/, tokens/, history) in the same
// top-level directory.
func osxStateDir() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "osx"), nil
}

// migrateLegacyOSXState moves gophermind-osx's local state files from
// their pre-consolidation location (the OS's own per-app config
// directory, e.g. ~/Library/Application Support/gophermind-osx on macOS)
// to osxStateDir(), so an existing user upgrading to this version doesn't
// silently lose window position, panel state, and cache/history
// preferences. Best-effort and safe to call on every startup: a missing
// old directory, an already-migrated new directory, or any error along
// the way just leaves things as they are (see migrateOSXStateBetween).
func migrateLegacyOSXState() {
	oldBase, err := os.UserConfigDir()
	if err != nil {
		return
	}
	newDir, err := osxStateDir()
	if err != nil {
		return
	}
	migrateOSXStateBetween(filepath.Join(oldBase, "gophermind-osx"), newDir)
}

// migrateOSXStateBetween moves every file in oldDir into newDir, same
// non-destructive, resumable shape as gophermind-lib/config.Migrate(): a
// file already present at the destination wins (never overwritten), a
// missing or empty old directory is a silent no-op, and the old directory
// is removed only once it's empty (os.Remove fails harmlessly otherwise,
// leaving anything unmigrated in place for the next run to retry).
func migrateOSXStateBetween(oldDir, newDir string) {
	if oldDir == newDir {
		return
	}
	entries, err := os.ReadDir(oldDir)
	if err != nil || len(entries) == 0 {
		return
	}
	if err := os.MkdirAll(newDir, 0o700); err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		dst := filepath.Join(newDir, e.Name())
		if _, err := os.Stat(dst); err == nil {
			continue // destination already has it
		}
		os.Rename(filepath.Join(oldDir, e.Name()), dst)
	}
	os.Remove(oldDir) // no-op (fails silently) if anything was left behind
}

// loadState reads path and decodes it with decode, returning zero for a
// missing file or any read/parse error -- a corrupt or absent state file
// must never block the app from starting.
func loadState[T any](path string, decode func(io.Reader) (T, error), zero T) T {
	f, err := os.Open(path)
	if err != nil {
		return zero
	}
	defer f.Close()
	v, err := decode(f)
	if err != nil {
		return zero
	}
	return v
}

// saveState writes to path via encode, creating its parent directory
// (0700: this directory can hold backends.json, naming configured
// servers and gocloak realms, which is user-private config even though
// it carries no secrets itself -- those live in the Keychain) as needed.
// Best-effort: a failure here (e.g. a read-only config dir) is not fatal
// to the app, and this package has no logger to report it to.
func saveState(path string, encode func(io.Writer) error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	encode(f)
}
