package ui

import (
	"encoding/json"
	"io"
	"sync"
)

// PanelState is the right panel's collapsed/expanded state (.planning/
// tasks/04-03.json). libui-ng exposes no splitter widget and no animation
// API (uiMenuItem has no key-equivalent/accelerator API either -- see
// input.go's ShouldSend doc comment for that same finding against this
// exact ui.h), so the panel is toggle-only at a fixed width rather than
// drag-resizable or animated; this type holds only what that toggle needs,
// same split as Transcript/ApprovalTracker: plain Go here, cgo widget
// assembly (rightpanel.go) elsewhere.
type PanelState struct {
	mu        sync.Mutex
	Collapsed bool
	onChange  func()
}

// NewPanelState returns a panel state defaulting to expanded (visible),
// per 04-03's "Right panel is visible by default."
func NewPanelState() *PanelState {
	return &PanelState{}
}

// OnChange registers f to be called after every Toggle. Same non-blocking,
// non-reentrant contract as Transcript.OnChange.
func (p *PanelState) OnChange(f func()) {
	p.mu.Lock()
	p.onChange = f
	p.mu.Unlock()
}

// Toggle flips Collapsed and notifies OnChange.
func (p *PanelState) Toggle() {
	p.mu.Lock()
	p.Collapsed = !p.Collapsed
	f := p.onChange
	p.mu.Unlock()
	if f != nil {
		f()
	}
}

// panelStateJSON is PanelState's on-disk shape.
type panelStateJSON struct {
	Collapsed bool `json:"collapsed"`
}

// Save writes p's state as JSON to w. The caller owns where those bytes
// end up (a file, e.g.) -- this package has no file I/O, matching the
// rest of gophermind-osx/ui.
func (p *PanelState) Save(w io.Writer) error {
	p.mu.Lock()
	data := panelStateJSON{Collapsed: p.Collapsed}
	p.mu.Unlock()
	return json.NewEncoder(w).Encode(data)
}

// LoadPanelState reads a PanelState previously written by Save. An empty
// r (no saved state yet -- first run) returns a plain default rather than
// an error, so callers don't need to special-case "file doesn't exist yet"
// beyond turning a missing file into an empty reader.
func LoadPanelState(r io.Reader) (*PanelState, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return NewPanelState(), nil
	}
	var decoded panelStateJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, err
	}
	return &PanelState{Collapsed: decoded.Collapsed}, nil
}
