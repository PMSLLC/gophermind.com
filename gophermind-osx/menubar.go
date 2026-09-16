// This file wires real actions onto the menu items app.go's NewApp builds
// (.planning/tasks/04-08.json). Split from app.go because the actions
// themselves (opening the settings window, toggling the right panel,
// starting a new project) belong to ChatWindow, which doesn't exist until
// after NewApp returns -- same "build now, wire later" shape main.go's
// sendTurn closure already uses.
package main

/*
#include <ui.h>

extern void goMenuItemClicked(void *item, void *window, void *data);

static inline void attachMenuItemClicked(uiMenuItem *item, long long h) {
	uiMenuItemOnClicked(item, (void (*)(uiMenuItem *, uiWindow *, void *))goMenuItemClicked, (void *)h);
}

// asVoidPtr does the long long -> void* cast on the C side, the same way
// every attachXClicked helper elsewhere in this package does it -- so
// menuActionHandleForTest's Go code never does its own uintptr->
// unsafe.Pointer conversion, which go vet flags as a possible misuse.
static inline void *asVoidPtr(long long h) {
	return (void *)h;
}
*/
import "C"

import (
	"sync"
	"unsafe"
)

var (
	menuActionMu     sync.Mutex
	menuActions      = map[C.longlong]func(){}
	menuItemHandles  = map[*C.uiMenuItem]C.longlong{}
	nextMenuActionID C.longlong
)

// wireMenuItem attaches action to item's click, via the shared
// goMenuItemClicked trampoline (one exported callback for every menu item
// this app wires, distinguished by handle -- same registry pattern as
// queueMain's dispatch in chatinput.go).
func wireMenuItem(item *C.uiMenuItem, action func()) {
	if item == nil || action == nil {
		return
	}
	menuActionMu.Lock()
	h := nextMenuActionID
	nextMenuActionID++
	menuActions[h] = action
	menuItemHandles[item] = h
	menuActionMu.Unlock()
	C.attachMenuItemClicked(item, h)
}

// menuActionHandleForTest returns the data pointer goMenuItemClicked would
// receive for item's own click, so menubar_test.go can drive that exact
// callback directly (a real click on a real menu bar isn't reachable in a
// headless test run). Exists for the same reason chatview.go/rightpanel.go
// expose their own test-only lookups: Go's cgo forbids import "C" in
// _test.go files entirely, so the handle lookup has to live here.
func menuActionHandleForTest(item *C.uiMenuItem) unsafe.Pointer {
	menuActionMu.Lock()
	h := menuItemHandles[item]
	menuActionMu.Unlock()
	return C.asVoidPtr(h)
}

//export goMenuItemClicked
func goMenuItemClicked(item unsafe.Pointer, window unsafe.Pointer, data unsafe.Pointer) {
	h := C.longlong(uintptr(data))
	menuActionMu.Lock()
	action, ok := menuActions[h]
	menuActionMu.Unlock()
	if ok {
		action()
	}
}

// WireMenuActions attaches real behavior to every menu item NewApp built
// that needs one beyond what libui-ng already wires itself (Quit/About/
// Preferences' default behavior is Quit's uiOnShouldQuit callback and, for
// About/Preferences, simply showing their standard system panel or, for
// Preferences with a click handler attached, running that handler instead
// -- see uiMenuAppendPreferencesItem's doc comment). Any nil func leaves
// that item a no-op, same nil-injected-action precedent as every other
// phase-4 widget in this codebase.
func (a *App) WireMenuActions(onNewProject, onOpenBrief, onSettings, onTogglePanel, onToggleDarkMode func()) {
	wireMenuItem(a.NewProjectItem, onNewProject)
	wireMenuItem(a.OpenBriefItem, onOpenBrief)
	wireMenuItem(a.SettingsItem, onSettings)
	wireMenuItem(a.TogglePanelItem, onTogglePanel)
	wireMenuItem(a.ToggleDarkModeItem, onToggleDarkMode)
}
