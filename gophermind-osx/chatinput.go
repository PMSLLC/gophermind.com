package main

/*
#cgo CFLAGS: -I/opt/homebrew/include
#cgo LDFLAGS: -L/opt/homebrew/lib -lui
#include <ui.h>
#include <stdlib.h>

extern void goSendButtonClicked(void *button, void *data);
extern void goQueueMainCallback(void *data);

static inline void attachSendClicked(uiButton *b, long long handle) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goSendButtonClicked, (void *)handle);
}

static inline void queueMainDispatch(long long handle) {
	uiQueueMain((void (*)(void *))goQueueMainCallback, (void *)handle);
}
*/
import "C"

import (
	"context"
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

// ChatWindow assembles the full chat UI (transcript + input) into the
// App's window, and wires a session stream's events into the transcript
// via ui.StreamPump. This is 04-01's actual top-level entry point; 04-03
// added the collapsible right panel alongside it.
type ChatWindow struct {
	App        *App
	Transcript *appui.Transcript
	Panel      *appui.PanelState
	Model      *appui.ModelPickerState
	area       *chatArea
	input      *chatInput
	panel      *rightPanel
	modelUI    *modelPicker
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
	area := newChatArea(transcript)
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
	panel := newRightPanel(panelState, modelUI.Control())

	// left is the chat column: the panel's toggle button (which must stay
	// visible even when the panel itself is hidden -- otherwise there'd be
	// no way to bring it back), the transcript, then the input row.
	left := C.uiNewVerticalBox()
	C.uiBoxSetPadded(left, 1)
	C.uiBoxAppend(left, panel.ToggleControl(), 0)
	C.uiBoxAppend(left, area.Control(), 1) // stretchy: takes remaining space
	C.uiBoxAppend(left, (*C.uiControl)(unsafe.Pointer(input.entry)), 0)
	C.uiBoxAppend(left, (*C.uiControl)(unsafe.Pointer(input.button)), 0)

	root := C.uiNewHorizontalBox()
	C.uiBoxSetPadded(root, 1)
	C.uiBoxAppend(root, (*C.uiControl)(unsafe.Pointer(left)), 1) // stretchy
	C.uiBoxAppend(root, panel.PanelControl(), 0)

	C.uiWindowSetChild(app.window, (*C.uiControl)(unsafe.Pointer(root)))

	return &ChatWindow{App: app, Transcript: transcript, Panel: panelState, Model: modelState, area: area, input: input, panel: panel, modelUI: modelUI}
}

// RunTurn sends task as a user message, opens a stream via streamFn, and
// pumps its events into cw.Transcript in a background goroutine -- the
// concrete example of "SSE reading in goroutine, UI updates via channel"
// this whole file exists to wire up. streamFn is typically
// conn.Client().Stream(ctx, sessionID, task) from a connected
// gophermind-osx/connection.Connection.
func (cw *ChatWindow) RunTurn(ctx context.Context, task string, streamFn func(context.Context, string) (*client.EventStream, error)) {
	cw.Transcript.AddUserMessage(task)
	stream, err := streamFn(ctx, task)
	if err != nil {
		cw.Transcript.AddSystem("error: " + err.Error())
		return
	}
	pump := &appui.StreamPump{Transcript: cw.Transcript}
	go pump.Run(ctx, stream)
}
