// This file is the model picker's widget-assembly layer (.planning/tasks/
// 04-04.json), same split as rightpanel.go: gophermind-osx/ui
// (ModelPickerState, in ui/modelpicker.go) holds the plain-Go catalogue,
// filters and preference-order state; this file turns that into real
// libui-ng widgets and is where the actual client.Catalogue/ModelSettings/
// PatchModelSettings/CreateSession calls live, injected as function values
// (PinModelFunc, refreshFunc, patchFunc) so ModelPickerState itself never
// imports gophermind-osx/client or touches cgo.
//
// "Click to pin" (POST /session with model+profile) creates a NEW session
// with that model, matching gophermind-lib/serve's actual contract: there
// is no endpoint to change an existing session's model, only to create one
// with a model already set (see gophermind-osx/client/service.go's
// CreateSession). Wiring that new session id into the live chat window is
// deferred: NewChatWindow has no connected client yet at all (main.go's
// sendTurn stub says as much -- "Not connected to a backend yet"), so pin
// here calls the injected PinModelFunc and, on success, just updates
// ModelPickerState's CurrentKey and posts the model-switched notification;
// actually attaching the chat window to the new session is 04-05's job
// (session management), once a live connection exists to attach through.
package main

/*
#include <ui.h>
#include <stdlib.h>

extern void goModelComboSelected(void *c, void *data);
extern void goModelPinClicked(void *b, void *data);
extern void goModelReachableToggled(void *c, void *data);
extern void goModelCapacityToggled(void *c, void *data);
extern void goModelOrderAddClicked(void *b, void *data);
extern void goModelOrderRemoveClicked(void *b, void *data);
extern void goModelOrderUpClicked(void *b, void *data);
extern void goModelOrderDownClicked(void *b, void *data);

static inline void attachComboSelected(uiCombobox *c, long long h) {
	uiComboboxOnSelected(c, (void (*)(uiCombobox *, void *))goModelComboSelected, (void *)h);
}
static inline void attachPinClicked(uiButton *b, long long h) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goModelPinClicked, (void *)h);
}
static inline void attachReachableToggled(uiCheckbox *c, long long h) {
	uiCheckboxOnToggled(c, (void (*)(uiCheckbox *, void *))goModelReachableToggled, (void *)h);
}
static inline void attachCapacityToggled(uiCheckbox *c, long long h) {
	uiCheckboxOnToggled(c, (void (*)(uiCheckbox *, void *))goModelCapacityToggled, (void *)h);
}
static inline void attachOrderAddClicked(uiButton *b, long long h) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goModelOrderAddClicked, (void *)h);
}
static inline void attachOrderRemoveClicked(uiButton *b, long long h) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goModelOrderRemoveClicked, (void *)h);
}
static inline void attachOrderUpClicked(uiButton *b, long long h) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goModelOrderUpClicked, (void *)h);
}
static inline void attachOrderDownClicked(uiButton *b, long long h) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goModelOrderDownClicked, (void *)h);
}
*/
import "C"

import (
	"context"
	"fmt"
	"sync"
	"unsafe"

	appui "gophermind/gophermind-osx/ui"
)

// PinModelFunc pins model+profile as a new session's model, returning the
// new session's id -- normally client.CreateSession with
// CreateSessionOptions{Model: model, Profile: profile}, injected rather
// than imported directly so this file's construction can be exercised
// without a real server (see modelpicker_test.go).
type PinModelFunc func(ctx context.Context, model, profile string) (sessionID string, err error)

// modelPicker is the model picker section's cgo widgets, bound to one
// appui.ModelPickerState.
type modelPicker struct {
	state *appui.ModelPickerState

	combo        *C.uiCombobox
	filteredKeys []string // parallel to combo's items, rebuilt by refresh

	pinButton      *C.uiButton
	reachableCheck *C.uiCheckbox
	capacityCheck  *C.uiCheckbox
	cycleLabel     *C.uiLabel

	orderCombo        *C.uiCombobox
	addOrderButton    *C.uiButton
	removeOrderButton *C.uiButton
	upButton          *C.uiButton
	downButton        *C.uiButton

	box *C.uiBox

	pin    PinModelFunc
	notify func(string) // posts a model-switched notification, e.g. Transcript.AddSystem
}

var (
	modelPickerMu       sync.Mutex
	modelPickerRegistry = map[C.longlong]*modelPicker{}
	nextModelPicker     C.longlong
)

// newModelPicker builds the model picker's widgets and wires them to
// state. pin performs the actual "click to pin" server call; notify posts
// the model-switched notification bar (see this file's top doc comment for
// why pin doesn't itself attach the chat window to the new session yet).
func newModelPicker(state *appui.ModelPickerState, pin PinModelFunc, notify func(string)) *modelPicker {
	mp := &modelPicker{
		state:             state,
		combo:             C.uiNewCombobox(),
		pinButton:         newCButton("Pin"),
		reachableCheck:    newCCheckbox("Reachable only"),
		capacityCheck:     newCCheckbox("Has capacity only"),
		cycleLabel:        newCLabel(""),
		orderCombo:        C.uiNewCombobox(),
		addOrderButton:    newCButton("Add to order"),
		removeOrderButton: newCButton("Remove"),
		upButton:          newCButton("Up"),
		downButton:        newCButton("Down"),
		pin:               pin,
		notify:            notify,
	}

	modelPickerMu.Lock()
	h := nextModelPicker
	nextModelPicker++
	modelPickerRegistry[h] = mp
	modelPickerMu.Unlock()

	C.attachComboSelected(mp.combo, h)
	C.attachPinClicked(mp.pinButton, h)
	C.attachReachableToggled(mp.reachableCheck, h)
	C.attachCapacityToggled(mp.capacityCheck, h)
	C.attachOrderAddClicked(mp.addOrderButton, h)
	C.attachOrderRemoveClicked(mp.removeOrderButton, h)
	C.attachOrderUpClicked(mp.upButton, h)
	C.attachOrderDownClicked(mp.downButton, h)

	filters := C.uiNewHorizontalBox()
	C.uiBoxSetPadded(filters, 1)
	C.uiBoxAppend(filters, (*C.uiControl)(unsafe.Pointer(mp.reachableCheck)), 0)
	C.uiBoxAppend(filters, (*C.uiControl)(unsafe.Pointer(mp.capacityCheck)), 0)

	orderButtons := C.uiNewHorizontalBox()
	C.uiBoxSetPadded(orderButtons, 1)
	C.uiBoxAppend(orderButtons, (*C.uiControl)(unsafe.Pointer(mp.upButton)), 0)
	C.uiBoxAppend(orderButtons, (*C.uiControl)(unsafe.Pointer(mp.downButton)), 0)
	C.uiBoxAppend(orderButtons, (*C.uiControl)(unsafe.Pointer(mp.addOrderButton)), 0)
	C.uiBoxAppend(orderButtons, (*C.uiControl)(unsafe.Pointer(mp.removeOrderButton)), 0)

	box := C.uiNewVerticalBox()
	C.uiBoxSetPadded(box, 1)
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(filters)), 0)
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(mp.combo)), 0)
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(mp.pinButton)), 0)
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(mp.cycleLabel)), 0)
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(mp.orderCombo)), 0)
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(orderButtons)), 0)
	mp.box = box

	state.OnChange(mp.refresh)
	mp.refresh()

	return mp
}

// Control returns the picker's box, for embedding into the right panel's
// "Model" section.
func (mp *modelPicker) Control() *C.uiControl {
	return (*C.uiControl)(unsafe.Pointer(mp.box))
}

// refresh rebuilds every widget from state: the filtered catalogue combo
// (with the current model marked), the auto-cycling indicator, the filter
// checkboxes, and the preference-order combo. Called on construction and
// on every state.OnChange.
func (mp *modelPicker) refresh() {
	entries := mp.state.FilteredEntries()
	mp.filteredKeys = mp.filteredKeys[:0]

	C.uiComboboxClear(mp.combo)
	for _, e := range entries {
		key := appui.ModelKey(e)
		mp.filteredKeys = append(mp.filteredKeys, key)
		label := fmt.Sprintf("%s / %s", e.Provider, e.ID)
		if mp.state.IsCurrent(e) {
			label = "* " + label + " (current)"
		}
		cLabel := C.CString(label)
		C.uiComboboxAppend(mp.combo, cLabel)
		C.free(unsafe.Pointer(cLabel))
	}
	for i := range mp.filteredKeys {
		if mp.state.IsCurrent(entries[i]) {
			C.uiComboboxSetSelected(mp.combo, C.int(i))
			break
		}
	}

	C.uiCheckboxSetChecked(mp.reachableCheck, boolToC(mp.state.FilterReachable()))
	C.uiCheckboxSetChecked(mp.capacityCheck, boolToC(mp.state.FilterHasCapacity()))

	cycleText := "Auto-cycling: off"
	if mp.state.AutoCyclingEnabled() {
		cycleText = "Auto-cycling: on"
	}
	cCycle := C.CString(cycleText)
	C.uiLabelSetText(mp.cycleLabel, cCycle)
	C.free(unsafe.Pointer(cCycle))

	C.uiComboboxClear(mp.orderCombo)
	for _, key := range mp.state.Order() {
		cKey := C.CString(key)
		C.uiComboboxAppend(mp.orderCombo, cKey)
		C.free(unsafe.Pointer(cKey))
	}
}

// newCButton/newCCheckbox/newCLabel wrap the C.CString conversion each of
// these one-time widget-label constructions needs. Not freed: same
// unfreed-but-one-shot precedent as chatinput.go's
// C.uiNewButton(C.CString("Send")) and rightpanel.go's sectionGroup --
// these run once per widget at construction, not in a per-frame loop like
// chatview.go's buildAttributedString, which does free every string it
// allocates.
func newCButton(text string) *C.uiButton {
	return C.uiNewButton(C.CString(text))
}

func newCCheckbox(text string) *C.uiCheckbox {
	return C.uiNewCheckbox(C.CString(text))
}

func newCLabel(text string) *C.uiLabel {
	return C.uiNewLabel(C.CString(text))
}

func boolToC(v bool) C.int {
	if v {
		return 1
	}
	return 0
}

// selectedFilteredKey returns the model key for the combo's currently
// selected item, or "" if nothing is selected (uiComboboxSelected returns
// -1 then).
func (mp *modelPicker) selectedFilteredKey() string {
	i := int(C.uiComboboxSelected(mp.combo))
	if i < 0 || i >= len(mp.filteredKeys) {
		return ""
	}
	return mp.filteredKeys[i]
}

// selectedOrderKey returns the order combo's currently selected key, or ""
// if nothing is selected.
func (mp *modelPicker) selectedOrderKey() string {
	i := int(C.uiComboboxSelected(mp.orderCombo))
	order := mp.state.Order()
	if i < 0 || i >= len(order) {
		return ""
	}
	return order[i]
}

// pinSelected performs the "click to pin" flow for the combo's current
// selection: splits its "profile/model" key, calls PinModelFunc, and on
// success updates CurrentKey and posts the model-switched notification.
func (mp *modelPicker) pinSelected() {
	key := mp.selectedFilteredKey()
	if key == "" || mp.pin == nil {
		return
	}
	profile, model := splitModelKey(key)
	_, err := mp.pin(context.Background(), model, profile)
	if err != nil {
		if mp.notify != nil {
			mp.notify(fmt.Sprintf("Failed to pin model %s: %s", key, err))
		}
		return
	}
	mp.state.SetCurrentKey(key)
	if mp.notify != nil {
		mp.notify(fmt.Sprintf("Switched to model %s", key))
	}
}

// splitModelKey splits a "profile/model" key as ModelKey produces it.
// modelcat.Entry.Profile is empty for a locally-served model (see
// modelcat.Entry's doc comment), which ModelKey then renders as a leading
// "/model" -- SplitN(key, "/", 2) still recovers ("", "model") correctly
// in that case.
func splitModelKey(key string) (profile, model string) {
	for i := 0; i < len(key); i++ {
		if key[i] == '/' {
			return key[:i], key[i+1:]
		}
	}
	return "", key
}

//export goModelComboSelected
func goModelComboSelected(c unsafe.Pointer, data unsafe.Pointer) {}

//export goModelPinClicked
func goModelPinClicked(b unsafe.Pointer, data unsafe.Pointer) {
	withModelPicker(data, func(mp *modelPicker) { mp.pinSelected() })
}

//export goModelReachableToggled
func goModelReachableToggled(c unsafe.Pointer, data unsafe.Pointer) {
	withModelPicker(data, func(mp *modelPicker) {
		mp.state.SetFilterReachable(C.uiCheckboxChecked(mp.reachableCheck) != 0)
	})
}

//export goModelCapacityToggled
func goModelCapacityToggled(c unsafe.Pointer, data unsafe.Pointer) {
	withModelPicker(data, func(mp *modelPicker) {
		mp.state.SetFilterHasCapacity(C.uiCheckboxChecked(mp.capacityCheck) != 0)
	})
}

//export goModelOrderAddClicked
func goModelOrderAddClicked(b unsafe.Pointer, data unsafe.Pointer) {
	withModelPicker(data, func(mp *modelPicker) {
		if key := mp.selectedFilteredKey(); key != "" {
			mp.state.AddToOrder(key)
		}
	})
}

//export goModelOrderRemoveClicked
func goModelOrderRemoveClicked(b unsafe.Pointer, data unsafe.Pointer) {
	withModelPicker(data, func(mp *modelPicker) {
		if key := mp.selectedOrderKey(); key != "" {
			mp.state.RemoveFromOrder(key)
		}
	})
}

//export goModelOrderUpClicked
func goModelOrderUpClicked(b unsafe.Pointer, data unsafe.Pointer) {
	withModelPicker(data, func(mp *modelPicker) {
		if key := mp.selectedOrderKey(); key != "" {
			mp.state.MoveUp(key)
		}
	})
}

//export goModelOrderDownClicked
func goModelOrderDownClicked(b unsafe.Pointer, data unsafe.Pointer) {
	withModelPicker(data, func(mp *modelPicker) {
		if key := mp.selectedOrderKey(); key != "" {
			mp.state.MoveDown(key)
		}
	})
}

// withModelPicker looks up the *modelPicker registered under data's handle
// and calls f with it, matching the handle-registry pattern chatview.go,
// chatinput.go, and rightpanel.go each use for their own libui-ng
// callbacks.
func withModelPicker(data unsafe.Pointer, f func(mp *modelPicker)) {
	h := C.longlong(uintptr(data))
	modelPickerMu.Lock()
	mp, ok := modelPickerRegistry[h]
	modelPickerMu.Unlock()
	if !ok {
		return
	}
	f(mp)
}
