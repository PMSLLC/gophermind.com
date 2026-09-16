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
// The task list is a plain rebuilt stack of labels (uiBoxAppend/uiBoxDelete
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
*/
import "C"

import (
	"context"
	"fmt"
	"os"
	"sync"
	"unsafe"

	"gophermind/gophermind-lib/phaseflow"
	"gophermind/gophermind-osx/client"
	appui "gophermind/gophermind-osx/ui"
)

// StartBreakdownFunc creates a new session and sends prompt as its first
// turn, returning the new session's id -- normally client.CreateSession
// followed by client.Stream(ctx, id, prompt) pumped through a
// appui.StreamPump, injected so this file's construction can be exercised
// without a real server.
type StartBreakdownFunc func(ctx context.Context, prompt string) (sessionID string, err error)

// FetchPipelineFunc fetches the current task snapshot, normally
// client.PipelineState.
type FetchPipelineFunc func(ctx context.Context) ([]phaseflow.Task, error)

// pipelinePanel is the pipeline section's widgets: a file picker + Start
// Breakdown button, a rebuilt stack of task-status labels, a wave label,
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
	taskListBox  *C.uiBox
	taskLabels   []*C.uiLabel
	waveLabel    *C.uiLabel
	reportEntry  *C.uiMultilineEntry
	briefContent string
	briefName    string
}

var (
	pipelinePanelMu    sync.Mutex
	pipelinePanels     = map[C.longlong]*pipelinePanel{}
	nextPipelineHandle C.longlong
)

// newPipelinePanel builds the pipeline section's widgets and wires them to
// state: state.OnChange rebuilds the task list / wave label / report box
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

	waveLabel := C.uiNewLabel(C.CString("No wave in progress"))
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(waveLabel)), 0)

	taskListBox := C.uiNewVerticalBox()
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(taskListBox)), 0)

	reportEntry := C.uiNewMultilineEntry()
	C.uiMultilineEntrySetReadOnly(reportEntry, 1)
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(reportEntry)), 1)

	pp := &pipelinePanel{
		state: state, window: window, notify: notify,
		startBreakdown: startBreakdown, fetchPipeline: fetchPipeline,
		box: box, briefLabel: briefLabel, startButton: startButton,
		taskListBox: taskListBox, waveLabel: waveLabel, reportEntry: reportEntry,
	}

	pipelinePanelMu.Lock()
	h := nextPipelineHandle
	nextPipelineHandle++
	pipelinePanels[h] = pp
	pipelinePanelMu.Unlock()
	C.attachPickBriefClicked(pickButton, h)
	C.attachStartClicked(startButton, h)

	state.OnChange(func() { queueMain(pp.refresh) })
	pp.refresh()

	// Seed the initial task snapshot (GET /pipeline/state) in the
	// background; PipelineState.SetTasks fires OnChange, which is already
	// wired to queueMain above, so this is safe to call directly from this
	// goroutine (same contract StreamPump's callers rely on for
	// Transcript). nil fetchPipeline (no live connection yet, see this
	// file's doc comment) leaves the panel starting empty.
	if fetchPipeline != nil {
		go func() {
			tasks, err := fetchPipeline(context.Background())
			if err != nil {
				if notify != nil {
					queueMain(func() { notify("error fetching pipeline state: " + err.Error()) })
				}
				return
			}
			pp.state.SetTasks(tasks, pp.state.GeneratedAt())
		}()
	}

	return pp
}

// Control returns the panel's content as a generic uiControl, for
// rightpanel.go's "Pipeline" section.
func (pp *pipelinePanel) Control() *C.uiControl {
	return (*C.uiControl)(unsafe.Pointer(pp.box))
}

// WatchEvents pumps a live GET /pipeline/events stream (SSE) into pp's
// state in a background goroutine, covering the live half of "live task
// status display (GET /pipeline/state + SSE /pipeline/events)." The
// caller owns ctx's lifetime; cancelling it stops the pump (see
// appui.PipelinePump.Run).
func (pp *pipelinePanel) WatchEvents(ctx context.Context, stream *client.EventStream) {
	pump := &appui.PipelinePump{State: pp.state}
	go pump.Run(ctx, stream)
}

// refresh rebuilds the task list and updates the wave/report labels to
// match pp.state. Safe to call from state.OnChange (dispatched through
// queueMain, since a pipeline event can arrive from the PipelinePump's own
// goroutine, not just from a button click already on the UI thread).
func (pp *pipelinePanel) refresh() {
	tasks := pp.state.Tasks()

	for i := len(pp.taskLabels) - 1; i >= 0; i-- {
		C.uiBoxDelete(pp.taskListBox, C.int(i))
		C.uiControlDestroy((*C.uiControl)(unsafe.Pointer(pp.taskLabels[i])))
	}
	pp.taskLabels = pp.taskLabels[:0]
	for _, t := range tasks {
		label := C.uiNewLabel(C.CString(formatTaskLine(t)))
		C.uiBoxAppend(pp.taskListBox, (*C.uiControl)(unsafe.Pointer(label)), 0)
		pp.taskLabels = append(pp.taskLabels, label)
	}

	wave, waveState := pp.state.CurrentWave()
	if waveState == "" {
		C.uiLabelSetText(pp.waveLabel, C.CString("No wave in progress"))
	} else {
		C.uiLabelSetText(pp.waveLabel, C.CString(fmt.Sprintf("Wave %d: %s", wave, waveState)))
	}

	if r := pp.state.Report(); r != nil {
		C.uiMultilineEntrySetText(pp.reportEntry, C.CString(formatRunReport(*r)))
	}
}

// formatTaskLine renders one task's status badge, wave, and latest
// attempt (model, duration, verdict) as a single display line, covering
// "tasks displayed with status badges" and "attempt history: model,
// duration, verdict shown for each attempt."
func formatTaskLine(t phaseflow.Task) string {
	line := fmt.Sprintf("[%s] %s (wave %d)", t.Status, t.ID, t.Wave)
	if n := len(t.Attempts); n > 0 {
		a := t.Attempts[n-1]
		line += fmt.Sprintf(" -- last attempt: %s, %s, %s", a.Model, a.Duration, a.Verdict)
	}
	return line
}

// formatRunReport renders a phaseflow.RunReport as plain text for the
// read-only report box, covering "run report: final report displayed when
// pipeline completes."
func formatRunReport(r phaseflow.RunReport) string {
	out := fmt.Sprintf("Run report (generated %s)\n\n", r.GeneratedAt.Format("2006-01-02 15:04:05"))
	for _, m := range r.Models {
		out += fmt.Sprintf("%s: %d/%d passed\n", m.Model, m.Passes, m.Attempts)
	}
	if len(r.RevisedTasks) > 0 {
		out += "\nRevised tasks:\n"
		for _, rt := range r.RevisedTasks {
			out += fmt.Sprintf("%s (%d rounds)\n", rt.ID, rt.Rounds)
		}
	}
	return out
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
	if pp.startBreakdown == nil || pp.briefContent == "" {
		return
	}

	prompt := appui.BreakdownSeedPrompt(pp.briefName, pp.briefContent)
	go func() {
		_, err := pp.startBreakdown(context.Background(), prompt)
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
