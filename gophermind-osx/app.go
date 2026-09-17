// Package main is gophermind-osx, the native macOS desktop app for
// gophermind (.planning/ROADMAP.md Phase 3): a libui-ng GUI that talks to
// gophermind-server over userspace WireGuard.
//
// This file (.planning/tasks/03-01.json) is the app skeleton: libui-ng
// initialization, main window creation, app lifecycle (quit on window
// close or Cmd+Q), and a minimal menu bar. It intentionally shows an empty
// window -- chat, panels, and settings are Phase 4's job.
//
// libui-ng is not vendored or fetched as a Go module: no Go binding
// compatible with this machine's architecture exists (github.com/andlabs/ui,
// the obvious candidate, bundles only a 2020-era amd64 static library for
// darwin, with no arm64 build -- broken on Apple Silicon). The library
// itself is built from source and installed to /opt/homebrew (see
// docs/DEPLOYMENT.md or the session that did this) as a universal
// (x86_64+arm64) dylib; this file binds directly to its C API via cgo,
// covering only the small surface 03-01 needs.
package main

/*
#include <ui.h>
#include <stdlib.h>
#include <stdint.h>
#import <AppKit/AppKit.h>

// Trampolines: libui's callback registration functions take a plain C
// function pointer, which cgo cannot construct directly from a Go func
// value. Each callback is instead a small C shim calling into a Go
// function exported via //export, matching the standard cgo pattern for
// C-library callbacks.
extern int goWindowOnClosing(uiWindow *w, void *data);
extern int goShouldQuit(void *data);

static inline void attachOnClosing(uiWindow *w) {
	uiWindowOnClosing(w, (int (*)(uiWindow *, void *))goWindowOnClosing, NULL);
}

static inline void attachShouldQuit(void) {
	uiOnShouldQuit((int (*)(void *))goShouldQuit, NULL);
}

// windowZoom/windowIsZoomed reach through libui-ng's documented, public
// uiControlHandle (an OS-level handle -- on this Cocoa backend, the real
// NSWindow*) to call AppKit's own -zoom:, the exact action the window's
// green button performs. libui-ng has no "fill the screen" API of its own
// to call instead (see App.Maximize's doc comment).
static inline void windowZoom(uintptr_t handle) {
	[(NSWindow *)handle zoom:nil];
}

static inline int windowIsZoomed(uintptr_t handle) {
	return [(NSWindow *)handle isZoomed] ? 1 : 0;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// DefaultTitle and DefaultWidth/DefaultHeight are the main window's initial
// title and content size, per 03-01's acceptance criteria ("titled
// 'gophermind'", "1200x800 or similar").
const (
	DefaultTitle  = "gophermind"
	DefaultWidth  = 1200
	DefaultHeight = 800
)

// App wraps one libui-ng application: the initialized library, the main
// window, and the app-level menu. Only one App may exist at a time --
// libui-ng itself is a global, single-instance C library (uiInit/uiUninit
// affect global state), so this is a constraint of the library, not a
// design choice made here.
type App struct {
	window *C.uiWindow

	// NewProjectItem/OpenBriefItem/SettingsItem/TogglePanelItem/
	// ToggleDarkModeItem are exposed so a caller can attach real actions
	// once they exist (menubar.go's WireMenuActions) -- the menu bar
	// itself must be built here, before the window (see below), but the
	// actions it triggers (opening a new project, showing the settings
	// window, toggling the right panel) all belong to ChatWindow, which
	// isn't built until after NewApp returns. Same "build now, wire later"
	// shape as main.go's sendTurn closure trick.
	NewProjectItem     *C.uiMenuItem
	OpenBriefItem      *C.uiMenuItem
	ExamplesItem       *C.uiMenuItem
	SettingsItem       *C.uiMenuItem
	TogglePanelItem    *C.uiMenuItem
	ToggleDarkModeItem *C.uiMenuItem
}

// NewApp initializes libui-ng and creates the main window with a full menu
// bar (.planning/tasks/04-08.json), but does not show the window or start
// the event loop -- see Show and Run. Returns an error (never panics) if
// uiInit fails, e.g. on a platform/session with no usable display.
//
// Menu bar shape follows libui-ng's own documented cross-platform example
// (ui.h's uiMenu doc comment): About and Preferences (Settings) join Quit
// in the app's own menu (uiMenuAppendAboutItem/uiMenuAppendPreferencesItem/
// uiMenuAppendQuitItem place themselves per-OS convention regardless of
// which uiMenu they're nominally appended to -- checked against ui.h's own
// note that "the various operating systems impose different requirements
// on placement" of these three specifically), File carries New Project and
// Open Brief (a real Quit already exists in the app menu, so File does not
// duplicate it), View carries Toggle Panel and Toggle Dark Mode, and Edit/
// Window/Help exist but stay empty: libui-ng exposes no API to wire
// Cut/Copy/Paste to a text field's real editing actions or to add the
// standard Minimize/Zoom items, so an empty menu is an honest placeholder
// rather than a fabricated one.
//
// uiMenuItem also has no key-equivalent/accelerator API (see
// gophermind-osx/ui/input.go's ShouldSend doc comment, and rightpanel.go's
// top comment, for the same finding already made against this exact
// header) -- so none of 04-08's "Cmd+Enter send, Y/N approve, Cmd+\ panel,
// Cmd+, settings, Cmd+N new project" keyboard shortcuts can be wired
// through this menu (or anywhere else in ui.h). Every one of those actions
// is reachable only through a visible button/menu click, same as Cmd+Enter
// send already was in 04-01.
func NewApp(title string, width, height int) (*App, error) {
	var opts C.uiInitOptions
	if cErr := C.uiInit(&opts); cErr != nil {
		return nil, fmt.Errorf("uiInit: %s", C.GoString(cErr))
	}

	// The menu must be created before the window that carries it (hasMenubar
	// = 1 below) -- libui-ng's darwin backend builds the app's menu bar from
	// whatever uiMenu instances exist at uiNewWindow time.
	appMenu := C.uiNewMenu(C.CString("gophermind"))
	C.uiMenuAppendAboutItem(appMenu)
	C.uiMenuAppendSeparator(appMenu)
	settingsItem := C.uiMenuAppendPreferencesItem(appMenu)
	C.uiMenuAppendSeparator(appMenu)
	C.uiMenuAppendQuitItem(appMenu) // wires Cmd+Q to uiOnShouldQuit's callback

	fileMenu := C.uiNewMenu(C.CString("File"))
	newProjectItem := C.uiMenuAppendItem(fileMenu, C.CString("New Project"))
	openBriefItem := C.uiMenuAppendItem(fileMenu, C.CString("Open Brief"))
	examplesItem := C.uiMenuAppendItem(fileMenu, C.CString("Examples"))

	C.uiNewMenu(C.CString("Edit")) // standard editing actions: see doc comment above

	viewMenu := C.uiNewMenu(C.CString("View"))
	togglePanelItem := C.uiMenuAppendItem(viewMenu, C.CString("Toggle Panel"))
	toggleDarkModeItem := C.uiMenuAppendItem(viewMenu, C.CString("Toggle Dark Mode"))

	C.uiNewMenu(C.CString("Window")) // standard window actions: see doc comment above
	C.uiNewMenu(C.CString("Help"))   // About already lives in the app menu, per macOS convention

	cTitle := C.CString(title)
	defer C.free(unsafe.Pointer(cTitle))
	window := C.uiNewWindow(cTitle, C.int(width), C.int(height), 1)
	// uiWindowSetMargined is called from Show (below), after uiControlShow,
	// not here on the freshly created window. Measured cause: an
	// intermittent (~5%, reproduces on this machine with or without
	// margining -- confirmed by an A/B run of ~20 launches each) native
	// crash inside AppKit's own window-manager code (lldb backtrace:
	// uiControlShow -> -[NSWindow makeKeyAndOrderFront:] -> ... ->
	// -[NSWMWindowCoordinator performTransactionUsingBlock:],
	// EXC_BREAKPOINT), pre-existing and unrelated to margining. Margining
	// after the window is already shown and realized is still the more
	// conservative ordering -- mutating window chrome before the window
	// has a live backing store is exactly the kind of thing that class of
	// bug tends to live in -- so it stays deferred to Show even though it
	// isn't a proven fix for this specific crash.

	C.attachOnClosing(window)
	C.attachShouldQuit()

	return &App{
		window:             window,
		NewProjectItem:     newProjectItem,
		OpenBriefItem:      openBriefItem,
		ExamplesItem:       examplesItem,
		SettingsItem:       settingsItem,
		TogglePanelItem:    togglePanelItem,
		ToggleDarkModeItem: toggleDarkModeItem,
	}, nil
}

// Title returns the window's current title.
func (a *App) Title() string {
	cTitle := C.uiWindowTitle(a.window)
	defer C.uiFreeText(cTitle)
	return C.GoString(cTitle)
}

// ContentSize returns the window's current content size.
func (a *App) ContentSize() (width, height int) {
	var w, h C.int
	C.uiWindowContentSize(a.window, &w, &h)
	return int(w), int(h)
}

// SetContentSize resizes the window's content area, e.g. restoring a
// persisted size (.planning/tasks/04-08.json's "window state: size,
// position ... persisted across app restarts").
func (a *App) SetContentSize(width, height int) {
	C.uiWindowSetContentSize(a.window, C.int(width), C.int(height))
}

// Position returns the window's current on-screen position.
func (a *App) Position() (x, y int) {
	var cx, cy C.int
	C.uiWindowPosition(a.window, &cx, &cy)
	return int(cx), int(cy)
}

// SetPosition moves the window, e.g. restoring a persisted position.
func (a *App) SetPosition(x, y int) {
	C.uiWindowSetPosition(a.window, C.int(x), C.int(y))
}

// Show makes the main window visible. Separate from NewApp so a caller (or
// a test) can inspect/adjust the window before it's shown.
func (a *App) Show() {
	C.uiControlShow((*C.uiControl)(unsafe.Pointer(a.window)))
	// Margined AFTER Show, not at window creation -- see NewApp's doc
	// comment on the window-creation line for why margining before the
	// first Show crashes on this libui-ng/AppKit combination. Gives the
	// window's content breathing room around its edges (uiBoxSetPadded
	// alone only spaces siblings apart, not the outermost edge).
	C.uiWindowSetMargined(a.window, 1)
}

// Maximize toggles the window between its current frame and one that
// fills the screen -- the same effect as clicking the window's green
// zoom button, or double-clicking its title bar. libui-ng exposes no
// "fill the screen" sizing of its own (checked ui.h: there is no uiScreen
// API at all, nothing to query a display's size from), so this reaches
// through uiControlHandle to the real NSWindow and calls AppKit's own
// -zoom: directly. Call after Show: zooming a window that has never been
// shown is unreliable on Cocoa.
func (a *App) Maximize() {
	C.windowZoom(C.uiControlHandle((*C.uiControl)(unsafe.Pointer(a.window))))
}

// IsMaximized reports whether the window is currently zoomed (filling the
// screen), so window-state persistence (main.go) can save "maximized" as
// a state distinct from any particular width/height/position -- the same
// distinction WindowState.Maximized documents.
func (a *App) IsMaximized() bool {
	return C.windowIsZoomed(C.uiControlHandle((*C.uiControl)(unsafe.Pointer(a.window)))) != 0
}

// Run starts libui-ng's event loop and blocks until the app quits (window
// closed, Cmd+Q, or uiQuit called some other way). Requires a real display
// session; not exercised by any test in this package for that reason --
// see app_test.go's doc comment.
func (a *App) Run() {
	C.uiMain()
}

// Close tears down the app: destroys the window and uninitializes
// libui-ng. Safe to call instead of Run (e.g. in a test that only wants to
// verify NewApp/Show succeed without starting the blocking event loop).
func (a *App) Close() {
	C.uiControlDestroy((*C.uiControl)(unsafe.Pointer(a.window)))
	C.uiUninit()
}

//export goWindowOnClosing
func goWindowOnClosing(w *C.uiWindow, data unsafe.Pointer) C.int {
	C.uiQuit()
	return 1 // destroy the window
}

//export goShouldQuit
func goShouldQuit(data unsafe.Pointer) C.int {
	C.uiQuit()
	return 1
}
