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
#cgo CFLAGS: -I/opt/homebrew/include
#cgo LDFLAGS: -L/opt/homebrew/lib -lui
#include <ui.h>
#include <stdlib.h>

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
}

// NewApp initializes libui-ng and creates the main window with a minimal
// menu bar (currently just the platform-standard Quit item), but does not
// show the window or start the event loop -- see Show and Run. Returns an
// error (never panics) if uiInit fails, e.g. on a platform/session with no
// usable display.
func NewApp(title string, width, height int) (*App, error) {
	var opts C.uiInitOptions
	if cErr := C.uiInit(&opts); cErr != nil {
		return nil, fmt.Errorf("uiInit: %s", C.GoString(cErr))
	}

	// The menu must be created before the window that carries it (hasMenubar
	// = 1 below) -- libui-ng's darwin backend builds the app's menu bar from
	// whatever uiMenu instances exist at uiNewWindow time.
	appMenu := C.uiNewMenu(C.CString("gophermind"))
	C.uiMenuAppendQuitItem(appMenu) // wires Cmd+Q to uiOnShouldQuit's callback

	cTitle := C.CString(title)
	defer C.free(unsafe.Pointer(cTitle))
	window := C.uiNewWindow(cTitle, C.int(width), C.int(height), 1)

	C.attachOnClosing(window)
	C.attachShouldQuit()

	return &App{window: window}, nil
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

// Show makes the main window visible. Separate from NewApp so a caller (or
// a test) can inspect/adjust the window before it's shown.
func (a *App) Show() {
	C.uiControlShow((*C.uiControl)(unsafe.Pointer(a.window)))
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
