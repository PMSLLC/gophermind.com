// This file persists CacheHistorySettings (.planning/tasks/04-07.json's
// "cache/history: options configurable") as a small JSON file under
// gophermind-osx's local state directory (see localstate.go), same
// pattern as panelstate.go.
package main

import (
	"path/filepath"

	appui "gophermind/gophermind-osx/ui"
)

// cacheHistoryFile returns the on-disk path for the persisted
// cache/history settings.
func cacheHistoryFile() (string, error) {
	dir, err := osxStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cache-history.json"), nil
}

// loadCacheHistorySettingsFrom reads settings from path, returning
// appui.DefaultCacheHistorySettings() for a missing file or any read/parse
// error -- same first-run/corrupt-file tolerance as loadPanelStateFrom.
func loadCacheHistorySettingsFrom(path string) appui.CacheHistorySettings {
	return loadState(path, appui.LoadCacheHistorySettings, appui.DefaultCacheHistorySettings())
}

// saveCacheHistorySettingsTo writes s to path, creating parent directories
// as needed. Best-effort, same tolerance as savePanelStateTo.
func saveCacheHistorySettingsTo(path string, s appui.CacheHistorySettings) {
	saveState(path, s.Save)
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
