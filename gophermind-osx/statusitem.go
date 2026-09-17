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

// statusItemCreate prefers the gopher glyph at iconPath, loaded as an AppKit
// "template image" so the system recolors it for light/dark menu bars and
// the highlighted state -- the same reason every built-in menu-bar icon
// (Wi-Fi, battery, ...) works correctly in both appearances. Falls back to
// a plain text title when iconPath is empty or fails to load (e.g. a dev
// build run before the icon was bundled/copied next to the binary -- see
// findMenubarIcon), so a missing asset degrades to something usable rather
// than an invisible status item.
static inline void statusItemCreate(const char *title, const char *iconPath) {
	gStatusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];

	NSImage *icon = nil;
	if (iconPath != NULL && iconPath[0] != '\0') {
		icon = [[NSImage alloc] initWithContentsOfFile:[NSString stringWithUTF8String:iconPath]];
	}
	if (icon != nil) {
		icon.size = NSMakeSize(18, 18);
		icon.template = YES;
		gStatusItem.button.image = icon;
	} else {
		gStatusItem.button.title = [NSString stringWithUTF8String:title];
	}
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
	"os"
	"path/filepath"
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
	iconPath := C.CString(findMenubarIcon())
	defer C.free(unsafe.Pointer(iconPath))
	C.statusItemCreate(title, iconPath)

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

// findMenubarIcon locates the status-bar glyph (design/gopher-menubar-icon.svg,
// rasterized to menubar-icon.png), same search order and same bundle-vs-dev-
// layout reasoning as findExamplesDir/findServerBinary: GOPHERMIND_MENUBAR_ICON
// override, then the .app bundle's Resources directory (build-app.sh copies
// menubar-icon.png there, sibling to gophermind-server -- see that script's
// own copy step), then the bare dev-checkout path relative to the current
// directory. Returns "" (not an error) when not found: statusItemCreate
// already degrades to a plain text title, so there's nothing for a caller to
// react to.
func findMenubarIcon() string {
	if p := os.Getenv("GOPHERMIND_MENUBAR_ICON"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		for _, candidate := range []string{
			filepath.Join(dir, "menubar-icon.png"),
			filepath.Join(dir, "..", "Resources", "menubar-icon.png"),
		} {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	if _, err := os.Stat("menubar-icon.png"); err == nil {
		return "menubar-icon.png"
	}
	return ""
}
