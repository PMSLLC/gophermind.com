package main

// Like the other widget-assembly test files, this only proves the
// settings panel's widgets build and respond to actions without
// panicking, routed through runOnUIThread for the same
// AppKit-single-thread reason -- whether it actually LOOKS right needs a
// human at a real window.
//
// Unlike sessionlist.go's uiOpenFolder or pipelinepanel.go's uiOpenFile,
// doOpen() here never calls a native dialog -- the settings window is a
// plain uiWindow this file builds itself -- so it's safe to call directly
// in a test, same as confirmWindow would be if a test needed it.

import (
	"context"
	"errors"
	"testing"
	"time"

	"gophermind/gophermind-lib/modelcat"
	"gophermind/gophermind-lib/skills"
	appui "gophermind/gophermind-osx/ui"
)

func TestNewChatWindow_BuildsSettingsPanelWithoutPanic(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var err error
	var settingsNil bool
	runOnUIThread(t, func() {
		var app *App
		app, err = NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			return
		}
		defer app.Close()
		cw := NewChatWindow(app, func(string) {})
		settingsNil = cw.Backends == nil
		cw.settingsUI.doOpen() // must not panic with every func nil
		defer cw.settingsUI.destroyForTest()
	})
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	if settingsNil {
		t.Fatal("ChatWindow.Backends is nil")
	}
}

func TestSettingsPanel_AddConnectDisconnectRemoveBackend(t *testing.T) {
	runOnUIThread(t, func() {
		app, err := NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			t.Fatalf("NewApp: %v", err)
		}
		defer app.Close()

		backends := appui.NewBackendListState()
		model := appui.NewModelPickerState(nil, modelcat.Settings{})
		var connected string
		connectFn := func(ctx context.Context, p appui.BackendProfile) error {
			connected = p.Name
			return nil
		}
		var disconnected string
		disconnectFn := func(name string) { disconnected = name }

		sp := newSettingsPanel(app.window, backends, model, appui.DefaultCacheHistorySettings(), nil,
			connectFn, disconnectFn, nil, nil, nil, nil, nil, nil)
		attachControlForTest(app.window, sp.GearControl())
		sp.doOpen()
		defer sp.destroyForTest()

		setEntryText(sp.backendNameEntry, "home")
		setEntryText(sp.backendURLEntry, "https://example.com")
		sp.doAddBackend()

		if len(backends.Profiles()) != 1 || backends.Profiles()[0].Name != "home" {
			t.Fatalf("Profiles() = %+v, want one entry named home", backends.Profiles())
		}

		sp.doConnect()
		deadline := time.Now().Add(2 * time.Second)
		for connected != "home" && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if connected != "home" {
			t.Fatal("connectFn was never called")
		}
		if backends.Status("home") == "disconnected" {
			t.Error("Status() should not still be disconnected after a successful connect")
		}

		sp.doDisconnect()
		if disconnected != "home" {
			t.Errorf("disconnected = %q, want home", disconnected)
		}
		if backends.Status("home") != "disconnected" {
			t.Errorf("Status() = %q, want disconnected", backends.Status("home"))
		}

		sp.doRemoveBackend()
		if len(backends.Profiles()) != 0 {
			t.Errorf("Profiles() = %+v, want empty after remove", backends.Profiles())
		}
	})
}

func TestSettingsPanel_ApplyModelSettingsWithNilPatchFuncDoesNotPanic(t *testing.T) {
	runOnUIThread(t, func() {
		app, err := NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			t.Fatalf("NewApp: %v", err)
		}
		defer app.Close()

		model := appui.NewModelPickerState(nil, modelcat.Settings{})
		sp := newSettingsPanel(app.window, appui.NewBackendListState(), model, appui.DefaultCacheHistorySettings(), nil,
			nil, nil, nil, nil, nil, nil, nil, nil)
		attachControlForTest(app.window, sp.GearControl())
		sp.doOpen()
		defer sp.destroyForTest()
		sp.doApplyModelSettings() // patchModelFunc == nil: must not panic
	})
}

func TestSettingsPanel_SkillsErrorFromListDoesNotPanic(t *testing.T) {
	runOnUIThread(t, func() {
		app, err := NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			t.Fatalf("NewApp: %v", err)
		}
		defer app.Close()

		model := appui.NewModelPickerState(nil, modelcat.Settings{})
		listFn := func(ctx context.Context) ([]skills.Source, []skills.Skill, error) {
			return nil, nil, errors.New("boom")
		}
		sp := newSettingsPanel(app.window, appui.NewBackendListState(), model, appui.DefaultCacheHistorySettings(), nil,
			nil, nil, nil, listFn, nil, nil, nil, nil)
		attachControlForTest(app.window, sp.GearControl())
		sp.doOpen() // refreshSkills sees the error: must not panic
		defer sp.destroyForTest()
	})
}

func TestSettingsPanel_SaveCacheHistoryCallsInjectedSave(t *testing.T) {
	runOnUIThread(t, func() {
		app, err := NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			t.Fatalf("NewApp: %v", err)
		}
		defer app.Close()

		var saved appui.CacheHistorySettings
		saveFn := func(s appui.CacheHistorySettings) { saved = s }
		model := appui.NewModelPickerState(nil, modelcat.Settings{})
		sp := newSettingsPanel(app.window, appui.NewBackendListState(), model, appui.DefaultCacheHistorySettings(), saveFn,
			nil, nil, nil, nil, nil, nil, nil, nil)
		attachControlForTest(app.window, sp.GearControl())
		sp.doOpen()
		defer sp.destroyForTest()
		sp.doSaveCacheHistory()

		if saved != appui.DefaultCacheHistorySettings() {
			t.Errorf("saved = %+v, want the panel's current (default) values", saved)
		}
	})
}
