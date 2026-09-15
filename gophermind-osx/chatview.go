// This file is gophermind-osx's chat UI widget layer (.planning/tasks/
// 04-01.json): it renders a *ui.Transcript into real libui-ng widgets.
// Everything renderable/testable as plain logic (transcript state,
// syntax highlighting, the SSE-to-model pump, the Cmd+Enter decision) is
// gophermind-osx/ui, which has no cgo dependency and is fully unit-tested
// (see its own doc comment). This file is the thin, cgo-touching
// remainder that turns that model into pixels -- like app.go before it,
// it can only be smoke-tested (built and driven without panicking), never
// visually verified in this environment.
package main

/*
#cgo CFLAGS: -I/opt/homebrew/include
#cgo LDFLAGS: -L/opt/homebrew/lib -lui
#include <ui.h>
#include <stdlib.h>

extern void goChatAreaDraw(void *ah, uiArea *a, uiAreaDrawParams *p);
extern void goChatAreaMouseEvent(void *ah, uiArea *a, uiAreaMouseEvent *e);
extern void goChatAreaMouseCrossed(void *ah, uiArea *a, int left);
extern void goChatAreaDragBroken(void *ah, uiArea *a);
extern int goChatAreaKeyEvent(void *ah, uiArea *a, uiAreaKeyEvent *e);

// chatAreaHandler embeds uiAreaHandler as its first field (libui-ng's
// "subclass by first-field embedding" pattern: a chatAreaHandler* is a
// valid uiAreaHandler* because its address is the same as its first
// field's) plus a handle used to look up the owning Go *chatArea in a
// registry -- cgo cannot safely store a Go pointer inside C-allocated
// memory long-term (the Go GC doesn't scan C memory), so an integer
// handle into a Go-side map is the standard workaround.
typedef struct chatAreaHandler {
	uiAreaHandler base;
	long long handle;
} chatAreaHandler;

static chatAreaHandler *newChatAreaHandler(long long handle) {
	chatAreaHandler *h = (chatAreaHandler *)malloc(sizeof(chatAreaHandler));
	h->base.Draw = (void (*)(uiAreaHandler *, uiArea *, uiAreaDrawParams *))goChatAreaDraw;
	h->base.MouseEvent = (void (*)(uiAreaHandler *, uiArea *, uiAreaMouseEvent *))goChatAreaMouseEvent;
	h->base.MouseCrossed = (void (*)(uiAreaHandler *, uiArea *, int))goChatAreaMouseCrossed;
	h->base.DragBroken = (void (*)(uiAreaHandler *, uiArea *))goChatAreaDragBroken;
	h->base.KeyEvent = (int (*)(uiAreaHandler *, uiArea *, uiAreaKeyEvent *))goChatAreaKeyEvent;
	h->handle = handle;
	return h;
}

static long long chatAreaHandlerHandle(void *ah) {
	return ((chatAreaHandler *)ah)->handle;
}
*/
import "C"

import (
	"fmt"
	"strings"
	"sync"
	"unsafe"

	appui "gophermind/gophermind-osx/ui"
)

// defaultTextColor is near-black, used for unstyled (ui.Color{}) spans --
// see ui.Color's doc comment on the zero value meaning "unset."
var defaultTextColor = appui.Color{R: 0x20, G: 0x20, B: 0x20}

// lineHeight is a fixed approximate line height in points, used to lay
// out the transcript's message stack. A real implementation would measure
// each uiDrawTextLayout's actual extents per message and accumulate exact
// offsets (uiDrawTextLayoutExtents exists for this); this simplification
// (fixed height per wrapped-at-80-cols line) is a known, documented
// tradeoff given this layer can't be visually verified here to tune
// against real rendering.
const lineHeight = 16.0

// chatArea is the Go side of a custom-drawn, scrolling uiArea rendering a
// *appui.Transcript with real syntax-highlighted code blocks (via
// appui.HighlightCode), registered in chatAreaRegistry so the C-side
// draw/key callbacks (which only carry the opaque handle) can find it.
type chatArea struct {
	area       *C.uiArea
	transcript *appui.Transcript
	contentH   float64 // last-computed content height, for ScrollTo/SetSize
}

var (
	chatAreaRegistryMu sync.Mutex
	chatAreaRegistry   = map[C.longlong]*chatArea{}
	nextChatAreaHandle C.longlong
)

// newChatArea creates a scrolling uiArea bound to transcript: transcript's
// OnChange callback triggers a re-render (dispatched via uiQueueMain, so
// it's safe even though the pump that mutates transcript runs on its own
// goroutine -- covers "UI updates via channel").
func newChatArea(transcript *appui.Transcript) *chatArea {
	chatAreaRegistryMu.Lock()
	handle := nextChatAreaHandle
	nextChatAreaHandle++
	chatAreaRegistryMu.Unlock()

	ah := C.newChatAreaHandler(handle)
	area := C.uiNewScrollingArea((*C.uiAreaHandler)(unsafe.Pointer(ah)), 800, 2000)

	ca := &chatArea{area: area, transcript: transcript}
	chatAreaRegistryMu.Lock()
	chatAreaRegistry[handle] = ca
	chatAreaRegistryMu.Unlock()

	transcript.OnChange(func() {
		queueMain(func() {
			ca.recomputeSize()
			C.uiAreaQueueRedrawAll(ca.area)
			ca.scrollToBottom()
		})
	})
	return ca
}

// Control returns the widget as a generic uiControl, for adding to a box.
func (c *chatArea) Control() *C.uiControl {
	return (*C.uiControl)(unsafe.Pointer(c.area))
}

// recomputeSize sets the uiArea's content height to fit every current
// message (covers auto-scroll's "how tall is the content" prerequisite):
// counts wrapped lines at a fixed width, at lineHeight each -- see
// lineHeight's doc comment on why this is approximate rather than
// measured.
func (c *chatArea) recomputeSize() {
	lines := 0
	for _, m := range c.transcript.Messages() {
		lines += messageLineCount(m)
	}
	c.contentH = float64(lines) * lineHeight
	C.uiAreaSetSize(c.area, 800, C.int(c.contentH))
}

func (c *chatArea) scrollToBottom() {
	C.uiAreaScrollTo(c.area, 0, C.double(c.contentH), 800, lineHeight)
}

// messageLineCount estimates how many lineHeight rows m needs: one for
// its role prefix, plus one per wrapped line of its text/args at
// wrapCols, plus a blank spacer line.
func messageLineCount(m appui.Message) int {
	text := m.Text
	if m.Role == appui.RoleToolCall {
		text = m.ToolArgs
	}
	n := 1 // role prefix line
	for _, line := range strings.Split(text, "\n") {
		n += wrappedLineCount(line, 80)
	}
	return n + 1 // spacer
}

func wrappedLineCount(s string, cols int) int {
	if len(s) == 0 {
		return 1
	}
	n := (len(s) + cols - 1) / cols
	if n < 1 {
		n = 1
	}
	return n
}

// messageSpans renders m as colored spans: a role-prefix span, then either
// plain text (prose) or ui.HighlightCode's spans for a tool call's args /
// a detected code block within assistant text.
func messageSpans(m appui.Message) []appui.Span {
	var spans []appui.Span
	prefix, prefixColor := roleLabel(m.Role, m.ToolName)
	spans = append(spans, appui.Span{Text: prefix + "\n", Color: prefixColor, Bold: true})

	switch m.Role {
	case appui.RoleToolCall:
		spans = append(spans, appui.HighlightCode(m.ToolArgs, "json")...)
	case appui.RoleAssistant:
		for _, block := range appui.ExtractBlocks(m.Text) {
			if block.IsCode {
				spans = append(spans, appui.HighlightCode(block.Text, block.Lang)...)
			} else {
				spans = append(spans, appui.Span{Text: block.Text})
			}
		}
	default:
		spans = append(spans, appui.Span{Text: m.Text})
	}
	spans = append(spans, appui.Span{Text: "\n\n"})
	return spans
}

func roleLabel(role appui.Role, toolName string) (string, appui.Color) {
	switch role {
	case appui.RoleUser:
		return "You", appui.Color{R: 0x30, G: 0x60, B: 0xC0}
	case appui.RoleAssistant:
		return "Assistant", appui.Color{R: 0x20, G: 0x90, B: 0x60}
	case appui.RoleSystem:
		return "System", appui.Color{R: 0xC0, G: 0x40, B: 0x40}
	case appui.RoleToolCall:
		return fmt.Sprintf("Tool call: %s", toolName), appui.Color{R: 0x90, G: 0x60, B: 0x20}
	case appui.RoleToolResult:
		return fmt.Sprintf("Tool result: %s", toolName), appui.Color{R: 0x90, G: 0x60, B: 0x20}
	default:
		return string(role), defaultTextColor
	}
}

// freeAttributedString exists so callers outside this file (chatview_test.go)
// can free what buildAttributedString returns without themselves needing
// import "C" -- which Go's cgo does not support in _test.go files at all.
func freeAttributedString(as *C.uiAttributedString) {
	C.uiFreeAttributedString(as)
}

// buildAttributedString renders every message in the transcript into one
// uiAttributedString with per-span color attributes -- the actual
// mechanism behind "code blocks displayed with syntax highlighting":
// uiMultilineEntry (used elsewhere in this app, e.g. the input field) is
// plain-text only, so real per-token coloring requires this uiArea +
// uiAttributedString + uiDrawText path instead. Caller must free the
// result via freeAttributedString (or C.uiFreeAttributedString directly,
// from a non-test file).
func buildAttributedString(transcript *appui.Transcript) *C.uiAttributedString {
	as := C.uiNewAttributedString(C.CString(""))
	var offset C.size_t
	for _, m := range transcript.Messages() {
		for _, span := range messageSpans(m) {
			cText := C.CString(span.Text)
			C.uiAttributedStringAppendUnattributed(as, cText)
			C.free(unsafe.Pointer(cText))

			start := offset
			offset += C.size_t(len(span.Text))
			color := span.Color
			if color == (appui.Color{}) {
				color = defaultTextColor
			}
			colorAttr := C.uiNewColorAttribute(
				C.double(float64(color.R)/255), C.double(float64(color.G)/255), C.double(float64(color.B)/255), 1.0)
			C.uiAttributedStringSetAttribute(as, colorAttr, start, offset)
			if span.Bold {
				weightAttr := C.uiNewWeightAttribute(C.uiTextWeightBold)
				C.uiAttributedStringSetAttribute(as, weightAttr, start, offset)
			}
		}
	}
	return as
}

//export goChatAreaDraw
func goChatAreaDraw(ah unsafe.Pointer, a *C.uiArea, p *C.uiAreaDrawParams) {
	handle := C.chatAreaHandlerHandle(ah)
	chatAreaRegistryMu.Lock()
	ca, ok := chatAreaRegistry[handle]
	chatAreaRegistryMu.Unlock()
	if !ok {
		return
	}

	as := buildAttributedString(ca.transcript)
	defer C.uiFreeAttributedString(as)

	params := C.uiDrawTextLayoutParams{
		String:      as,
		DefaultFont: nil,
		Width:       C.double(800),
		Align:       C.uiDrawTextAlignLeft,
	}
	layout := C.uiDrawNewTextLayout(&params)
	defer C.uiDrawFreeTextLayout(layout)
	C.uiDrawText(p.Context, layout, 8, 8)
}

//export goChatAreaMouseEvent
func goChatAreaMouseEvent(ah unsafe.Pointer, a *C.uiArea, e *C.uiAreaMouseEvent) {}

//export goChatAreaMouseCrossed
func goChatAreaMouseCrossed(ah unsafe.Pointer, a *C.uiArea, left C.int) {}

//export goChatAreaDragBroken
func goChatAreaDragBroken(ah unsafe.Pointer, a *C.uiArea) {}

//export goChatAreaKeyEvent
func goChatAreaKeyEvent(ah unsafe.Pointer, a *C.uiArea, e *C.uiAreaKeyEvent) C.int {
	// The transcript display is read-only; it doesn't consume key events.
	// (The input field is a separate uiMultilineEntry -- see
	// newChatInput -- which handles its own text entry natively; this
	// area never has focus for typing.)
	return 0
}
