// This file is the pipeline panel's widget-assembly layer (.planning/
// tasks/04-06.json), same split as rightpanel.go/modelpicker.go/
// sessionlist.go: gophermind-osx/ui (PipelineState in ui/pipeline.go,
// PipelinePump in ui/pipelinepump.go, BreakdownSeedPrompt in
// ui/breakdown.go) holds the plain-Go state, SSE-consuming pump, and seed
// prompt text; this file turns that into real libui-ng widgets and is
// where the actual client.PipelineState/PipelineEvents/CreateSession/
// Stream calls live, injected as function values so the plain-Go layer
// never imports gophermind-osx/client or touches cgo.
//
// Same nil-injected-funcs precedent as modelpicker.go/sessionlist.go: no
// live connection exists yet in NewChatWindow (main.go's sendTurn stub
// says as much), so every server call here is nil-safe until a real
// connection exists to wire through.
//
// The node list is a plain rebuilt stack of labels (uiBoxAppend/uiBoxDelete
// plus explicit uiControlDestroy -- uiBoxDelete does not destroy or free
// the removed control, per the installed ui.h, so this file tracks and
// destroys its own old labels on every rebuild rather than leaking them),
// not a uiTable: uiTable's model-handler API is a much larger surface
// (NumColumns/ColumnType/NumRows/CellValue callbacks) that buys column
// sorting and inline editing this read-only status list doesn't need.
// Rebuilding a label stack on every change matches this codebase's
// existing precedent (chatview.go's buildAttributedString also rebuilds
// its whole rendering from scratch on every OnChange) rather than
// introducing a second, heavier live-update mechanism.
package main

/*
#include <ui.h>
#include <stdlib.h>

extern void goPipelinePickBriefClicked(void *b, void *data);
extern void goPipelineStartClicked(void *b, void *data);

static inline void attachPickBriefClicked(uiButton *b, long long h) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goPipelinePickBriefClicked, (void *)h);
}
static inline void attachStartClicked(uiButton *b, long long h) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goPipelineStartClicked, (void *)h);
}

extern void goPipelineShowClicked(void *b, void *data);

static inline void attachShowClicked(uiButton *b, long long h) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goPipelineShowClicked, (void *)h);
}
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"gophermind/gophermind-lib/runview"
	"gophermind/gophermind-osx/client"
	appui "gophermind/gophermind-osx/ui"
)

// StartBreakdownFunc creates a new session and sends prompt as its first
// turn, returning the new session's id -- normally client.CreateSession
// followed by client.Stream(ctx, id, prompt) pumped through a
// appui.StreamPump, injected so this file's construction can be exercised
// without a real server.
// briefPath is the file the prompt was built from. The session needs it to
// pick a working root: the seed prompt refers to the brief and to sibling
// paths relative to the project it belongs to, so a session rooted anywhere
// else cannot open them.
type StartBreakdownFunc func(ctx context.Context, briefPath, prompt string) (sessionID string, err error)

// FetchPipelineFunc fetches the newest run's state, normally
// client.PipelineState with an empty run id (the server picks the newest). A
// zero body and nil error mean "no backend yet": the panel stays empty.
type FetchPipelineFunc func(ctx context.Context) (runview.StateBody, error)

// FetchReportFunc fetches a run's report, normally client.PipelineReport.
type FetchReportFunc func(ctx context.Context, runID string) (runview.ReportBody, error)

// FetchNodeFunc returns one node's detail text, normally appui.LoadNodeDetail
// over the live client.
type FetchNodeFunc func(ctx context.Context, runID, nodeID string) (string, error)

// PipelineLive is what the panel needs from a live connection. The panel is
// built before any connection exists (chatinput.go), so main.go supplies these
// later through setLiveFuncs. Any field may be nil.
type PipelineLive struct {
	Start  StartBreakdownFunc
	Fetch  FetchPipelineFunc
	Report FetchReportFunc
	Node   FetchNodeFunc
	Open   appui.OpenEventsFunc
}

// pipelinePanel is the pipeline section's widgets: a file picker + Start
// Breakdown button, a rebuilt stack of node labels, a wave label,
// and a read-only report text box.
type pipelinePanel struct {
	state  *appui.PipelineState
	window *C.uiWindow
	notify func(string)

	startBreakdown StartBreakdownFunc
	fetchPipeline  FetchPipelineFunc

	box          *C.uiBox
	briefLabel   *C.uiLabel
	startButton  *C.uiButton
	nodeListBox  *C.uiBox
	nodeLabels   []*C.uiLabel
	waveLabel    *C.uiLabel
	reportEntry  *C.uiMultilineEntry
	briefContent string
	briefName    string

	runLabel    *C.uiLabel
	nodeEntry   *C.uiEntry
	showButton  *C.uiButton
	detailEntry *C.uiMultilineEntry

	fetchReport FetchReportFunc
	fetchNode   FetchNodeFunc
	openEvents  appui.OpenEventsFunc

	followMu     sync.Mutex
	followCancel context.CancelFunc
	followGen    uint64 // bumped by stop: a seed begun before it must not start a follower after it
}

var (
	pipelinePanelMu    sync.Mutex
	pipelinePanels     = map[C.longlong]*pipelinePanel{}
	nextPipelineHandle C.longlong
)

// newPipelinePanel builds the pipeline section's widgets and wires them to
// state: state.OnChange rebuilds the node list / wave label / report box
// to match. window is the parent for the native file picker (uiOpenFile).
func newPipelinePanel(state *appui.PipelineState, window *C.uiWindow, notify func(string), startBreakdown StartBreakdownFunc, fetchPipeline FetchPipelineFunc) *pipelinePanel {
	box := C.uiNewVerticalBox()
	C.uiBoxSetPadded(box, 1)

	briefLabel := C.uiNewLabel(C.CString("No brief selected"))
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(briefLabel)), 0)

	pickButton := C.uiNewButton(C.CString("Pick Brief..."))
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(pickButton)), 0)

	startButton := C.uiNewButton(C.CString("Start Breakdown"))
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(startButton)), 0)

	runLabel := C.uiNewLabel(C.CString("No run yet"))
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(runLabel)), 0)

	waveLabel := C.uiNewLabel(C.CString("No wave in progress"))
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(waveLabel)), 0)

	nodeListBox := C.uiNewVerticalBox()
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(nodeListBox)), 0)

	reportEntry := C.uiNewMultilineEntry()
	C.uiMultilineEntrySetReadOnly(reportEntry, 1)
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(reportEntry)), 1)

	nodeEntry := C.uiNewEntry()
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(nodeEntry)), 0)
	showButton := C.uiNewButton(C.CString("Show Node"))
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(showButton)), 0)
	detailEntry := C.uiNewMultilineEntry()
	C.uiMultilineEntrySetReadOnly(detailEntry, 1)
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(detailEntry)), 1)

	pp := &pipelinePanel{
		state: state, window: window, notify: notify,
		startBreakdown: startBreakdown, fetchPipeline: fetchPipeline,
		box: box, briefLabel: briefLabel, startButton: startButton,
		nodeListBox: nodeListBox, waveLabel: waveLabel, reportEntry: reportEntry,
		runLabel: runLabel, nodeEntry: nodeEntry, showButton: showButton, detailEntry: detailEntry,
	}

	pipelinePanelMu.Lock()
	h := nextPipelineHandle
	nextPipelineHandle++
	pipelinePanels[h] = pp
	pipelinePanelMu.Unlock()
	C.attachPickBriefClicked(pickButton, h)
	C.attachStartClicked(startButton, h)
	C.attachShowClicked(showButton, h)

	state.OnChange(func() { queueMain(pp.refresh) })
	pp.refresh()

	pp.seed()

	return pp
}

// setLiveFuncs supplies the connection-dependent callbacks after construction
// and seeds the panel from the newest run, as the constructor does when it is
// handed a fetch up front. The panel is built in chatinput.go before any
// connection exists, so Start Breakdown stays inert until this runs (main.go,
// once the connection manager is up). It is call-once: the fields are written
// without a lock and read by the seed and follower goroutines, so calling it a
// second time while those run is a data race.
func (pp *pipelinePanel) setLiveFuncs(l PipelineLive) {
	pp.startBreakdown = l.Start
	pp.fetchPipeline = l.Fetch
	pp.fetchReport = l.Report
	pp.fetchNode = l.Node
	pp.openEvents = l.Open
	pp.seed()
}

// seed fetches the newest run's state in the background. On success it also
// fetches the report (best effort) and starts following the event stream. A
// server speaking another contract version sets the panel message instead.
// state.SetState fires OnChange, which is wired to queueMain, so this is safe
// to call from a goroutine.
func (pp *pipelinePanel) seed() {
	fetch := pp.fetchPipeline
	if fetch == nil {
		return
	}
	gen := pp.generation()
	go func() {
		if !pp.reload(context.Background()) {
			return
		}
		pp.followAt(gen)
	}()
}

// restart replaces the follower after the connection changed: it stops the
// old one and seeds again (state, report, then a new follower). Disconnect
// stops the follower and nothing else restarts it, and a launch with no
// reachable backend never starts one, so main.go calls this after a connect
// succeeds. It does not block: the fetches run on a goroutine.
func (pp *pipelinePanel) restart() {
	pp.stop()
	pp.seed()
}

// generation returns the current follower generation (see followGen).
func (pp *pipelinePanel) generation() uint64 {
	pp.followMu.Lock()
	defer pp.followMu.Unlock()
	return pp.followGen
}

// reload fetches the newest run's state and, best effort, its report into
// pp.state. It reports whether the state fetch succeeded.
func (pp *pipelinePanel) reload(ctx context.Context) bool {
	body, err := pp.fetchPipeline(ctx)
	if err != nil {
		var mm *client.SchemaMismatchError
		if errors.As(err, &mm) {
			pp.state.SetMessage(client.SchemaMismatchMessage)
		} else if pp.notify != nil {
			queueMain(func() { pp.notify("error fetching pipeline state: " + err.Error()) })
		}
		return false
	}
	pp.state.SetMessage("")
	if body.Run == nil && pp.state.Run() != nil {
		// "No backend yet" (empty body, nil error): keep what is shown
		// rather than wiping the run and nodes.
		return true
	}
	pp.state.SetState(body)
	if pp.fetchReport != nil && body.Run != nil {
		if r, err := pp.fetchReport(ctx, body.Run.RunID); err == nil {
			pp.state.SetReport(r)
		}
	}
	return true
}

// stop cancels the running follower, if any. Call it when the connection it
// reads from goes away (disconnect, app quit).
func (pp *pipelinePanel) stop() {
	pp.followMu.Lock()
	defer pp.followMu.Unlock()
	pp.followGen++
	if pp.followCancel != nil {
		pp.followCancel()
		pp.followCancel = nil
	}
}

// follow starts one PipelineFollower for the current run, unless there is no
// run to follow. A follower already running is cancelled and replaced. The follower reconnects at the
// last event id after a drop and refetches the state on every (re)connect,
// because wave, run-status and report events are never replayed.
func (pp *pipelinePanel) follow() {
	pp.followAt(pp.generation())
}

// followAt is follow for a seed that began at generation gen: when stop ran
// since then, the connection the seed fetched from is gone and it does nothing.
func (pp *pipelinePanel) followAt(gen uint64) {
	pp.followMu.Lock()
	defer pp.followMu.Unlock()
	if gen != pp.followGen || pp.openEvents == nil || pp.state.RunID() == "" {
		return
	}
	if pp.followCancel != nil {
		pp.followCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	pp.followCancel = cancel
	f := &appui.PipelineFollower{
		State: pp.state,
		Open:  pp.openEvents,
		OnOpen: func(ctx context.Context) {
			if pp.fetchPipeline != nil {
				pp.reload(ctx)
			}
		},
	}
	go func() {
		_ = f.Run(ctx)
		pp.followMu.Lock()
		if ctx.Err() == nil { // not superseded or stopped: nobody else owns the slot
			pp.followCancel = nil
		}
		pp.followMu.Unlock()
	}()
}

// Control returns the panel's content as a generic uiControl, for
// rightpanel.go's "Pipeline" section.
func (pp *pipelinePanel) Control() *C.uiControl {
	return (*C.uiControl)(unsafe.Pointer(pp.box))
}

// refresh rebuilds the node list and updates the run, wave and report text to
// match pp.state. Safe to call from state.OnChange (dispatched through
// queueMain, since an event can arrive from the follower's goroutine).
func (pp *pipelinePanel) refresh() {
	for i := len(pp.nodeLabels) - 1; i >= 0; i-- {
		C.uiBoxDelete(pp.nodeListBox, C.int(i))
		C.uiControlDestroy((*C.uiControl)(unsafe.Pointer(pp.nodeLabels[i])))
	}
	pp.nodeLabels = pp.nodeLabels[:0]
	for _, line := range appui.FormatNodeTree(pp.state.Nodes()) {
		cs := C.CString(line)
		label := C.uiNewLabel(cs)
		C.free(unsafe.Pointer(cs))
		C.uiBoxAppend(pp.nodeListBox, (*C.uiControl)(unsafe.Pointer(label)), 0)
		pp.nodeLabels = append(pp.nodeLabels, label)
	}

	head := pp.state.Message()
	if head == "" {
		head = appui.FormatRunHeader(pp.state.Run())
	}
	setLabel(pp.runLabel, head)

	wave, waveState := pp.state.CurrentWave()
	if waveState == "" {
		setLabel(pp.waveLabel, "No wave in progress")
	} else {
		setLabel(pp.waveLabel, fmt.Sprintf("Wave %d: %s", wave, waveState))
	}

	if r := pp.state.Report(); r != nil {
		cs := C.CString(appui.FormatReport(*r))
		C.uiMultilineEntrySetText(pp.reportEntry, cs)
		C.free(unsafe.Pointer(cs))
	} else {
		empty := C.CString("")
		C.uiMultilineEntrySetText(pp.reportEntry, empty)
		C.free(unsafe.Pointer(empty))
	}
}

// reportTextForTest returns the report box's current text.
func (pp *pipelinePanel) reportTextForTest() string {
	c := C.uiMultilineEntryText(pp.reportEntry)
	defer C.uiFreeText(c)
	return C.GoString(c)
}

// setLabel sets a label's text and frees the C copy (libui copies it).
func setLabel(l *C.uiLabel, s string) {
	cs := C.CString(s)
	C.uiLabelSetText(l, cs)
	C.free(unsafe.Pointer(cs))
}

// doShow loads the node whose id is typed into the entry and shows its detail
// (groups, decisions, attempts, saved check output) in the detail box.
func (pp *pipelinePanel) doShow() {
	cText := C.uiEntryText(pp.nodeEntry)
	id := strings.TrimSpace(C.GoString(cText))
	C.uiFreeText(cText)
	run := pp.state.RunID()
	if pp.fetchNode == nil || run == "" || id == "" {
		if pp.notify != nil {
			pp.notify("Connect to a backend with a run, then type a node id such as fn-greet and click Show Node.")
		}
		return
	}
	go func() {
		text, err := pp.fetchNode(context.Background(), run, id)
		queueMain(func() {
			if err != nil {
				text = "error: " + err.Error()
			}
			cs := C.CString(text)
			C.uiMultilineEntrySetText(pp.detailEntry, cs)
			C.free(unsafe.Pointer(cs))
		})
	}()
}

//export goPipelineShowClicked
func goPipelineShowClicked(b unsafe.Pointer, data unsafe.Pointer) {
	h := C.longlong(uintptr(data))
	pipelinePanelMu.Lock()
	pp, ok := pipelinePanels[h]
	pipelinePanelMu.Unlock()
	if !ok {
		return
	}
	pp.doShow()
}

// doPickBrief opens the native file picker and stages its content --
// factored out of goPipelinePickBriefClicked so menubar wiring (the "Open
// Brief"/"New Project" menu items, .planning/tasks/04-08.json) can trigger
// the exact same picker the button does, and so a test could call it
// directly if it didn't need a real dialog (see this file's own test's
// doc comment on why pick-brief itself is never exercised in a test).
func (pp *pipelinePanel) doPickBrief() {
	cPath := C.uiOpenFile(pp.window)
	if cPath == nil {
		return // cancelled
	}
	path := C.GoString(cPath)
	C.uiFreeText(cPath)

	content, err := os.ReadFile(path)
	if err != nil {
		if pp.notify != nil {
			pp.notify("error reading brief: " + err.Error())
		}
		return
	}
	pp.briefName = path
	pp.briefContent = string(content)
	C.uiLabelSetText(pp.briefLabel, C.CString("Brief: "+path))
}

//export goPipelinePickBriefClicked
func goPipelinePickBriefClicked(b unsafe.Pointer, data unsafe.Pointer) {
	h := C.longlong(uintptr(data))
	pipelinePanelMu.Lock()
	pp, ok := pipelinePanels[h]
	pipelinePanelMu.Unlock()
	if !ok {
		return
	}
	pp.doPickBrief()
}

// doStart sends the breakdown-seed prompt for the currently picked brief,
// if any, via startBreakdown -- factored out of goPipelineStartClicked so
// pipelinepanel_test.go can exercise it directly (as sessionlist.go's
// doResume/doCreate/doRename are), without needing to name the cgo
// handle-registry's C.longlong key type, which a _test.go file cannot do.
func (pp *pipelinePanel) doStart() {
	// Say why nothing happened. Both of these used to return silently, so a
	// click on Start Breakdown produced no session, no error and no hint --
	// indistinguishable from a wedged backend.
	if pp.startBreakdown == nil {
		if pp.notify != nil {
			pp.notify("Not connected to a backend, so a breakdown cannot start. Open Settings (gear icon) and click Connect.")
		}
		return
	}
	if pp.briefContent == "" {
		if pp.notify != nil {
			pp.notify("No brief loaded. Click Pick Brief... and choose a Markdown file first.")
		}
		return
	}

	// The brief's base name, not its full path. BreakdownSeedPrompt puts this
	// in "a new software project called %q", and handing it an absolute path
	// told the model the project was named after a file -- which is why it
	// went off trying to read that file instead of using the brief content
	// already embedded in the prompt.
	name := strings.TrimSuffix(filepath.Base(pp.briefName), filepath.Ext(pp.briefName))
	prompt := appui.BreakdownSeedPrompt(name, pp.briefContent)
	go func() {
		_, err := pp.startBreakdown(context.Background(), pp.briefName, prompt)
		if err != nil && pp.notify != nil {
			queueMain(func() { pp.notify("error starting breakdown: " + err.Error()) })
		}
	}()
}

//export goPipelineStartClicked
func goPipelineStartClicked(b unsafe.Pointer, data unsafe.Pointer) {
	h := C.longlong(uintptr(data))
	pipelinePanelMu.Lock()
	pp, ok := pipelinePanels[h]
	pipelinePanelMu.Unlock()
	if !ok {
		return
	}
	pp.doStart()
}
