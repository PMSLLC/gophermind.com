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
#include <ui.h>
#include <stdlib.h>
#import <AppKit/AppKit.h>

// appIsDarkMode reports whether the app is currently rendering in dark mode.
// The transcript is drawn into a uiArea, which means libui gives us no
// system label colour to inherit -- we pick the ink ourselves, so we have to
// ask what it is being drawn on. Read at draw time rather than cached: the
// user can flip appearance while the app is running, and every redraw then
// picks up the new answer on its own.
static int appIsDarkMode(void) {
	if (@available(macOS 10.14, *)) {
		NSAppearance *a = NSApp.effectiveAppearance;
		if (a == nil) return 0;
		NSAppearanceName n = [a bestMatchFromAppearancesWithNames:@[
			NSAppearanceNameAqua, NSAppearanceNameDarkAqua]];
		return [n isEqualToString:NSAppearanceNameDarkAqua] ? 1 : 0;
	}
	return 0;
}

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

// uiDrawContextValid checks whether the draw context's internal
// CGContextRef is non-nil (libui-ng's uiDrawContext wraps a CGContextRef;
// on macOS it's the first field of the struct). This guards against
// uiDrawNewTextLayout crashing on a null context.
static int uiDrawContextValid(uiDrawContext *ctx) {
	if (ctx == NULL) return 0;
	// uiDrawContext on macOS: { CGContextRef context; ... }
	// We can't access the field directly without the full struct definition,
	// so we check the first 8 bytes (pointer) for non-zero.
	unsigned char *p = (unsigned char *)ctx;
	for (int i = 0; i < 8; i++) {
		if (p[i] != 0) return 1;
	}
	return 0;
}
*/
import "C"

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
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
	approvals  *appui.ApprovalTracker // consulted when rendering RoleApproval messages
	contentH   float64                // last-computed content height, for ScrollTo/SetSize
}

var (
	chatAreaRegistryMu sync.Mutex
	chatAreaRegistry   = map[C.longlong]*chatArea{}
	nextChatAreaHandle C.longlong
)

// newChatArea creates a scrolling uiArea bound to transcript: transcript's
// OnChange callback triggers a re-render (dispatched via uiQueueMain, so
// it's safe even though the pump that mutates transcript runs on its own
// goroutine -- covers "UI updates via channel"). approvals is the
// ApprovalTracker consulted when rendering RoleApproval messages (nil is
// fine: approval cards just show "unknown" status).
func newChatArea(transcript *appui.Transcript, approvals *appui.ApprovalTracker) *chatArea {
	chatAreaRegistryMu.Lock()
	handle := nextChatAreaHandle
	nextChatAreaHandle++
	chatAreaRegistryMu.Unlock()

	ah := C.newChatAreaHandler(handle)
	area := C.uiNewScrollingArea((*C.uiAreaHandler)(unsafe.Pointer(ah)), 800, 2000)

	ca := &chatArea{area: area, transcript: transcript, approvals: approvals}
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
// a detected code block within assistant text. RoleApproval messages are
// rendered as an inline approval card (tool name, pretty-printed args,
// current status) looked up from the chatArea's ApprovalTracker.
func (c *chatArea) messageSpans(m appui.Message) []appui.Span {
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
	case appui.RoleApproval:
		spans = append(spans, c.approvalCardSpans(m)...)
	default:
		spans = append(spans, appui.Span{Text: m.Text})
	}
	spans = append(spans, appui.Span{Text: "\n\n"})
	return spans
}

// approvalCardSpans renders an inline approval card for m (a RoleApproval
// message): the tool name, pretty-printed args, and the approval's current
// status (pending/approved/denied/timed-out). The live status is looked up
// from the chatArea's ApprovalTracker by m.ApprovalID, so the card always
// reflects the approval's current state even though the transcript itself
// is append-only.
func (c *chatArea) approvalCardSpans(m appui.Message) []appui.Span {
	var spans []appui.Span

	// Look up the live approval for its tool name, args, and status.
	var tool, args, status string
	if c.approvals != nil {
		for _, a := range c.approvals.All() {
			if a.ID == m.ApprovalID {
				tool = a.Tool
				args = a.Args
				status = a.Status.String()
				break
			}
		}
	}
	if tool == "" {
		tool = "(unknown tool)"
	}
	if status == "" {
		status = "unknown"
	}

	// Card header: "Approval: <tool>" in a distinct color.
	spans = append(spans, appui.Span{Text: fmt.Sprintf("Approval: %s\n", tool), Color: appui.Color{R: 0x80, G: 0x40, B: 0xC0}, Bold: true})

	// Pretty-printed args (indented to look like a card body).
	if args != "" {
		for _, line := range strings.Split(args, "\n") {
			spans = append(spans, appui.Span{Text: "  " + line + "\n"})
		}
	}

	// Status line, color-coded: green for approved, red for denied/timed-out,
	// amber for pending.
	var statusColor appui.Color
	switch {
	case status == "approved":
		statusColor = appui.Color{R: 0x20, G: 0x90, B: 0x60}
	case status == "denied" || status == "denied (timed out)":
		statusColor = appui.Color{R: 0xC0, G: 0x40, B: 0x40}
	default: // pending
		statusColor = appui.Color{R: 0xC0, G: 0x90, B: 0x20}
	}
	spans = append(spans, appui.Span{Text: fmt.Sprintf("  Status: %s\n", status), Color: statusColor, Bold: true})

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
func (c *chatArea) buildAttributedString() *C.uiAttributedString {
	as := C.uiNewAttributedString(C.CString(""))
	var offset C.size_t
	for _, m := range c.transcript.Messages() {
		for _, span := range c.messageSpans(m) {
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
	if p == nil || p.Context == nil {
		return
	}
	handle := C.chatAreaHandlerHandle(ah)
	chatAreaRegistryMu.Lock()
	ca, ok := chatAreaRegistry[handle]
	chatAreaRegistryMu.Unlock()
	if !ok {
		return
	}

	// Skip drawing if there are no messages yet.
	if len(ca.transcript.Messages()) == 0 {
		return
	}

	// Build a plain attributed string (no color/weight attributes) for
	// simple rendering. The full attributed string with per-span colors
	// crashes uiDrawNewTextLayout in some libui-ng builds.
	var sb strings.Builder
	for _, m := range ca.transcript.Messages() {
		sb.WriteString(m.Text)
		sb.WriteString("\n\n")
	}
	text := sb.String()
	if strings.TrimSpace(text) == "" {
		return
	}
	cText := C.CString(text)
	defer C.free(unsafe.Pointer(cText))
	as := C.uiNewAttributedString(cText)
	defer C.uiFreeAttributedString(as)

	// Add a color attribute for the entire string. uiDrawText uses the
	// attributed string's color attributes; without one it defaults to
	// black, which is invisible on dark mode's grey background.
	//
	// This used to be a fixed 0.88 grey, chosen to survive both appearances.
	// It does not: 0.88 on white is the pale, barely-legible transcript this
	// replaces. There is no single grey with decent contrast against both a
	// white and a near-black background -- the midpoint that clears 4.5:1 on
	// one fails it on the other -- so pick per appearance instead of
	// compromising.
	ink := C.double(0.11) // near-black on light, ~15:1 against white
	if C.appIsDarkMode() != 0 {
		ink = C.double(0.92) // near-white on dark, ~14:1 against 0x1E grey
	}
	textColor := C.uiNewColorAttribute(ink, ink, ink, 1.0)
	C.uiAttributedStringSetAttribute(as, textColor, 0, C.size_t(len(text)))

	width := C.double(p.AreaWidth)
	if width <= 0 {
		width = C.double(800)
	}
	fontPtr := (*C.uiFontDescriptor)(C.malloc(C.size_t(unsafe.Sizeof(C.uiFontDescriptor{}))))
	C.uiLoadControlFont(fontPtr)
	// uiLoadControlFont returns the system control font, sized for button
	// labels (13pt). The transcript is a page of prose, not a label, so give
	// it the size AppKit uses for document text. Everything else in the
	// window keeps the control font, which is what makes the chrome recede
	// and the conversation read as the content.
	if fontPtr.Size < 15 {
		fontPtr.Size = 15
	}
	defer func() {
		C.uiFreeFontDescriptor(fontPtr)
		C.free(unsafe.Pointer(fontPtr))
	}()
	params := C.uiDrawTextLayoutParams{
		String:      as,
		DefaultFont: fontPtr,
		Width:       width,
		Align:       C.uiDrawTextAlignLeft,
	}
	layout := C.uiDrawNewTextLayout(&params)
	if layout == nil {
		return
	}
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
	// The transcript display is read-only; it doesn't consume key events
	// except for the Y/N approval shortcut (04-02: "Y/N keyboard shortcut:
	// works when not in editable field"). The input field is a separate
	// uiMultilineEntry which handles its own text entry natively, so this
	// area only gets key events when it has focus (user clicked on the
	// transcript), meaning the input field is not focused.
	if e.Up != 0 {
		return 0 // key-up: ignore
	}
	key := byte(e.Key)
	switch key {
	case 'y', 'Y':
		if ca := chatAreaFromHandler(ah); ca != nil && ca.approvals != nil {
			ca.approvals.ApproveLatest()
			return 1 // consumed
		}
	case 'n', 'N':
		if ca := chatAreaFromHandler(ah); ca != nil && ca.approvals != nil {
			ca.approvals.DenyLatest()
			return 1 // consumed
		}
	}
	return 0
}

// chatAreaFromHandler looks up the *chatArea for a C-side handler pointer.
func chatAreaFromHandler(ah unsafe.Pointer) *chatArea {
	handle := C.chatAreaHandlerHandle(ah)
	chatAreaRegistryMu.Lock()
	defer chatAreaRegistryMu.Unlock()
	return chatAreaRegistry[handle]
}

// approvalBar is a persistent label at the bottom of the chat column that
// summarizes pending approvals (tool name + time remaining). It updates
// itself via ApprovalTracker.OnChange, so it always reflects the tracker's
// current state without the caller needing to poll.
type approvalBar struct {
	label   *C.uiLabel
	tracker *appui.ApprovalTracker
}

// newApprovalBar creates the bar bound to tracker: tracker's OnChange
// callback updates the label text (dispatched via uiQueueMain, same
// pattern as newChatArea's transcript.OnChange). A 1-second timer also
// ticks the countdown display and calls CheckTimeouts for auto-deny at
// 5:00 (04-02: "5-min timeout: warning at 4:30, auto-deny at 5:00").
func newApprovalBar(tracker *appui.ApprovalTracker) *approvalBar {
	label := C.uiNewLabel(C.CString("No pending approvals"))
	bar := &approvalBar{label: label, tracker: tracker}

	tracker.OnChange(func() {
		queueMain(func() {
			bar.update()
		})
	})

	// 1-second timer: tick the countdown and sweep expired approvals.
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for range ticker.C {
			tracker.CheckTimeouts(context.Background())
			queueMain(func() {
				bar.update()
			})
		}
	}()
	return bar
}

// Control returns the widget as a generic uiControl, for adding to a box.
func (b *approvalBar) Control() *C.uiControl {
	return (*C.uiControl)(unsafe.Pointer(b.label))
}

// update refreshes the label text from the tracker's current pending
// approvals.
func (b *approvalBar) update() {
	cText := C.CString(b.pendingSummary())
	C.uiLabelSetText(b.label, cText)
	C.free(unsafe.Pointer(cText))
}

// pendingSummary formats the tracker's pending approvals as a short
// one-line summary. A "!" warning marker appears when an approval has
// ≤ 30 seconds remaining (04-02: "warning at 4:30").
func (b *approvalBar) pendingSummary() string {
	pending := b.tracker.Pending()
	if len(pending) == 0 {
		return "No pending approvals"
	}
	parts := make([]string, 0, len(pending))
	now := time.Now()
	for _, a := range pending {
		remaining := a.TimeRemaining(now)
		s := fmt.Sprintf("%s (%s)", a.Tool, remaining)
		if remaining <= 30*time.Second {
			s += " !"
		}
		parts = append(parts, s)
	}
	return "Pending: " + strings.Join(parts, ", ")
}
