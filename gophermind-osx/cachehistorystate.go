// This file persists CacheHistorySettings (.planning/tasks/04-07.json's
// "cache/history: options configurable") as a small JSON file under the
// user's config dir, same pattern as panelstate.go.
package main

import (
	"os"
	"path/filepath"

	appui "gophermind/gophermind-osx/ui"
)

// cacheHistoryFile returns the on-disk path for the persisted
// cache/history settings.
func cacheHistoryFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gophermind-osx", "cache-history.json"), nil
}

// loadCacheHistorySettingsFrom reads settings from path, returning
// appui.DefaultCacheHistorySettings() for a missing file or any read/parse
// error -- same first-run/corrupt-file tolerance as loadPanelStateFrom.
func loadCacheHistorySettingsFrom(path string) appui.CacheHistorySettings {
	f, err := os.Open(path)
	if err != nil {
		return appui.DefaultCacheHistorySettings()
	}
	defer f.Close()

	s, err := appui.LoadCacheHistorySettings(f)
	if err != nil {
		return appui.DefaultCacheHistorySettings()
	}
	return s
}

// saveCacheHistorySettingsTo writes s to path, creating parent directories
// as needed. Best-effort, same tolerance as savePanelStateTo.
func saveCacheHistorySettingsTo(path string, s appui.CacheHistorySettings) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	s.Save(f)
}

// loadCacheHistorySettings and saveCacheHistorySettings are
// loadCacheHistorySettingsFrom/saveCacheHistorySettingsTo against the real
// default path, for NewChatWindow to use.

func loadCacheHistorySettings() appui.CacheHistorySettings {
	path, err := cacheHistoryFile()
	if err != nil {
		return appui.DefaultCacheHistorySettings()
	}
	return loadCacheHistorySettingsFrom(path)
}

func saveCacheHistorySettings(s appui.CacheHistorySettings) {
	path, err := cacheHistoryFile()
	if err != nil {
		return
	}
	saveCacheHistorySettingsTo(path, s)
}
