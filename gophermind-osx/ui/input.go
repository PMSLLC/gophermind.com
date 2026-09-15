package ui

// Modifier is a platform-agnostic keyboard modifier flag. Duplicated here
// (rather than referencing libui-ng's uiModifiers directly) so this
// decision logic is plain Go, testable without cgo/libui-ng initialized.
type Modifier int

const (
	ModifierCommand Modifier = 1 << iota
	ModifierAlt
	ModifierControl
	ModifierShift
)

// Has reports whether flag is set in m.
func (m Modifier) Has(flag Modifier) bool { return m&flag != 0 }

// KeyEvent is the minimal key-press information ShouldSend/
// ShouldInsertNewline need.
type KeyEvent struct {
	Key       rune // '\r' or '\n' for Enter/Return
	Modifiers Modifier
}

// ShouldSend reports whether ev is the send shortcut (Cmd+Enter), covering
// "Multi-line input: Cmd+Enter sends" as a decision, independent of how a
// caller actually obtains key events.
//
// This is not wired to a real key-event source: libui-ng's uiMultilineEntry
// (the widget chatview.go's input field actually uses, for its native
// cursor/selection/IME/undo support) exposes no key-event hook at all --
// only uiMultilineEntryOnChanged (text-changed, after the fact) -- and
// uiMenuItem exposes no key-equivalent/accelerator API either. Both were
// checked against the installed ui.h before writing this, not assumed.
// The only toolkit path to real per-keystroke modifier info is a
// uiArea's KeyEvent handler, which would mean reimplementing text editing
// (cursor, selection, IME) from scratch to get one shortcut -- out of
// scope here. chatview.go instead drives sending via a visible Send
// button. This function stays as tested, ready-to-wire logic for if the
// input is ever rebuilt on uiArea.
func ShouldSend(ev KeyEvent) bool {
	return isReturn(ev.Key) && ev.Modifiers.Has(ModifierCommand)
}

// ShouldInsertNewline is ShouldSend's complement: a plain Enter (no Cmd)
// should behave like an ordinary multi-line text field and insert a
// newline -- which uiMultilineEntry already does natively without any
// help from this package; kept for symmetry and for the same future
// uiArea-based input ShouldSend documents.
func ShouldInsertNewline(ev KeyEvent) bool {
	return isReturn(ev.Key) && !ev.Modifiers.Has(ModifierCommand)
}

func isReturn(k rune) bool { return k == '\r' || k == '\n' }
