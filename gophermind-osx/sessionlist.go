// This file is the session list's widget-assembly layer (.planning/tasks/
// 04-05.json), same split as modelpicker.go: gophermind-osx/ui
// (SessionListState in ui/sessionlist.go, ApplyHistory in ui/history.go)
// holds the plain-Go session list, selection, and history-replay logic;
// this file turns that into real libui-ng widgets and is where the actual
// client.ListSessions/CreateSession/RenameSession/DeleteSession/
// SessionConfig/SessionMessages calls live, injected as function values so
// SessionListState itself never imports gophermind-osx/client or touches
// cgo.
//
// Like modelpicker.go's PinModelFunc, every injected func here is nil in
// NewChatWindow today: there is still no live backend connection wired
// into the chat window (main.go's sendTurn stub says as much), so listing,
// creating, renaming, deleting, and resuming a session all need a real
// gophermind-osx/connection.Connection to call through, which is a
// separate integration step, not this task's job. What IS built and
// tested here is the full widget behavior and the ApplyHistory replay
// logic (ui/history.go) that "resume" will call once a connection exists.
//
// A session list here is a uiCombobox (like modelpicker.go's catalogue
// combo), not a uiTable: uiTable's real backing API
// (uiTableModelHandler's C function-pointer struct) is a much larger cgo
// surface for the same practical capability this feature actually needs
// -- one row selected at a time, no inline editing, no sortable columns --
// and this codebase already prefers the simpler combobox composition
// (modelpicker.go's catalogue and preference-order lists) over uiTable
// elsewhere. Checked against /opt/homebrew/include/ui.h before choosing
// this, not guessed.
//
// libui-ng has no native yes/no confirm dialog (only uiMsgBox/
// uiMsgBoxError, which are informational, no return value -- checked
// against ui.h). "Delete with confirm dialog" is instead a small secondary
// uiWindow with Confirm/Cancel buttons (confirmWindow below), the same
// class of honest, checked substitution rightpanel.go's toggle-only panel
// and modelpicker.go's up/down-instead-of-drag already document.
package main

/*
#include <ui.h>
#include <stdlib.h>

extern void goSessionResumeClicked(void *b, void *data);
extern void goSessionRenameClicked(void *b, void *data);
extern void goSessionDeleteClicked(void *b, void *data);
extern void goSessionChooseRootClicked(void *b, void *data);
extern void goSessionCreateClicked(void *b, void *data);
extern void goSessionModeSelected(void *c, void *data);
extern void goConfirmYesClicked(void *b, void *data);
extern void goConfirmNoClicked(void *b, void *data);

static inline void attachSessionResumeClicked(uiButton *b, long long h) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goSessionResumeClicked, (void *)h);
}
static inline void attachSessionRenameClicked(uiButton *b, long long h) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goSessionRenameClicked, (void *)h);
}
static inline void attachSessionDeleteClicked(uiButton *b, long long h) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goSessionDeleteClicked, (void *)h);
}
static inline void attachSessionChooseRootClicked(uiButton *b, long long h) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goSessionChooseRootClicked, (void *)h);
}
static inline void attachSessionCreateClicked(uiButton *b, long long h) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goSessionCreateClicked, (void *)h);
}
static inline void attachSessionModeSelected(uiCombobox *c, long long h) {
	uiComboboxOnSelected(c, (void (*)(uiCombobox *, void *))goSessionModeSelected, (void *)h);
}
static inline void attachConfirmYesClicked(uiButton *b, long long h) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goConfirmYesClicked, (void *)h);
}
static inline void attachConfirmNoClicked(uiButton *b, long long h) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goConfirmNoClicked, (void *)h);
}
*/
import "C"

import (
	"context"
	"fmt"
	"sync"
	"unsafe"

	"gophermind/gophermind-lib/llm"
	appui "gophermind/gophermind-osx/ui"
)

// ListSessionsFunc lists every known session across whatever backends are
// configured, each tagged with the backend it came from -- normally
// merging client.ListSessions(ctx) per gophermind-osx/connection.Connection
// with that connection's name as SessionEntry.Backend.
type ListSessionsFunc func(ctx context.Context) ([]appui.SessionEntry, error)

// CreateSessionFunc creates a new session with the given mode/root,
// returning its id -- normally client.CreateSession with
// CreateSessionOptions{Mode: mode, Root: root}.
type CreateSessionFunc func(ctx context.Context, mode, root string) (id string, err error)

// RenameSessionFunc renames a session -- normally client.RenameSession.
type RenameSessionFunc func(ctx context.Context, id, name string) error

// DeleteSessionFunc deletes a session -- normally client.DeleteSession.
type DeleteSessionFunc func(ctx context.Context, id string) error

// ResumeSessionFunc fetches a session's config and full message history so
// it can be attached to the chat window -- normally client.SessionConfig
// plus client.SessionMessages, decoded into []llm.Message and handed to
// appui.ApplyHistory by the caller.
type ResumeSessionFunc func(ctx context.Context, id string) (appui.SessionConfig, []llm.Message, error)

// sessionList is the session list section's cgo widgets, bound to one
// appui.SessionListState.
type sessionList struct {
	state *appui.SessionListState

	listCombo *C.uiCombobox
	listIDs   []string // parallel to listCombo's items, rebuilt by refresh

	resumeButton *C.uiButton
	renameEntry  *C.uiEntry
	renameButton *C.uiButton
	deleteButton *C.uiButton

	configLabel *C.uiLabel

	modeCombo        *C.uiCombobox
	rootLabel        *C.uiLabel
	chooseRootButton *C.uiButton
	createButton     *C.uiButton

	box *C.uiBox

	window     *C.uiWindow // parent, for uiOpenFolder and the confirm dialog
	list       ListSessionsFunc
	create     CreateSessionFunc
	rename     RenameSessionFunc
	deleteFn   DeleteSessionFunc
	resume     ResumeSessionFunc
	transcript *appui.Transcript
	notify     func(string)
}

var (
	sessionListMu       sync.Mutex
	sessionListRegistry = map[C.longlong]*sessionList{}
	nextSessionList     C.longlong
)

// newSessionList builds the session list's widgets and wires them to
// state. Every *Func parameter may be nil (see this file's top doc comment
// on why): a nil func makes the corresponding action a silent no-op rather
// than a panic, matching modelpicker.go's pinSelected precedent for
// pin == nil.
func newSessionList(state *appui.SessionListState, window *C.uiWindow, transcript *appui.Transcript, notify func(string), list ListSessionsFunc, create CreateSessionFunc, rename RenameSessionFunc, deleteFn DeleteSessionFunc, resume ResumeSessionFunc) *sessionList {
	sl := &sessionList{
		state:            state,
		listCombo:        C.uiNewCombobox(),
		resumeButton:     newCButton("Resume"),
		renameEntry:      C.uiNewEntry(),
		renameButton:     newCButton("Rename"),
		deleteButton:     newCButton("Delete"),
		configLabel:      newCLabel(""),
		modeCombo:        C.uiNewCombobox(),
		rootLabel:        newCLabel("(no root chosen)"),
		chooseRootButton: newCButton("Choose Folder"),
		createButton:     newCButton("New Session"),
		window:           window,
		list:             list,
		create:           create,
		rename:           rename,
		deleteFn:         deleteFn,
		resume:           resume,
		transcript:       transcript,
		notify:           notify,
	}

	sessionListMu.Lock()
	h := nextSessionList
	nextSessionList++
	sessionListRegistry[h] = sl
	sessionListMu.Unlock()

	C.attachSessionResumeClicked(sl.resumeButton, h)
	C.attachSessionRenameClicked(sl.renameButton, h)
	C.attachSessionDeleteClicked(sl.deleteButton, h)
	C.attachSessionChooseRootClicked(sl.chooseRootButton, h)
	C.attachSessionCreateClicked(sl.createButton, h)
	C.attachSessionModeSelected(sl.modeCombo, h)

	for _, m := range appui.SessionModes {
		cMode := C.CString(m)
		C.uiComboboxAppend(sl.modeCombo, cMode)
		C.free(unsafe.Pointer(cMode))
	}
	C.uiComboboxSetSelected(sl.modeCombo, 0)

	actions := C.uiNewHorizontalBox()
	C.uiBoxSetPadded(actions, 1)
	C.uiBoxAppend(actions, (*C.uiControl)(unsafe.Pointer(sl.resumeButton)), 0)
	C.uiBoxAppend(actions, (*C.uiControl)(unsafe.Pointer(sl.deleteButton)), 0)

	renameRow := C.uiNewHorizontalBox()
	C.uiBoxSetPadded(renameRow, 1)
	C.uiBoxAppend(renameRow, (*C.uiControl)(unsafe.Pointer(sl.renameEntry)), 1)
	C.uiBoxAppend(renameRow, (*C.uiControl)(unsafe.Pointer(sl.renameButton)), 0)

	newRow := C.uiNewHorizontalBox()
	C.uiBoxSetPadded(newRow, 1)
	C.uiBoxAppend(newRow, (*C.uiControl)(unsafe.Pointer(sl.chooseRootButton)), 0)
	C.uiBoxAppend(newRow, (*C.uiControl)(unsafe.Pointer(sl.createButton)), 0)

	box := C.uiNewVerticalBox()
	C.uiBoxSetPadded(box, 1)
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(sl.listCombo)), 0)
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(actions)), 0)
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(renameRow)), 0)
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(sl.configLabel)), 0)
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(sl.modeCombo)), 0)
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(sl.rootLabel)), 0)
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(newRow)), 0)
	sl.box = box

	state.OnChange(sl.refresh)
	sl.refreshList()
	sl.refresh()

	return sl
}

// Control returns the section's box, for embedding into the right panel's
// "Sessions" section.
func (sl *sessionList) Control() *C.uiControl {
	return (*C.uiControl)(unsafe.Pointer(sl.box))
}

// attachControlForTest sets win's child to c. Exists so sessionlist_test.go
// (which, like every _test.go file, cannot import "C" itself -- a Go
// toolchain restriction) can parent a standalone sessionList's widgets
// under a real window before App.Close's leak check runs; a widget never
// added to a shown window's tree is reported as leaked at uiUninit. Same
// purpose as chatview.go's freeAttributedString: a plain-Go wrapper around
// one C call, solely for test files to reach.
func attachControlForTest(win *C.uiWindow, c *C.uiControl) {
	C.uiWindowSetChild(win, c)
}

// refresh rebuilds every widget from state: the session combo (title,
// message count, mod time, backend tag), the config label for the
// resumed session, and the root label. Called on construction and on
// every state.OnChange.
func (sl *sessionList) refresh() {
	entries := sl.state.Entries()
	sl.listIDs = sl.listIDs[:0]

	C.uiComboboxClear(sl.listCombo)
	for _, e := range entries {
		sl.listIDs = append(sl.listIDs, e.Info.ID)
		title := e.Info.Title
		if e.Info.Name != "" {
			title = e.Info.Name
		}
		label := fmt.Sprintf("%s (%d msgs, %s, %s)", title, e.Info.Messages, e.Info.ModTime.Format("2006-01-02 15:04"), e.Backend)
		cLabel := C.CString(label)
		C.uiComboboxAppend(sl.listCombo, cLabel)
		C.free(unsafe.Pointer(cLabel))
	}
	for i, id := range sl.listIDs {
		if id == sl.state.SelectedID() {
			C.uiComboboxSetSelected(sl.listCombo, C.int(i))
			break
		}
	}

	cfg := sl.state.Config()
	configText := "(no session resumed)"
	if sl.state.SelectedID() != "" {
		configText = fmt.Sprintf("model: %s  mode: %s  root: %s", cfg.Model, cfg.Mode, cfg.Root)
	}
	cConfig := C.CString(configText)
	C.uiLabelSetText(sl.configLabel, cConfig)
	C.free(unsafe.Pointer(cConfig))

	rootText := sl.state.NewRoot()
	if rootText == "" {
		rootText = "(no root chosen)"
	}
	cRoot := C.CString(rootText)
	C.uiLabelSetText(sl.rootLabel, cRoot)
	C.free(unsafe.Pointer(cRoot))
}

// selectedID returns the combo's currently selected session id, "" if none.
func (sl *sessionList) selectedID() string {
	i := int(C.uiComboboxSelected(sl.listCombo))
	if i < 0 || i >= len(sl.listIDs) {
		return ""
	}
	return sl.listIDs[i]
}

// doResume attaches to id: fetches its config and history via resume, then
// replays the history into transcript -- covers "resume: click session to
// attach, chat transcript loads" and "session config: model, mode, root
// displayed on resume."
func (sl *sessionList) doResume(id string) {
	if id == "" || sl.resume == nil {
		return
	}
	cfg, msgs, err := sl.resume(context.Background(), id)
	if err != nil {
		if sl.notify != nil {
			sl.notify("Failed to resume session: " + err.Error())
		}
		return
	}
	sl.state.Select(id)
	sl.state.SetConfig(cfg)
	if sl.transcript != nil {
		appui.ApplyHistory(sl.transcript, msgs)
	}
}

// doDelete deletes id after the user confirms via confirmWindow.
func (sl *sessionList) doDelete(id string) {
	if id == "" || sl.deleteFn == nil {
		return
	}
	confirmWindow(sl.window, "Delete Session", "Delete this session? This cannot be undone.", func() {
		if err := sl.deleteFn(context.Background(), id); err != nil {
			if sl.notify != nil {
				sl.notify("Failed to delete session: " + err.Error())
			}
			return
		}
		sl.state.RemoveLocal(id)
	})
}

// doRename renames id to the renameEntry's current text.
func (sl *sessionList) doRename(id string) {
	name := C.GoString(C.uiEntryText(sl.renameEntry))
	if id == "" || name == "" || sl.rename == nil {
		return
	}
	if err := sl.rename(context.Background(), id, name); err != nil {
		if sl.notify != nil {
			sl.notify("Failed to rename session: " + err.Error())
		}
		return
	}
	sl.state.RenameLocal(id, name)
}

// doChooseRoot opens the native folder picker and stages the result.
func (sl *sessionList) doChooseRoot() {
	cPath := C.uiOpenFolder(sl.window)
	if cPath == nil {
		return
	}
	defer C.uiFreeText(cPath)
	sl.state.SetNewRoot(C.GoString(cPath))
}

// doCreate creates a new session with the staged mode/root, refreshes the
// list (so the new session's real Info/backend tag shows up rather than a
// synthesized placeholder), and attaches to it.
func (sl *sessionList) doCreate() {
	if sl.create == nil {
		return
	}
	mode, root := sl.state.NewMode(), sl.state.NewRoot()
	id, err := sl.create(context.Background(), mode, root)
	if err != nil {
		if sl.notify != nil {
			sl.notify("Failed to create session: " + err.Error())
		}
		return
	}
	sl.refreshList()
	sl.doResume(id)
}

// refreshList re-fetches the session list via list, if set, and applies it
// to state. A no-op when list is nil (see this file's top doc comment).
func (sl *sessionList) refreshList() {
	if sl.list == nil {
		return
	}
	entries, err := sl.list(context.Background())
	if err != nil {
		if sl.notify != nil {
			sl.notify("Failed to list sessions: " + err.Error())
		}
		return
	}
	sl.state.SetEntries(entries)
}

//export goSessionResumeClicked
func goSessionResumeClicked(b unsafe.Pointer, data unsafe.Pointer) {
	withSessionList(data, func(sl *sessionList) { sl.doResume(sl.selectedID()) })
}

//export goSessionRenameClicked
func goSessionRenameClicked(b unsafe.Pointer, data unsafe.Pointer) {
	withSessionList(data, func(sl *sessionList) { sl.doRename(sl.selectedID()) })
}

//export goSessionDeleteClicked
func goSessionDeleteClicked(b unsafe.Pointer, data unsafe.Pointer) {
	withSessionList(data, func(sl *sessionList) { sl.doDelete(sl.selectedID()) })
}

//export goSessionChooseRootClicked
func goSessionChooseRootClicked(b unsafe.Pointer, data unsafe.Pointer) {
	withSessionList(data, func(sl *sessionList) { sl.doChooseRoot() })
}

//export goSessionCreateClicked
func goSessionCreateClicked(b unsafe.Pointer, data unsafe.Pointer) {
	withSessionList(data, func(sl *sessionList) { sl.doCreate() })
}

//export goSessionModeSelected
func goSessionModeSelected(c unsafe.Pointer, data unsafe.Pointer) {
	withSessionList(data, func(sl *sessionList) {
		i := int(C.uiComboboxSelected(sl.modeCombo))
		if i >= 0 && i < len(appui.SessionModes) {
			sl.state.SetNewMode(appui.SessionModes[i])
		}
	})
}

// withSessionList looks up the *sessionList registered under data's handle
// and calls f with it, matching the handle-registry pattern chatview.go,
// chatinput.go, rightpanel.go, and modelpicker.go each use for their own
// libui-ng callbacks.
func withSessionList(data unsafe.Pointer, f func(sl *sessionList)) {
	h := C.longlong(uintptr(data))
	sessionListMu.Lock()
	sl, ok := sessionListRegistry[h]
	sessionListMu.Unlock()
	if !ok {
		return
	}
	f(sl)
}

// --- confirm dialog: libui-ng has no native yes/no dialog (see this
// file's top doc comment) ---

type confirmDialog struct {
	window    *C.uiWindow
	onConfirm func()
}

var (
	confirmMu       sync.Mutex
	confirmRegistry = map[C.longlong]*confirmDialog{}
	nextConfirm     C.longlong
)

// confirmWindow shows a small secondary window titled title, with message
// and Yes/No buttons; onConfirm runs (on the UI thread, since it's called
// from a button-click callback) only if the user clicks Yes. The window
// closes itself either way.
func confirmWindow(parent *C.uiWindow, title, message string, onConfirm func()) {
	cTitle := C.CString(title)
	defer C.free(unsafe.Pointer(cTitle))
	win := C.uiNewWindow(cTitle, 360, 100, 0)

	label := newCLabel(message)
	yes := newCButton("Yes")
	no := newCButton("No")

	buttons := C.uiNewHorizontalBox()
	C.uiBoxSetPadded(buttons, 1)
	C.uiBoxAppend(buttons, (*C.uiControl)(unsafe.Pointer(yes)), 1)
	C.uiBoxAppend(buttons, (*C.uiControl)(unsafe.Pointer(no)), 1)

	box := C.uiNewVerticalBox()
	C.uiBoxSetPadded(box, 1)
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(label)), 0)
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(buttons)), 0)
	C.uiWindowSetChild(win, (*C.uiControl)(unsafe.Pointer(box)))

	cd := &confirmDialog{window: win, onConfirm: onConfirm}
	confirmMu.Lock()
	h := nextConfirm
	nextConfirm++
	confirmRegistry[h] = cd
	confirmMu.Unlock()

	C.attachConfirmYesClicked(yes, h)
	C.attachConfirmNoClicked(no, h)

	C.uiControlShow((*C.uiControl)(unsafe.Pointer(win)))
}

//export goConfirmYesClicked
func goConfirmYesClicked(b unsafe.Pointer, data unsafe.Pointer) {
	h := C.longlong(uintptr(data))
	confirmMu.Lock()
	cd, ok := confirmRegistry[h]
	delete(confirmRegistry, h)
	confirmMu.Unlock()
	if !ok {
		return
	}
	C.uiControlDestroy((*C.uiControl)(unsafe.Pointer(cd.window)))
	if cd.onConfirm != nil {
		cd.onConfirm()
	}
}

//export goConfirmNoClicked
func goConfirmNoClicked(b unsafe.Pointer, data unsafe.Pointer) {
	h := C.longlong(uintptr(data))
	confirmMu.Lock()
	cd, ok := confirmRegistry[h]
	delete(confirmRegistry, h)
	confirmMu.Unlock()
	if !ok {
		return
	}
	C.uiControlDestroy((*C.uiControl)(unsafe.Pointer(cd.window)))
}
