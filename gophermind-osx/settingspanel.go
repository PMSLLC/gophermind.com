// This file is the settings panel's widget-assembly layer (.planning/
// tasks/04-07.json), same split as modelpicker.go/sessionlist.go/
// pipelinepanel.go: gophermind-osx/ui (BackendListState and
// CacheHistorySettings in ui/settings.go, plus the additive model-settings
// accessors 04-07 added to ui/modelpicker.go's ModelPickerState) holds the
// plain-Go state; this file turns it into real libui-ng widgets and is
// where the actual client.PatchModelSettings/SkillsList/SetSkillEnabled/
// AddSkillSource/RemoveSkillSource calls and gophermind-osx/connection
// calls live, injected as function values so the plain-Go layer never
// imports gophermind-osx/client or gophermind-osx/connection or touches
// cgo.
//
// "Opens as modal overlay via gear icon": libui-ng has no true modal
// window API (checked against /opt/homebrew/include/ui.h, same finding
// sessionlist.go's confirmWindow already documents) -- the gear button
// opens a plain secondary uiWindow instead, which does not block input to
// the main window. Its own gear glyph is also a substitution: uiButton
// takes plain text, not a symbol font or SF Symbol, so the button is
// labeled "Settings" rather than a real gear icon.
//
// "Remote backends: add/remove (server URL, gocloak realm), connect/
// disconnect, WG status shown": BackendProfile (ui/settings.go)
// deliberately carries only non-secret metadata (name, mode, server URL,
// gocloak realm). A real connection.RemoteConfig also needs a WireGuard
// client private key (connection.RemoteConfig.ClientPrivateKey) obtained
// through a gocloak login + WG registration handshake (03-03/03-04's
// job) that this simple add-a-backend form does not attempt to drive --
// connectFunc/disconnectFunc are injected exactly like every other
// server/connection call in this codebase's later phase-4 tasks (nil in
// NewChatWindow today, until a real integration step wires them). Nothing
// secret is ever written to BackendListState or to any file this panel
// writes; if a real private key needs storing, gophermind-osx/auth's
// existing Keychain-backed TokenStore is the place, not a plain file like
// panelstate.go's -- exactly the pattern gophermind-osx/auth already uses
// for OAuth tokens.
//
// "Cache/history: options configurable": no existing spec or code defines
// this beyond the name (checked: no prior task introduced a cache/history
// settings concept). ui/settings.go's CacheHistorySettings is a
// deliberately minimal, honest interpretation: how many transcript
// messages to keep, and whether history persists to disk at all.
package main

/*
#include <ui.h>
#include <stdlib.h>

extern void goSettingsGearClicked(void *b, void *data);
extern int goSettingsWindowClosing(void *w, void *data);
extern void goSettingsAddBackendClicked(void *b, void *data);
extern void goSettingsConnectClicked(void *b, void *data);
extern void goSettingsDisconnectClicked(void *b, void *data);
extern void goSettingsRemoveBackendClicked(void *b, void *data);
extern void goSettingsBackendSelected(void *c, void *data);
extern void goSettingsApplyModelSettingsClicked(void *b, void *data);
extern void goSettingsAddCustomLinkClicked(void *b, void *data);
extern void goSettingsAddSkillSourceClicked(void *b, void *data);
extern void goSettingsRemoveSkillSourceClicked(void *b, void *data);
extern void goSettingsSkillToggled(void *c, void *data);
extern void goSettingsEndpointModeSelected(void *r, void *data);
extern void goSettingsSaveCacheHistoryClicked(void *b, void *data);

static inline void attachSettingsGearClicked(uiButton *b, long long h) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goSettingsGearClicked, (void *)h);
}
static inline void attachSettingsWindowClosing(uiWindow *w, long long h) {
	uiWindowOnClosing(w, (int (*)(uiWindow *, void *))goSettingsWindowClosing, (void *)h);
}
static inline void attachSettingsAddBackendClicked(uiButton *b, long long h) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goSettingsAddBackendClicked, (void *)h);
}
static inline void attachSettingsConnectClicked(uiButton *b, long long h) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goSettingsConnectClicked, (void *)h);
}
static inline void attachSettingsDisconnectClicked(uiButton *b, long long h) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goSettingsDisconnectClicked, (void *)h);
}
static inline void attachSettingsRemoveBackendClicked(uiButton *b, long long h) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goSettingsRemoveBackendClicked, (void *)h);
}
static inline void attachSettingsBackendSelected(uiCombobox *c, long long h) {
	uiComboboxOnSelected(c, (void (*)(uiCombobox *, void *))goSettingsBackendSelected, (void *)h);
}
static inline void attachSettingsApplyModelSettingsClicked(uiButton *b, long long h) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goSettingsApplyModelSettingsClicked, (void *)h);
}
static inline void attachSettingsAddCustomLinkClicked(uiButton *b, long long h) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goSettingsAddCustomLinkClicked, (void *)h);
}
static inline void attachSettingsAddSkillSourceClicked(uiButton *b, long long h) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goSettingsAddSkillSourceClicked, (void *)h);
}
static inline void attachSettingsRemoveSkillSourceClicked(uiButton *b, long long h) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goSettingsRemoveSkillSourceClicked, (void *)h);
}
static inline void attachSettingsSkillToggled(uiCheckbox *c, long long h) {
	uiCheckboxOnToggled(c, (void (*)(uiCheckbox *, void *))goSettingsSkillToggled, (void *)h);
}
static inline void attachSettingsEndpointModeSelected(uiRadioButtons *r, long long h) {
	uiRadioButtonsOnSelected(r, (void (*)(uiRadioButtons *, void *))goSettingsEndpointModeSelected, (void *)h);
}
static inline void attachSettingsSaveCacheHistoryClicked(uiButton *b, long long h) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goSettingsSaveCacheHistoryClicked, (void *)h);
}
*/
import "C"

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"gophermind/gophermind-lib/skills"
	appui "gophermind/gophermind-osx/ui"
)

// ConnectBackendFunc connects to the backend described by p, normally
// building a connection.BackendConfig from p and calling
// connection.Manager.Connect.
type ConnectBackendFunc func(ctx context.Context, p appui.BackendProfile) error

// DisconnectBackendFunc disconnects the named backend, normally
// connection.Manager.Disconnect.
type DisconnectBackendFunc func(name string)

// PatchModelSettingsFunc persists a full modelcat.Settings snapshot,
// normally client.PatchModelSettings called with every field (the server
// PATCH endpoint takes a partial map, but this panel always has the
// complete current picture from ModelPickerState.Settings(), so it sends
// all of it rather than computing a diff).
type PatchModelSettingsFunc func(ctx context.Context, patch map[string]any) error

// SkillsListFunc lists configured skill sources and skills, normally
// client.SkillsList.
type SkillsListFunc func(ctx context.Context) ([]skills.Source, []skills.Skill, error)

// SetSkillEnabledFunc toggles one skill, normally client.SetSkillEnabled.
type SetSkillEnabledFunc func(ctx context.Context, key string, enabled bool) error

// AddSkillSourceFunc adds a skill source, normally client.AddSkillSource.
type AddSkillSourceFunc func(ctx context.Context, url, ref string) error

// RemoveSkillSourceFunc removes a skill source, normally
// client.RemoveSkillSource.
type RemoveSkillSourceFunc func(ctx context.Context, id string) error

// SetEndpointModeFunc switches which backend is active, normally something
// that re-points ChatWindow's live connection at the named backend.
type SetEndpointModeFunc func(backendName string)

// settingsPanel owns the gear button (always visible) and the secondary
// settings window (built once, shown/hidden rather than rebuilt on every
// open -- see this file's doc comment on why it isn't a true modal).
type settingsPanel struct {
	parent *C.uiWindow
	window *C.uiWindow

	backends *appui.BackendListState
	model    *appui.ModelPickerState

	cacheHistory     appui.CacheHistorySettings
	saveCacheHistory func(appui.CacheHistorySettings)

	connectFunc      ConnectBackendFunc
	disconnectFunc   DisconnectBackendFunc
	patchModelFunc   PatchModelSettingsFunc
	skillsListFunc   SkillsListFunc
	setSkillFunc     SetSkillEnabledFunc
	addSourceFunc    AddSkillSourceFunc
	removeSourceFunc RemoveSkillSourceFunc
	setModeFunc      SetEndpointModeFunc

	gearButton *C.uiButton

	backendCombo      *C.uiCombobox
	backendNameEntry  *C.uiEntry
	backendURLEntry   *C.uiEntry
	backendRealmEntry *C.uiEntry
	backendTokenEntry *C.uiEntry
	backendModeRadio  *C.uiRadioButtons
	backendStatusLbl  *C.uiLabel
	backendNames      []string

	reachableCheck *C.uiCheckbox
	capacityCheck  *C.uiCheckbox
	autoCycleCheck *C.uiCheckbox
	capacitySpin   *C.uiSpinbox
	whenFullCombo  *C.uiCombobox
	excludedEntry  *C.uiEntry
	linkKeyEntry   *C.uiEntry
	linkURLEntry   *C.uiEntry
	linksLabel     *C.uiLabel

	skillsBox     *C.uiBox
	skillChecks   []*C.uiCheckbox
	skillKeys     []string
	sourceCombo   *C.uiCombobox
	sourceURLs    []string
	sourceURLEntr *C.uiEntry
	sourceRefEntr *C.uiEntry

	endpointRadio *C.uiRadioButtons

	persistHistoryCheck *C.uiCheckbox
	maxHistorySpin      *C.uiSpinbox
}

var (
	settingsPanelMu    sync.Mutex
	settingsPanels     = map[C.longlong]*settingsPanel{}
	nextSettingsHandle C.longlong
)

// newSettingsPanel builds the gear button; the settings window itself is
// built lazily on first click (doOpen), since it needs the current
// snapshot of backends/model settings to populate its fields.
func newSettingsPanel(parent *C.uiWindow, backends *appui.BackendListState, model *appui.ModelPickerState, cacheHistory appui.CacheHistorySettings, saveCacheHistory func(appui.CacheHistorySettings), connectFunc ConnectBackendFunc, disconnectFunc DisconnectBackendFunc, patchModelFunc PatchModelSettingsFunc, skillsListFunc SkillsListFunc, setSkillFunc SetSkillEnabledFunc, addSourceFunc AddSkillSourceFunc, removeSourceFunc RemoveSkillSourceFunc, setModeFunc SetEndpointModeFunc) *settingsPanel {
	sp := &settingsPanel{
		parent: parent, backends: backends, model: model,
		cacheHistory: cacheHistory, saveCacheHistory: saveCacheHistory,
		connectFunc: connectFunc, disconnectFunc: disconnectFunc, patchModelFunc: patchModelFunc,
		skillsListFunc: skillsListFunc, setSkillFunc: setSkillFunc,
		addSourceFunc: addSourceFunc, removeSourceFunc: removeSourceFunc, setModeFunc: setModeFunc,
	}
	sp.gearButton = newCButton("Settings")

	settingsPanelMu.Lock()
	h := nextSettingsHandle
	nextSettingsHandle++
	settingsPanels[h] = sp
	settingsPanelMu.Unlock()
	C.attachSettingsGearClicked(sp.gearButton, h)

	return sp
}

// GearControl returns the gear (Settings) button as a generic uiControl.
func (sp *settingsPanel) GearControl() *C.uiControl {
	return (*C.uiControl)(unsafe.Pointer(sp.gearButton))
}

// doOpen builds the settings window on first call, or just shows it again
// on a later one, and refreshes every field from current state.
func (sp *settingsPanel) doOpen() {
	if sp.window == nil {
		sp.build()
	}
	sp.refreshBackends()
	sp.refreshModelSettings()
	sp.refreshSkills()
	sp.refreshCacheHistory()
	C.uiControlShow((*C.uiControl)(unsafe.Pointer(sp.window)))
}

func (sp *settingsPanel) build() {
	win := C.uiNewWindow(C.CString("Settings"), 420, 560, 0)

	root := C.uiNewVerticalBox()
	C.uiBoxSetPadded(root, 1)

	// --- Backends ---
	backendsGroup := C.uiNewGroup(C.CString("Backends"))
	C.uiGroupSetMargined(backendsGroup, 1)
	backendsBox := C.uiNewVerticalBox()
	C.uiBoxSetPadded(backendsBox, 1)

	sp.backendCombo = C.uiNewCombobox()
	C.uiBoxAppend(backendsBox, (*C.uiControl)(unsafe.Pointer(sp.backendCombo)), 0)
	sp.backendStatusLbl = newCLabel("")
	C.uiBoxAppend(backendsBox, (*C.uiControl)(unsafe.Pointer(sp.backendStatusLbl)), 0)

	sp.backendNameEntry = C.uiNewEntry()
	sp.backendURLEntry = C.uiNewEntry()
	sp.backendRealmEntry = C.uiNewEntry()
	sp.backendTokenEntry = C.uiNewEntry()
	C.uiBoxAppend(backendsBox, (*C.uiControl)(unsafe.Pointer(newCLabel("Name"))), 0)
	C.uiBoxAppend(backendsBox, (*C.uiControl)(unsafe.Pointer(sp.backendNameEntry)), 0)
	C.uiBoxAppend(backendsBox, (*C.uiControl)(unsafe.Pointer(newCLabel("Server URL"))), 0)
	C.uiBoxAppend(backendsBox, (*C.uiControl)(unsafe.Pointer(sp.backendURLEntry)), 0)
	C.uiBoxAppend(backendsBox, (*C.uiControl)(unsafe.Pointer(newCLabel("Gocloak realm"))), 0)
	C.uiBoxAppend(backendsBox, (*C.uiControl)(unsafe.Pointer(sp.backendRealmEntry)), 0)
	C.uiBoxAppend(backendsBox, (*C.uiControl)(unsafe.Pointer(newCLabel("Bearer token"))), 0)
	C.uiBoxAppend(backendsBox, (*C.uiControl)(unsafe.Pointer(sp.backendTokenEntry)), 0)

	sp.backendModeRadio = C.uiNewRadioButtons()
	C.uiRadioButtonsAppend(sp.backendModeRadio, C.CString("local"))
	C.uiRadioButtonsAppend(sp.backendModeRadio, C.CString("remote"))
	C.uiBoxAppend(backendsBox, (*C.uiControl)(unsafe.Pointer(sp.backendModeRadio)), 0)

	addBackend := newCButton("Add / Update Backend")
	connectBtn := newCButton("Connect")
	disconnectBtn := newCButton("Disconnect")
	removeBackend := newCButton("Remove")
	backendBtnRow := C.uiNewHorizontalBox()
	C.uiBoxSetPadded(backendBtnRow, 1)
	C.uiBoxAppend(backendBtnRow, (*C.uiControl)(unsafe.Pointer(connectBtn)), 0)
	C.uiBoxAppend(backendBtnRow, (*C.uiControl)(unsafe.Pointer(disconnectBtn)), 0)
	C.uiBoxAppend(backendBtnRow, (*C.uiControl)(unsafe.Pointer(removeBackend)), 0)
	C.uiBoxAppend(backendsBox, (*C.uiControl)(unsafe.Pointer(addBackend)), 0)
	C.uiBoxAppend(backendsBox, (*C.uiControl)(unsafe.Pointer(backendBtnRow)), 0)

	C.uiGroupSetChild(backendsGroup, (*C.uiControl)(unsafe.Pointer(backendsBox)))
	C.uiBoxAppend(root, (*C.uiControl)(unsafe.Pointer(backendsGroup)), 0)

	// --- Endpoint mode ---
	endpointGroup := C.uiNewGroup(C.CString("Endpoint"))
	C.uiGroupSetMargined(endpointGroup, 1)
	sp.endpointRadio = C.uiNewRadioButtons()
	C.uiRadioButtonsAppend(sp.endpointRadio, C.CString("local"))
	C.uiRadioButtonsAppend(sp.endpointRadio, C.CString("remote"))
	C.uiGroupSetChild(endpointGroup, (*C.uiControl)(unsafe.Pointer(sp.endpointRadio)))
	C.uiBoxAppend(root, (*C.uiControl)(unsafe.Pointer(endpointGroup)), 0)

	// --- Model settings ---
	modelGroup := C.uiNewGroup(C.CString("Model settings"))
	C.uiGroupSetMargined(modelGroup, 1)
	modelBox := C.uiNewVerticalBox()
	C.uiBoxSetPadded(modelBox, 1)

	sp.reachableCheck = newCCheckbox("Reachable only")
	sp.capacityCheck = newCCheckbox("Has capacity only")
	sp.autoCycleCheck = newCCheckbox("Auto-cycle on capacity")
	C.uiBoxAppend(modelBox, (*C.uiControl)(unsafe.Pointer(sp.reachableCheck)), 0)
	C.uiBoxAppend(modelBox, (*C.uiControl)(unsafe.Pointer(sp.capacityCheck)), 0)
	C.uiBoxAppend(modelBox, (*C.uiControl)(unsafe.Pointer(sp.autoCycleCheck)), 0)

	C.uiBoxAppend(modelBox, (*C.uiControl)(unsafe.Pointer(newCLabel("Capacity threshold (%)"))), 0)
	sp.capacitySpin = C.uiNewSpinbox(0, 100)
	C.uiBoxAppend(modelBox, (*C.uiControl)(unsafe.Pointer(sp.capacitySpin)), 0)

	C.uiBoxAppend(modelBox, (*C.uiControl)(unsafe.Pointer(newCLabel("When all full"))), 0)
	sp.whenFullCombo = C.uiNewCombobox()
	C.uiComboboxAppend(sp.whenFullCombo, C.CString("stay"))
	C.uiComboboxAppend(sp.whenFullCombo, C.CString("ask"))
	C.uiBoxAppend(modelBox, (*C.uiControl)(unsafe.Pointer(sp.whenFullCombo)), 0)

	C.uiBoxAppend(modelBox, (*C.uiControl)(unsafe.Pointer(newCLabel("Excluded terms (comma-separated)"))), 0)
	sp.excludedEntry = C.uiNewEntry()
	C.uiBoxAppend(modelBox, (*C.uiControl)(unsafe.Pointer(sp.excludedEntry)), 0)

	sp.linksLabel = newCLabel("")
	C.uiBoxAppend(modelBox, (*C.uiControl)(unsafe.Pointer(sp.linksLabel)), 0)
	sp.linkKeyEntry = C.uiNewEntry()
	sp.linkURLEntry = C.uiNewEntry()
	C.uiBoxAppend(modelBox, (*C.uiControl)(unsafe.Pointer(newCLabel("Custom link key (profile/model)"))), 0)
	C.uiBoxAppend(modelBox, (*C.uiControl)(unsafe.Pointer(sp.linkKeyEntry)), 0)
	C.uiBoxAppend(modelBox, (*C.uiControl)(unsafe.Pointer(newCLabel("Custom link URL"))), 0)
	C.uiBoxAppend(modelBox, (*C.uiControl)(unsafe.Pointer(sp.linkURLEntry)), 0)
	addLink := newCButton("Add Custom Link")
	C.uiBoxAppend(modelBox, (*C.uiControl)(unsafe.Pointer(addLink)), 0)

	applyModel := newCButton("Apply Model Settings")
	C.uiBoxAppend(modelBox, (*C.uiControl)(unsafe.Pointer(applyModel)), 0)

	C.uiGroupSetChild(modelGroup, (*C.uiControl)(unsafe.Pointer(modelBox)))
	C.uiBoxAppend(root, (*C.uiControl)(unsafe.Pointer(modelGroup)), 0)

	// --- Skills ---
	skillsGroup := C.uiNewGroup(C.CString("Skills"))
	C.uiGroupSetMargined(skillsGroup, 1)
	skillsOuter := C.uiNewVerticalBox()
	C.uiBoxSetPadded(skillsOuter, 1)
	sp.skillsBox = C.uiNewVerticalBox()
	C.uiBoxAppend(skillsOuter, (*C.uiControl)(unsafe.Pointer(sp.skillsBox)), 0)

	sp.sourceCombo = C.uiNewCombobox()
	C.uiBoxAppend(skillsOuter, (*C.uiControl)(unsafe.Pointer(sp.sourceCombo)), 0)
	sp.sourceURLEntr = C.uiNewEntry()
	sp.sourceRefEntr = C.uiNewEntry()
	C.uiBoxAppend(skillsOuter, (*C.uiControl)(unsafe.Pointer(newCLabel("Source URL"))), 0)
	C.uiBoxAppend(skillsOuter, (*C.uiControl)(unsafe.Pointer(sp.sourceURLEntr)), 0)
	C.uiBoxAppend(skillsOuter, (*C.uiControl)(unsafe.Pointer(newCLabel("Ref"))), 0)
	C.uiBoxAppend(skillsOuter, (*C.uiControl)(unsafe.Pointer(sp.sourceRefEntr)), 0)
	sourceBtnRow := C.uiNewHorizontalBox()
	C.uiBoxSetPadded(sourceBtnRow, 1)
	addSource := newCButton("Add Source")
	removeSource := newCButton("Remove Selected Source")
	C.uiBoxAppend(sourceBtnRow, (*C.uiControl)(unsafe.Pointer(addSource)), 0)
	C.uiBoxAppend(sourceBtnRow, (*C.uiControl)(unsafe.Pointer(removeSource)), 0)
	C.uiBoxAppend(skillsOuter, (*C.uiControl)(unsafe.Pointer(sourceBtnRow)), 0)

	C.uiGroupSetChild(skillsGroup, (*C.uiControl)(unsafe.Pointer(skillsOuter)))
	C.uiBoxAppend(root, (*C.uiControl)(unsafe.Pointer(skillsGroup)), 0)

	// --- Cache / history ---
	cacheGroup := C.uiNewGroup(C.CString("Cache / history"))
	C.uiGroupSetMargined(cacheGroup, 1)
	cacheBox := C.uiNewVerticalBox()
	C.uiBoxSetPadded(cacheBox, 1)
	sp.persistHistoryCheck = newCCheckbox("Persist chat history")
	C.uiBoxAppend(cacheBox, (*C.uiControl)(unsafe.Pointer(sp.persistHistoryCheck)), 0)
	C.uiBoxAppend(cacheBox, (*C.uiControl)(unsafe.Pointer(newCLabel("Max history messages"))), 0)
	sp.maxHistorySpin = C.uiNewSpinbox(1, 100000)
	C.uiBoxAppend(cacheBox, (*C.uiControl)(unsafe.Pointer(sp.maxHistorySpin)), 0)
	saveCacheBtn := newCButton("Save")
	C.uiBoxAppend(cacheBox, (*C.uiControl)(unsafe.Pointer(saveCacheBtn)), 0)
	C.uiGroupSetChild(cacheGroup, (*C.uiControl)(unsafe.Pointer(cacheBox)))
	C.uiBoxAppend(root, (*C.uiControl)(unsafe.Pointer(cacheGroup)), 0)

	C.uiWindowSetChild(win, (*C.uiControl)(unsafe.Pointer(root)))
	sp.window = win

	settingsPanelMu.Lock()
	var h C.longlong
	for k, v := range settingsPanels {
		if v == sp {
			h = k
		}
	}
	settingsPanelMu.Unlock()

	C.attachSettingsWindowClosing(win, h)
	C.attachSettingsAddBackendClicked(addBackend, h)
	C.attachSettingsConnectClicked(connectBtn, h)
	C.attachSettingsDisconnectClicked(disconnectBtn, h)
	C.attachSettingsRemoveBackendClicked(removeBackend, h)
	C.attachSettingsBackendSelected(sp.backendCombo, h)
	C.attachSettingsApplyModelSettingsClicked(applyModel, h)
	C.attachSettingsAddCustomLinkClicked(addLink, h)
	C.attachSettingsAddSkillSourceClicked(addSource, h)
	C.attachSettingsRemoveSkillSourceClicked(removeSource, h)
	C.attachSettingsEndpointModeSelected(sp.endpointRadio, h)
	C.attachSettingsSaveCacheHistoryClicked(saveCacheBtn, h)
}

// destroyForTest destroys the settings window (built lazily by doOpen),
// so a test that calls doOpen doesn't leak it past app.Close(): the
// settings window is a separate top-level uiWindow, never a child of
// app.window, so destroying app.window does not destroy it too -- libui-
// ng's leak detector treats any un-destroyed uiControl as a bug at
// uiUninit, crashing the whole test binary if this isn't called.
func (sp *settingsPanel) destroyForTest() {
	if sp.window != nil {
		C.uiControlDestroy((*C.uiControl)(unsafe.Pointer(sp.window)))
	}
}

// setEntryText wraps the C.CString conversion uiEntrySetText needs, so
// settingspanel_test.go (which cannot import "C" itself, per Go's cgo
// restriction on _test.go files) can still drive an entry field's text
// directly, the same way this file's own doAddBackend etc. do.
func setEntryText(e *C.uiEntry, text string) {
	C.uiEntrySetText(e, C.CString(text))
}

func (sp *settingsPanel) selectedBackendName() string {
	i := int(C.uiComboboxSelected(sp.backendCombo))
	if i < 0 || i >= len(sp.backendNames) {
		return ""
	}
	return sp.backendNames[i]
}

// refreshBackends repopulates the backend combobox from sp.backends,
// matching modelpicker.go's own refresh pattern (uiComboboxClear then
// uiComboboxAppend each item) rather than rebuilding the widget.
func (sp *settingsPanel) refreshBackends() {
	// uiComboboxClear discards the current selection (checked against
	// ui.h: neither Clear nor Append preserves or restores it), so the
	// previously selected name is captured first and reselected after
	// repopulating -- without this, a status change (e.g. doDisconnect
	// calling this at its end) would silently clear the user's selection
	// and break a following Remove/Disconnect click.
	previouslySelected := sp.selectedBackendName()

	C.uiComboboxClear(sp.backendCombo)
	sp.backendNames = nil
	for _, p := range sp.backends.Profiles() {
		C.uiComboboxAppend(sp.backendCombo, C.CString(fmt.Sprintf("%s (%s)", p.Name, sp.backends.Status(p.Name))))
		sp.backendNames = append(sp.backendNames, p.Name)
	}

	if previouslySelected != "" {
		sp.selectBackend(previouslySelected)
	}

	if name := sp.selectedBackendName(); name != "" {
		C.uiLabelSetText(sp.backendStatusLbl, C.CString("Status: "+sp.backends.Status(name)))
	} else {
		C.uiLabelSetText(sp.backendStatusLbl, C.CString("No backend selected"))
	}
}

func (sp *settingsPanel) refreshModelSettings() {
	C.uiCheckboxSetChecked(sp.reachableCheck, boolToC(sp.model.FilterReachable()))
	C.uiCheckboxSetChecked(sp.capacityCheck, boolToC(sp.model.FilterHasCapacity()))
	C.uiCheckboxSetChecked(sp.autoCycleCheck, boolToC(sp.model.AutoCyclingEnabled()))
	C.uiSpinboxSetValue(sp.capacitySpin, C.int(sp.model.CapacityPercent()))
	if sp.model.WhenAllFull() == "ask" {
		C.uiComboboxSetSelected(sp.whenFullCombo, 1)
	} else {
		C.uiComboboxSetSelected(sp.whenFullCombo, 0)
	}
	C.uiEntrySetText(sp.excludedEntry, C.CString(strings.Join(sp.model.ExcludedTerms(), ",")))

	links := sp.model.CustomLinks()
	var lines []string
	for k, v := range links {
		lines = append(lines, k+" -> "+v)
	}
	C.uiLabelSetText(sp.linksLabel, C.CString(strings.Join(lines, "\n")))
}

func (sp *settingsPanel) refreshCacheHistory() {
	C.uiCheckboxSetChecked(sp.persistHistoryCheck, boolToC(sp.cacheHistory.PersistHistory))
	C.uiSpinboxSetValue(sp.maxHistorySpin, C.int(sp.cacheHistory.MaxHistoryMessages))
}

// refreshSkills fetches the current skills/sources via skillsListFunc (if
// set) and rebuilds the skills checkbox stack and the sources combobox.
// Same rebuilt-widget-stack approach as pipelinepanel.go's task list.
func (sp *settingsPanel) refreshSkills() {
	for i := len(sp.skillChecks) - 1; i >= 0; i-- {
		C.uiBoxDelete(sp.skillsBox, C.int(i))
		C.uiControlDestroy((*C.uiControl)(unsafe.Pointer(sp.skillChecks[i])))
	}
	sp.skillChecks = sp.skillChecks[:0]
	sp.skillKeys = sp.skillKeys[:0]

	C.uiComboboxClear(sp.sourceCombo)
	sp.sourceURLs = nil

	if sp.skillsListFunc == nil {
		return
	}

	sources, list, err := sp.skillsListFunc(context.Background())
	if err != nil {
		return
	}

	h := settingsHandleFor(sp)
	for _, sk := range list {
		cb := newCCheckbox(sk.Name)
		C.uiCheckboxSetChecked(cb, boolToC(sk.Enabled))
		C.attachSettingsSkillToggled(cb, h)
		C.uiBoxAppend(sp.skillsBox, (*C.uiControl)(unsafe.Pointer(cb)), 0)
		sp.skillChecks = append(sp.skillChecks, cb)
		sp.skillKeys = append(sp.skillKeys, sk.Key)
	}
	for _, src := range sources {
		C.uiComboboxAppend(sp.sourceCombo, C.CString(src.URL))
		sp.sourceURLs = append(sp.sourceURLs, src.URL)
	}
}

func settingsHandleFor(sp *settingsPanel) C.longlong {
	settingsPanelMu.Lock()
	defer settingsPanelMu.Unlock()
	for h, v := range settingsPanels {
		if v == sp {
			return h
		}
	}
	return -1
}

func withSettingsPanel(data unsafe.Pointer, f func(sp *settingsPanel)) {
	h := C.longlong(uintptr(data))
	settingsPanelMu.Lock()
	sp, ok := settingsPanels[h]
	settingsPanelMu.Unlock()
	if !ok {
		return
	}
	f(sp)
}

//export goSettingsGearClicked
func goSettingsGearClicked(b unsafe.Pointer, data unsafe.Pointer) {
	withSettingsPanel(data, func(sp *settingsPanel) { sp.doOpen() })
}

//export goSettingsWindowClosing
func goSettingsWindowClosing(w unsafe.Pointer, data unsafe.Pointer) C.int {
	withSettingsPanel(data, func(sp *settingsPanel) {
		C.uiControlHide((*C.uiControl)(unsafe.Pointer(sp.window)))
	})
	return 0 // do not destroy: the window is reused on the next open
}

func (sp *settingsPanel) doAddBackend() {
	name := C.GoString(C.uiEntryText(sp.backendNameEntry))
	url := C.GoString(C.uiEntryText(sp.backendURLEntry))
	realm := C.GoString(C.uiEntryText(sp.backendRealmEntry))
	token := C.GoString(C.uiEntryText(sp.backendTokenEntry))
	mode := "local"
	if C.uiRadioButtonsSelected(sp.backendModeRadio) == 1 {
		mode = "remote"
	}
	if name == "" {
		return
	}
	sp.backends.Add(appui.BackendProfile{Name: name, Mode: mode, ServerURL: url, GocloakRealm: realm, Token: token})
	sp.refreshBackends()
	sp.selectBackend(name)
}

// selectBackend selects name in the backend combobox, if present --
// refreshBackends alone leaves nothing selected (uiComboboxAppend/Clear
// never change the selection, per ui.h), so callers that just added or
// otherwise care about a specific backend call this afterward.
func (sp *settingsPanel) selectBackend(name string) {
	for i, n := range sp.backendNames {
		if n == name {
			C.uiComboboxSetSelected(sp.backendCombo, C.int(i))
			C.uiLabelSetText(sp.backendStatusLbl, C.CString("Status: "+sp.backends.Status(name)))
			return
		}
	}
}

//export goSettingsAddBackendClicked
func goSettingsAddBackendClicked(b unsafe.Pointer, data unsafe.Pointer) {
	withSettingsPanel(data, func(sp *settingsPanel) { sp.doAddBackend() })
}

func (sp *settingsPanel) doConnect() {
	name := sp.selectedBackendName()
	if name == "" || sp.connectFunc == nil {
		return
	}
	var profile appui.BackendProfile
	for _, p := range sp.backends.Profiles() {
		if p.Name == name {
			profile = p
		}
	}
	go func() {
		err := sp.connectFunc(context.Background(), profile)
		status := "connected"
		if err != nil {
			status = "error: " + err.Error()
		}
		sp.backends.SetStatus(name, status)
		queueMain(sp.refreshBackends)
	}()
}

//export goSettingsConnectClicked
func goSettingsConnectClicked(b unsafe.Pointer, data unsafe.Pointer) {
	withSettingsPanel(data, func(sp *settingsPanel) { sp.doConnect() })
}

func (sp *settingsPanel) doDisconnect() {
	name := sp.selectedBackendName()
	if name == "" {
		return
	}
	if sp.disconnectFunc != nil {
		sp.disconnectFunc(name)
	}
	sp.backends.SetStatus(name, "disconnected")
	sp.refreshBackends()
}

//export goSettingsDisconnectClicked
func goSettingsDisconnectClicked(b unsafe.Pointer, data unsafe.Pointer) {
	withSettingsPanel(data, func(sp *settingsPanel) { sp.doDisconnect() })
}

func (sp *settingsPanel) doRemoveBackend() {
	name := sp.selectedBackendName()
	if name == "" {
		return
	}
	sp.backends.Remove(name)
	sp.refreshBackends()
}

//export goSettingsRemoveBackendClicked
func goSettingsRemoveBackendClicked(b unsafe.Pointer, data unsafe.Pointer) {
	withSettingsPanel(data, func(sp *settingsPanel) { sp.doRemoveBackend() })
}

//export goSettingsBackendSelected
func goSettingsBackendSelected(c unsafe.Pointer, data unsafe.Pointer) {
	withSettingsPanel(data, func(sp *settingsPanel) {
		if name := sp.selectedBackendName(); name != "" {
			C.uiLabelSetText(sp.backendStatusLbl, C.CString("Status: "+sp.backends.Status(name)))
		}
	})
}

func (sp *settingsPanel) doApplyModelSettings() {
	sp.model.SetFilterReachable(C.uiCheckboxChecked(sp.reachableCheck) != 0)
	sp.model.SetFilterHasCapacity(C.uiCheckboxChecked(sp.capacityCheck) != 0)
	sp.model.SetAutoCycling(C.uiCheckboxChecked(sp.autoCycleCheck) != 0)
	sp.model.SetCapacityPercent(int(C.uiSpinboxValue(sp.capacitySpin)))
	if C.uiComboboxSelected(sp.whenFullCombo) == 1 {
		sp.model.SetWhenAllFull("ask")
	} else {
		sp.model.SetWhenAllFull("stay")
	}
	raw := C.GoString(C.uiEntryText(sp.excludedEntry))
	var terms []string
	for _, t := range strings.Split(raw, ",") {
		if t = strings.TrimSpace(t); t != "" {
			terms = append(terms, t)
		}
	}
	sp.model.SetExcludedTerms(terms)

	if sp.patchModelFunc == nil {
		return
	}
	settings := sp.model.Settings()
	patch := map[string]any{
		"order":               settings.Order,
		"cycle_on_capacity":   settings.CycleOnCapacity,
		"capacity_percent":    settings.CapacityPercent,
		"when_all_full":       settings.WhenAllFull,
		"filter_reachable":    settings.FilterReachable,
		"filter_has_capacity": settings.FilterHasCapacity,
		"excluded_terms":      settings.ExcludedTerms,
		"custom_links":        settings.CustomLinks,
	}
	go sp.patchModelFunc(context.Background(), patch)
}

//export goSettingsApplyModelSettingsClicked
func goSettingsApplyModelSettingsClicked(b unsafe.Pointer, data unsafe.Pointer) {
	withSettingsPanel(data, func(sp *settingsPanel) { sp.doApplyModelSettings() })
}

func (sp *settingsPanel) doAddCustomLink() {
	key := C.GoString(C.uiEntryText(sp.linkKeyEntry))
	url := C.GoString(C.uiEntryText(sp.linkURLEntry))
	if key == "" || url == "" {
		return
	}
	sp.model.SetCustomLink(key, url)
	sp.refreshModelSettings()
}

//export goSettingsAddCustomLinkClicked
func goSettingsAddCustomLinkClicked(b unsafe.Pointer, data unsafe.Pointer) {
	withSettingsPanel(data, func(sp *settingsPanel) { sp.doAddCustomLink() })
}

func (sp *settingsPanel) doSkillToggled() {
	for i, cb := range sp.skillChecks {
		enabled := C.uiCheckboxChecked(cb) != 0
		if sp.setSkillFunc != nil {
			go sp.setSkillFunc(context.Background(), sp.skillKeys[i], enabled)
		}
	}
}

//export goSettingsSkillToggled
func goSettingsSkillToggled(c unsafe.Pointer, data unsafe.Pointer) {
	withSettingsPanel(data, func(sp *settingsPanel) { sp.doSkillToggled() })
}

func (sp *settingsPanel) doAddSkillSource() {
	url := C.GoString(C.uiEntryText(sp.sourceURLEntr))
	ref := C.GoString(C.uiEntryText(sp.sourceRefEntr))
	if url == "" || sp.addSourceFunc == nil {
		return
	}
	go func() {
		if err := sp.addSourceFunc(context.Background(), url, ref); err == nil {
			queueMain(sp.refreshSkills)
		}
	}()
}

//export goSettingsAddSkillSourceClicked
func goSettingsAddSkillSourceClicked(b unsafe.Pointer, data unsafe.Pointer) {
	withSettingsPanel(data, func(sp *settingsPanel) { sp.doAddSkillSource() })
}

func (sp *settingsPanel) doRemoveSkillSource() {
	i := int(C.uiComboboxSelected(sp.sourceCombo))
	if i < 0 || i >= len(sp.sourceURLs) || sp.removeSourceFunc == nil {
		return
	}
	id := sp.sourceURLs[i]
	go func() {
		if err := sp.removeSourceFunc(context.Background(), id); err == nil {
			queueMain(sp.refreshSkills)
		}
	}()
}

//export goSettingsRemoveSkillSourceClicked
func goSettingsRemoveSkillSourceClicked(b unsafe.Pointer, data unsafe.Pointer) {
	withSettingsPanel(data, func(sp *settingsPanel) { sp.doRemoveSkillSource() })
}

// doEndpointModeSelected handles the Endpoint section's local/remote
// radio buttons: which mode the app should use for its live connection.
// This is a mode preference (matching connection.ModeLocal/ModeRemote),
// not "which named backend is selected" -- that stays a separate concern
// (the Backends section's own combobox and Connect/Disconnect buttons).
func (sp *settingsPanel) doEndpointModeSelected() {
	mode := "local"
	if C.uiRadioButtonsSelected(sp.endpointRadio) == 1 {
		mode = "remote"
	}
	if sp.setModeFunc != nil {
		sp.setModeFunc(mode)
	}
}

//export goSettingsEndpointModeSelected
func goSettingsEndpointModeSelected(r unsafe.Pointer, data unsafe.Pointer) {
	withSettingsPanel(data, func(sp *settingsPanel) { sp.doEndpointModeSelected() })
}

func (sp *settingsPanel) doSaveCacheHistory() {
	sp.cacheHistory = appui.CacheHistorySettings{
		PersistHistory:     C.uiCheckboxChecked(sp.persistHistoryCheck) != 0,
		MaxHistoryMessages: int(C.uiSpinboxValue(sp.maxHistorySpin)),
	}
	if sp.saveCacheHistory != nil {
		sp.saveCacheHistory(sp.cacheHistory)
	}
}

//export goSettingsSaveCacheHistoryClicked
func goSettingsSaveCacheHistoryClicked(b unsafe.Pointer, data unsafe.Pointer) {
	withSettingsPanel(data, func(sp *settingsPanel) { sp.doSaveCacheHistory() })
}
