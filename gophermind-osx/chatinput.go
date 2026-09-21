package main

/*
#include <ui.h>
#include <stdlib.h>
#import <AppKit/AppKit.h>

extern void goSendButtonClicked(void *button, void *data);
extern void goQueueMainCallback(void *data);
extern void goCopyButtonClicked(void *button, void *data);

static inline void attachSendClicked(uiButton *b, long long handle) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goSendButtonClicked, (void *)handle);
}

static inline void attachCopyClicked(uiButton *b, long long handle) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goCopyButtonClicked, (void *)handle);
}

static inline void queueMainDispatch(long long handle) {
	uiQueueMain((void (*)(void *))goQueueMainCallback, (void *)handle);
}

// copyToPasteboard copies a C string to the macOS system pasteboard.
static void copyToPasteboard(const char *text) {
	@autoreleasepool {
		NSPasteboard *pb = [NSPasteboard generalPasteboard];
		[pb clearContents];
		[pb setString:[NSString stringWithUTF8String:text]
			forType:NSPasteboardTypeString];
	}
}
*/
import "C"

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"unsafe"

	"gophermind/gophermind-lib/modelcat"
	"gophermind/gophermind-osx/client"
	appui "gophermind/gophermind-osx/ui"
)

// --- uiQueueMain dispatch: the same handle-registry pattern chatview.go
// uses for uiArea callbacks, applied to queuing an arbitrary Go closure
// onto libui-ng's main/UI thread. This is how a background goroutine
// (Transcript.OnChange fired from the SSE-reading goroutine in
// StreamPump.Run) safely triggers UI work -- libui-ng, like most native
// GUI toolkits, requires all widget access happen on the main thread.

var (
	queueMainMu     sync.Mutex
	queueMainFuncs  = map[C.longlong]func(){}
	nextQueueHandle C.longlong
)

func queueMain(f func()) {
	queueMainMu.Lock()
	h := nextQueueHandle
	nextQueueHandle++
	queueMainFuncs[h] = f
	queueMainMu.Unlock()
	C.queueMainDispatch(h)
}

//export goQueueMainCallback
func goQueueMainCallback(data unsafe.Pointer) {
	h := C.longlong(uintptr(data))
	queueMainMu.Lock()
	f, ok := queueMainFuncs[h]
	delete(queueMainFuncs, h)
	queueMainMu.Unlock()
	if ok {
		f()
	}
}

// chatInput is the message-composition area: a multi-line text field plus
// a Send button (see gophermind-osx/ui.ShouldSend's doc comment on why a
// button, not a Cmd+Enter key handler, actually triggers sending).
type chatInput struct {
	entry  *C.uiMultilineEntry
	button *C.uiButton
	onSend func(text string)
}

var (
	sendHandlerMu  sync.Mutex
	sendHandlers   = map[C.longlong]*chatInput{}
	nextSendHandle C.longlong
)

// newChatInput creates the input field + Send button, calling onSend with
// the field's current text (then clearing it) when the button is clicked.
func newChatInput(onSend func(text string)) *chatInput {
	entry := C.uiNewMultilineEntry()
	button := C.uiNewButton(C.CString("Send"))

	ci := &chatInput{entry: entry, button: button, onSend: onSend}

	sendHandlerMu.Lock()
	h := nextSendHandle
	nextSendHandle++
	sendHandlers[h] = ci
	sendHandlerMu.Unlock()

	C.attachSendClicked(button, h)
	return ci
}

func (c *chatInput) clear() {
	cEmpty := C.CString("")
	defer C.free(unsafe.Pointer(cEmpty))
	C.uiMultilineEntrySetText(c.entry, cEmpty)
}

func (c *chatInput) text() string {
	cText := C.uiMultilineEntryText(c.entry)
	defer C.uiFreeText(cText)
	return C.GoString(cText)
}

//export goSendButtonClicked
func goSendButtonClicked(button unsafe.Pointer, data unsafe.Pointer) {
	h := C.longlong(uintptr(data))
	sendHandlerMu.Lock()
	ci, ok := sendHandlers[h]
	sendHandlerMu.Unlock()
	if !ok {
		return
	}
	text := ci.text()
	if text == "" {
		return
	}
	ci.clear()
	if ci.onSend != nil {
		ci.onSend(text)
	}
}

// --- Copy transcript button ---

var (
	copyHandlerMu  sync.Mutex
	copyHandlers   = map[C.longlong]func(){}
	nextCopyHandle C.longlong
)

//export goCopyButtonClicked
func goCopyButtonClicked(button unsafe.Pointer, data unsafe.Pointer) {
	h := C.longlong(uintptr(data))
	copyHandlerMu.Lock()
	f, ok := copyHandlers[h]
	copyHandlerMu.Unlock()
	if ok {
		f()
	}
}

// attachCopyButton creates a "Copy" button that calls f when clicked.
func attachCopyButton(f func()) *C.uiButton {
	btn := C.uiNewButton(C.CString("Copy"))
	copyHandlerMu.Lock()
	h := nextCopyHandle
	nextCopyHandle++
	copyHandlers[h] = f
	copyHandlerMu.Unlock()
	C.attachCopyClicked(btn, h)
	return btn
}

// ChatWindow assembles the full chat UI (transcript + input) into the
// App's window, and wires a session stream's events into the transcript
// via ui.StreamPump. This is 04-01's actual top-level entry point; 04-03
// added the collapsible right panel alongside it.
type ChatWindow struct {
	App        *App
	Transcript *appui.Transcript
	Panel      *appui.PanelState
	Model      *appui.ModelPickerState
	Sessions   *appui.SessionListState
	Pipeline   *appui.PipelineState
	Backends   *appui.BackendListState
	Approvals  *appui.ApprovalTracker
	area       *chatArea
	input      *chatInput
	panel      *rightPanel
	modelUI    *modelPicker
	sessionsUI *sessionList
	pipelineUI *pipelinePanel
	settingsUI *settingsPanel
}

// NewChatWindow builds the chat UI as app's window content. sendTurn is
// called (from the Send button, on the UI thread) with the composed text
// each time the user sends a message; it's the caller's job to start a
// session stream and run a StreamPump against Chat.Transcript -- this
// constructor only wires the widgets, not any particular backend
// connection (that's the connection manager, gophermind-osx/connection,
// from plan 03-03).
func NewChatWindow(app *App, sendTurn func(text string)) *ChatWindow {
	transcript := appui.NewTranscript()

	// No connection is wired at construction time (same nil-injected-funcs
	// precedent as modelState/sessionState below), so the tracker's
	// ApproveFunc is a no-op for now: a real connection replaces it with
	// client.Approve once one exists (03-03's connection manager).
	approvals := appui.NewApprovalTracker(func(ctx context.Context, sessionID, approvalID string, approved bool) error {
		return nil
	})
	area := newChatArea(transcript, approvals)
	approvalBar := newApprovalBar(approvals)
	input := newChatInput(sendTurn)

	panelState := loadPanelState()
	panelState.OnChange(func() { savePanelState(panelState) })

	// No connection is wired at construction time (see the doc comment
	// above and main.go's identical stub for sendTurn), so the picker
	// starts with an empty catalogue and a nil PinModelFunc (a no-op click,
	// per modelpicker.go's pinSelected); a real connection wires
	// cw.Model.SetEntries/SetCurrentKey and a real PinModelFunc once one
	// exists (03-03's connection manager), the same way it will eventually
	// replace sendTurn's stub.
	modelState := appui.NewModelPickerState(nil, modelcat.Settings{})
	modelUI := newModelPicker(modelState, nil, transcript.AddSystem)

	// Same nil-injected-funcs precedent as modelState/modelUI above (see
	// this constructor's doc comment): no live connection exists yet to
	// list/create/rename/delete/resume a session through.
	sessionState := appui.NewSessionListState()
	sessionsUI := newSessionList(sessionState, app.window, transcript, transcript.AddSystem, nil, nil, nil, nil, nil)

	// Same nil-injected-funcs precedent as modelState/sessionState above:
	// no live connection exists yet to fetch pipeline state or start a
	// breakdown through.
	pipelineState := appui.NewPipelineState()
	pipelineUI := newPipelinePanel(pipelineState, app.window, transcript.AddSystem, nil, nil)

	panel := newRightPanel(panelState, modelUI.Control(), sessionsUI.Control(), pipelineUI.Control())

	// Same nil-injected-funcs precedent as the other sections: no live
	// connection/server calls exist yet for backends, model-settings
	// persistence, skills, or endpoint switching. Persisted profiles are
	// restored here (backendstore.go), non-secret metadata from
	// backends.json plus each one's bearer token from the Keychain --
	// actually reconnecting them is main.go's job, once connectFunc exists.
	backendState := appui.NewBackendListState()
	for _, p := range loadBackendProfiles() {
		p.Token = loadBackendToken(p.Name)
		backendState.Add(p)
	}
	cacheHistory := loadCacheHistorySettings()
	settingsUI := newSettingsPanel(app.window, backendState, modelState, cacheHistory, saveCacheHistorySettings,
		nil, nil, nil, nil, nil, nil, nil, nil, saveBackendProfiles, saveBackendToken)

	// left is the chat column: the panel's toggle button, the Copy button
	// (copies the transcript to the clipboard), and the settings gear
	// button (all must stay visible even when the panel itself is hidden
	// -- otherwise there'd be no way to bring either back), the
	// transcript, then the input row.
	topRow := C.uiNewHorizontalBox()
	C.uiBoxSetPadded(topRow, 1)
	C.uiBoxAppend(topRow, panel.ToggleControl(), 0)
	copyBtn := attachCopyButton(func() {
		var sb strings.Builder
		for _, m := range transcript.Messages() {
			sb.WriteString(m.Text)
			sb.WriteString("\n\n")
		}
		cText := C.CString(sb.String())
		C.copyToPasteboard(cText)
		C.free(unsafe.Pointer(cText))
		transcript.AddSystem("Transcript copied to clipboard.")
	})
	C.uiBoxAppend(topRow, (*C.uiControl)(unsafe.Pointer(copyBtn)), 0)
	C.uiBoxAppend(topRow, settingsUI.GearControl(), 0)

	// Input row: the multiline entry and Send button side by side, so the
	// entry gets most of the width and the button is always visible next
	// to it. A label above makes it clear where to type.
	inputRow := C.uiNewHorizontalBox()
	C.uiBoxSetPadded(inputRow, 1)
	C.uiBoxAppend(inputRow, (*C.uiControl)(unsafe.Pointer(input.entry)), 1) // stretchy
	C.uiBoxAppend(inputRow, (*C.uiControl)(unsafe.Pointer(input.button)), 0)

	inputLabel := C.uiNewLabel(C.CString("Message (click here to type, then Send):"))

	left := C.uiNewVerticalBox()
	C.uiBoxSetPadded(left, 1)
	C.uiBoxAppend(left, (*C.uiControl)(unsafe.Pointer(topRow)), 0)
	C.uiBoxAppend(left, area.Control(), 1) // stretchy: takes remaining space
	C.uiBoxAppend(left, approvalBar.Control(), 0)
	C.uiBoxAppend(left, (*C.uiControl)(unsafe.Pointer(inputLabel)), 0)
	C.uiBoxAppend(left, (*C.uiControl)(unsafe.Pointer(inputRow)), 0)

	root := C.uiNewHorizontalBox()
	C.uiBoxSetPadded(root, 1)
	C.uiBoxAppend(root, (*C.uiControl)(unsafe.Pointer(left)), 1) // stretchy
	C.uiBoxAppend(root, panel.PanelControl(), 0)

	C.uiWindowSetChild(app.window, (*C.uiControl)(unsafe.Pointer(root)))

	// Menu bar wiring (.planning/tasks/04-08.json): the menu items
	// themselves are built in NewApp, before ChatWindow exists, so the
	// actions are attached here instead -- see App.WireMenuActions' doc
	// comment. Toggle Panel calls both Toggle() and syncVisibility()
	// because rightpanel.go's own toggle button does the same (see
	// syncVisibility's doc comment on why PanelState.OnChange's single
	// callback slot is already spoken for by persistence). Toggle Dark
	// Mode is a documented no-op: libui-ng exposes no appearance API (see
	// NewApp's doc comment).
	app.WireMenuActions(
		func() { pipelineUI.doPickBrief(); pipelineUI.doStart() },
		pipelineUI.doPickBrief,
		func() { openExamplesDir(transcript.AddSystem) },
		settingsUI.doOpen,
		func() { panelState.Toggle(); panel.syncVisibility() },
		func() {
			transcript.AddSystem("Dark mode isn't switchable from here: libui-ng has no appearance API, so the app already just follows the system's light/dark setting on its own.")
		},
	)

	return &ChatWindow{App: app, Transcript: transcript, Panel: panelState, Model: modelState, Sessions: sessionState, Pipeline: pipelineState, Backends: backendState, Approvals: approvals, area: area, input: input, panel: panel, modelUI: modelUI, sessionsUI: sessionsUI, pipelineUI: pipelineUI, settingsUI: settingsUI}
}

// RunTurn sends task as a user message, opens a stream via streamFn, and
// pumps its events into cw.Transcript in a background goroutine -- the
// concrete example of "SSE reading in goroutine, UI updates via channel"
// this whole file exists to wire up. streamFn is typically
// conn.Client().Stream(ctx, sessionID, task) from a connected
// gophermind-osx/connection.Connection.
func (cw *ChatWindow) RunTurn(ctx context.Context, sessionID, task string, streamFn func(context.Context, string) (*client.EventStream, error)) {
	cw.Transcript.AddUserMessage(task)
	stream, err := streamFn(ctx, task)
	if err != nil {
		cw.Transcript.AddSystem("error: " + err.Error())
		return
	}
	pump := &appui.StreamPump{Transcript: cw.Transcript}

	// Route approval requests to the tracker, which renders the card and
	// owns the approve/deny decision.
	//
	// Nothing set this before, so every "approval-needed" event was dropped
	// on the floor. The server's gate does not give up when it is ignored:
	// it blocks the turn for five minutes, auto-denies, and the agent moves
	// on to its next tool call, which blocks for another five. From the
	// outside the app simply stops after the last tool output, with "No
	// pending approvals" still showing, because the card it would have shown
	// was never created.
	pump.OnApprovalNeeded = func(ev client.Event) {
		var a struct {
			ApprovalID string `json:"approval_id"`
			Tool       string `json:"tool"`
			Args       string `json:"args"`
		}
		if err := json.Unmarshal([]byte(ev.Data), &a); err != nil || a.ApprovalID == "" {
			cw.Transcript.AddSystem("received an approval request that could not be read; the turn will stall until it times out")
			return
		}
		cw.Approvals.Add(sessionID, a.ApprovalID, a.Tool, a.Args)
	}
	go func() {
		// A genuine mid-stream failure (network drop, server crash) must
		// reach the user the same way a failure to even open the stream
		// already does above -- otherwise the transcript just goes silent
		// with no sign anything went wrong. A clean end (nil, or ctx being
		// cancelled because the window closed) reports nothing.
		//
		// AddSystem is called directly, not via queueMain: like
		// AddUserMessage above, it only mutates the plain-Go, mutex-safe
		// Transcript (safe from any goroutine); it is Transcript.OnChange's
		// own callback (wired by newChatArea) that dispatches the actual
		// redraw through queueMain, not this call.
		if err := pump.Run(ctx, stream); err != nil && ctx.Err() == nil {
			cw.Transcript.AddSystem("error: " + err.Error())
		}
	}()
}
