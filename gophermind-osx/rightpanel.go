// This file is the right panel's widget-assembly layer (.planning/tasks/
// 04-03.json), same split as chatview.go/chatinput.go: gophermind-osx/ui
// (PanelState, in panel.go) holds the plain-Go collapsed/expanded state and
// its persistence format; this file turns that into real libui-ng widgets.
//
// The panel is toggle-only, with no drag-resize and no collapse/expand
// animation -- see appui.PanelState's doc comment for why (libui-ng has no
// splitter widget, no animation API, and -- checked against the installed
// ui.h -- no way to constrain a uiBox/uiGroup to a minimum or fixed width
// either, so its width is just whatever its section content naturally
// sizes to). Model, Sessions, and Pipeline all carry their real content
// (04-04, 04-05, 04-06).
package main

/*
#cgo CFLAGS: -I/opt/homebrew/include
#cgo LDFLAGS: -L/opt/homebrew/lib -lui
#include <ui.h>
#include <stdlib.h>

extern void goPanelToggleClicked(void *button, void *data);

static inline void attachPanelToggleClicked(uiButton *b, long long handle) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goPanelToggleClicked, (void *)handle);
}
*/
import "C"

import (
	"sync"
	"unsafe"

	appui "gophermind/gophermind-osx/ui"
)

// rightPanel is the collapsible right panel: a toggle button (always
// visible, in the chat column -- see NewChatWindow) plus a vertical stack
// of section groups, shown/hidden as a whole per state.Collapsed.
type rightPanel struct {
	state        *appui.PanelState
	box          *C.uiBox // the panel's own content, hidden/shown as a unit
	toggleButton *C.uiButton
}

var (
	panelToggleMu      sync.Mutex
	panelToggleButtons = map[C.longlong]*rightPanel{}
	nextPanelToggle    C.longlong
)

// newRightPanel builds the panel's widgets and wires them to state: a
// click on the toggle button calls state.Toggle(), and state.OnChange
// (fired by Toggle from anywhere, not just this button) shows/hides the
// panel's box to match. modelSection, sessionsSection, and
// pipelineSection are 04-04/04-05/04-06's real section content.
func newRightPanel(state *appui.PanelState, modelSection *C.uiControl, sessionsSection *C.uiControl, pipelineSection *C.uiControl) *rightPanel {
	box := C.uiNewVerticalBox()
	C.uiBoxSetPadded(box, 1)
	C.uiBoxAppend(box, sectionGroupWithControl("Model", modelSection), 0)
	C.uiBoxAppend(box, sectionGroupWithControl("Sessions", sessionsSection), 0)
	C.uiBoxAppend(box, sectionGroupWithControl("Pipeline", pipelineSection), 1)

	toggle := C.uiNewButton(C.CString("Toggle Panel"))

	rp := &rightPanel{state: state, box: box, toggleButton: toggle}

	panelToggleMu.Lock()
	h := nextPanelToggle
	nextPanelToggle++
	panelToggleButtons[h] = rp
	panelToggleMu.Unlock()
	C.attachPanelToggleClicked(toggle, h)

	rp.syncVisibility()

	return rp
}

// syncVisibility shows or hides the panel's box to match state.Collapsed.
// Called directly from goPanelToggleClicked (after state.Toggle()) rather
// than via state.OnChange: PanelState.OnChange, like Transcript.OnChange
// and ApprovalTracker.OnChange, holds a single callback, and NewChatWindow
// separately uses that one slot for persisting state to disk -- so this
// package drives visibility itself instead of contending for it. The
// toggle button click is already on the UI thread (libui-ng callbacks
// always are), so no queueMain dispatch is needed here the way
// chatview.go's StreamPump-driven redraws need one.
func (rp *rightPanel) syncVisibility() {
	if rp.state.Collapsed {
		C.uiControlHide((*C.uiControl)(unsafe.Pointer(rp.box)))
	} else {
		C.uiControlShow((*C.uiControl)(unsafe.Pointer(rp.box)))
	}
}

// Visible reports whether the panel's box is currently shown, for tests
// (rightpanel_test.go) to observe syncVisibility's effect without the
// test file needing to import "C" itself.
func (rp *rightPanel) Visible() bool {
	return C.uiControlVisible((*C.uiControl)(unsafe.Pointer(rp.box))) != 0
}

// PanelControl returns the panel's own content as a generic uiControl, for
// adding to the window's root box.
func (rp *rightPanel) PanelControl() *C.uiControl {
	return (*C.uiControl)(unsafe.Pointer(rp.box))
}

// ToggleControl returns the toggle button as a generic uiControl, for
// adding to the chat column (it must stay visible even when the panel
// itself is hidden, or there'd be no way to bring it back).
func (rp *rightPanel) ToggleControl() *C.uiControl {
	return (*C.uiControl)(unsafe.Pointer(rp.toggleButton))
}

// sectionGroupWithControl builds a titled uiGroup around an already-built
// control, for a section with real content (the model picker) rather than
// a placeholder label.
func sectionGroupWithControl(title string, child *C.uiControl) *C.uiControl {
	group := C.uiNewGroup(C.CString(title))
	C.uiGroupSetMargined(group, 1)
	C.uiGroupSetChild(group, child)
	return (*C.uiControl)(unsafe.Pointer(group))
}

//export goPanelToggleClicked
func goPanelToggleClicked(button unsafe.Pointer, data unsafe.Pointer) {
	h := C.longlong(uintptr(data))
	panelToggleMu.Lock()
	rp, ok := panelToggleButtons[h]
	panelToggleMu.Unlock()
	if !ok {
		return
	}
	rp.state.Toggle()
	rp.syncVisibility()
}
