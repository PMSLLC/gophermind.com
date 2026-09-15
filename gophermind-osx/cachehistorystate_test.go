package main

import (
	"path/filepath"
	"testing"

	appui "gophermind/gophermind-osx/ui"
)

func TestLoadCacheHistorySettingsFrom_MissingFileReturnsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	got := loadCacheHistorySettingsFrom(path)
	if got != appui.DefaultCacheHistorySettings() {
		t.Errorf("loadCacheHistorySettingsFrom on a missing file = %+v, want defaults", got)
	}
}

func TestSaveLoadCacheHistorySettingsFrom_RoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "cache-history.json")

	s := appui.CacheHistorySettings{MaxHistoryMessages: 250, PersistHistory: false}
	saveCacheHistorySettingsTo(path, s)

	got := loadCacheHistorySettingsFrom(path)
	if got != s {
		t.Errorf("loadCacheHistorySettingsFrom = %+v, want %+v", got, s)
	}
}
