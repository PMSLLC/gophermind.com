// This file adds a macOS menu-bar (status bar) item listing every
// configured gophermind backend and its live connection status -- a
// separate surface from the app's own File/Edit/View menu bar (app.go),
// which lives in the app's main menu, not the system status bar at the
// top-right of the screen. libui-ng has no status-item API at all
// (checked ui.h: no NSStatusItem/NSStatusBar equivalent anywhere), so
// this reaches directly into AppKit, the same way app.go's Maximize does.
//
// v1 is informational plus two real actions: a disabled line per backend
// ("name -- status"), refreshed live via BackendListState.OnChange, and
// working Show Window / Quit items. Per-backend actions (click to
// connect/disconnect a specific backend from the menu bar) are
// deliberately deferred: wiring a click callback to an arbitrary
// NSMenuItem requires either a custom Objective-C class (objc_allocateClassPair)
// or an Objective-C block bridged via imp_implementationWithBlock, real
// added complexity this first pass doesn't need -- Show Window and Quit
// both work today because NSWindow/NSApplication already implement
// makeKeyAndOrderFront:/terminate: as real, existing selectors, needing
// no custom target at all.
package main

/*
#include <ui.h>
#include <stdint.h>
#import <AppKit/AppKit.h>

// gStatusItem is a single package-level NSStatusItem: this app is a
// single-instance process (see App's own doc comment on libui-ng being a
// global, single-instance library), so there is never more than one to
// track.
static NSStatusItem *gStatusItem;

static inline void statusItemCreate(const char *title) {
	gStatusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
	gStatusItem.button.title = [NSString stringWithUTF8String:title];
	gStatusItem.menu = [[NSMenu alloc] init];
}

static inline void statusItemClearMenu(void) {
	[gStatusItem.menu removeAllItems];
}

// statusItemAddLabel adds a disabled, action-less line -- one configured
// backend's "name -- status" display.
static inline void statusItemAddLabel(const char *label) {
	NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:[NSString stringWithUTF8String:label]
	                                               action:nil
	                                        keyEquivalent:@""];
	[item setEnabled:NO];
	[gStatusItem.menu addItem:item];
}

static inline void statusItemAddSeparator(void) {
	[gStatusItem.menu addItem:[NSMenuItem separatorItem]];
}

// statusItemAddActionItem adds a real, clickable item whose target is an
// existing AppKit object (an NSWindow or NSApplication) and whose action
// is one of that object's own existing selectors -- see this file's top
// doc comment on why no custom target/action class is needed for v1.
static inline void statusItemAddActionItem(const char *label, uintptr_t target, const char *selName) {
	NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:[NSString stringWithUTF8String:label]
	                                               action:NSSelectorFromString([NSString stringWithUTF8String:selName])
	                                        keyEquivalent:@""];
	[item setTarget:(id)target];
	[gStatusItem.menu addItem:item];
}

static inline uintptr_t statusItemNSApp(void) {
	return (uintptr_t)[NSApplication sharedApplication];
}
*/
import "C"

import (
	"unsafe"

	appui "gophermind/gophermind-osx/ui"
)

// formatBackendMenuLabel renders one backend's status-bar menu line.
// Plain string formatting, pulled out of statusItem.refresh so it's
// testable without building any AppKit widgets.
func formatBackendMenuLabel(name, status string) string {
	return name + " -- " + status
}

// statusItem is the menu-bar item's Go side: the backend list it renders
// and the window its "Show Window" item targets.
type statusItem struct {
	backends *appui.BackendListState
	window   *C.uiWindow
}

// newStatusItem creates the status-bar item and keeps it in sync with
// backends for the app's lifetime (BackendListState.OnChange fires on
// every Add/Remove/SetStatus/SetActive, matching the pattern PanelState
// and every other *State type in this codebase already use).
func newStatusItem(backends *appui.BackendListState, window *C.uiWindow) *statusItem {
	title := C.CString("gophermind")
	defer C.free(unsafe.Pointer(title))
	C.statusItemCreate(title)

	si := &statusItem{backends: backends, window: window}
	backends.OnChange(si.refresh)
	si.refresh()
	return si
}

// refresh rebuilds the status item's menu from scratch: one disabled line
// per configured backend, a separator, then Show Window and Quit.
func (si *statusItem) refresh() {
	C.statusItemClearMenu()

	for _, p := range si.backends.Profiles() {
		label := formatBackendMenuLabel(p.Name, si.backends.Status(p.Name))
		cLabel := C.CString(label)
		C.statusItemAddLabel(cLabel)
		C.free(unsafe.Pointer(cLabel))
	}

	C.statusItemAddSeparator()

	windowHandle := C.uiControlHandle((*C.uiControl)(unsafe.Pointer(si.window)))
	addAction(windowHandle, "Show Window", "makeKeyAndOrderFront:")
	addAction(C.statusItemNSApp(), "Quit", "terminate:")
}

// addAction is a small unsafe.Pointer-free helper so refresh doesn't
// repeat the same three C.CString/C.free pairs twice.
func addAction(target C.uintptr_t, label, selector string) {
	cLabel := C.CString(label)
	defer C.free(unsafe.Pointer(cLabel))
	cSel := C.CString(selector)
	defer C.free(unsafe.Pointer(cSel))
	C.statusItemAddActionItem(cLabel, target, cSel)
}
