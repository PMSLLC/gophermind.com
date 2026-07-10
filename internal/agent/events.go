package agent

// EventDelta carries structured token delta information for the TUI to render
// reasoning vs. answer separately. It is emitted alongside the regular "token"
// event when the model supports structured deltas.
type EventDelta struct {
	Role    string // "assistant" or "reasoning"
	Index   int    // position in the choices array
	Content string // the delta content
}

// TokenStreamEvent is an extended event type that carries structured delta
// information alongside the raw token text.
type TokenStreamEvent struct {
	Event
	Delta *EventDelta // nil for legacy compatibility
}
