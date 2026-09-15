package ui

import (
	"sync"

	"gophermind/gophermind-lib/session"
)

// SessionModes is the fixed set of session modes gophermind-server's
// GET /modes hardcodes (gophermind-lib/serve/session_serve.go's
// modesResponse) -- duplicated here as a plain constant rather than a live
// client call: the server never returns anything else, so a client method
// would add a network round trip and an error path for a value that never
// changes. If the server ever grows a sixth mode, this list must be kept in
// sync with modesResponse by hand.
var SessionModes = []string{"coding", "conversational", "reviewer", "architect", "tester"}

// SessionEntry is one row in the session list (.planning/tasks/04-05.json):
// a saved session's Info plus which configured backend/connection it came
// from. gophermind-lib/session.Info has no backend field of its own (the
// server has no notion of "backend name" -- GET /session just lists
// whatever's on that one server); Backend is the name of the
// gophermind-osx/connection.Connection that fetched this Info, filled in by
// the widget layer, which is the only layer that knows about more than one
// backend at once.
type SessionEntry struct {
	Info    session.Info
	Backend string
}

// SessionConfig is a resumed session's model/mode/root, mirroring
// gophermind-osx/client.SessionConfigInfo's fields without importing that
// package -- same split as ModelPickerState: this package stays cgo-free
// and client-free, the widget layer injects results via SetConfig.
type SessionConfig struct {
	Model string
	Mode  string
	Root  string
}

// SessionListState is the session list's plain-Go state: the list of known
// sessions, which one is selected/resumed, its config once fetched, and
// the staged root/mode for creating a new session. Same split as
// PanelState/ApprovalTracker/ModelPickerState: plain Go here, no cgo, no
// direct dependency on gophermind-osx/client.
type SessionListState struct {
	mu         sync.Mutex
	entries    []SessionEntry
	selectedID string
	config     SessionConfig
	newRoot    string
	newMode    string
	onChange   func()
}

// NewSessionListState returns an empty session list state, with newMode
// defaulting to SessionModes[0] ("coding") for a new session's mode
// selector.
func NewSessionListState() *SessionListState {
	return &SessionListState{newMode: SessionModes[0]}
}

// OnChange registers f to be called after every mutation. Same
// non-blocking, non-reentrant contract as PanelState.OnChange.
func (s *SessionListState) OnChange(f func()) {
	s.mu.Lock()
	s.onChange = f
	s.mu.Unlock()
}

func (s *SessionListState) notify() {
	s.mu.Lock()
	f := s.onChange
	s.mu.Unlock()
	if f != nil {
		f()
	}
}

// SetEntries replaces the session list, e.g. after a fresh
// client.ListSessions call (merged with the backend name that fetched it).
func (s *SessionListState) SetEntries(entries []SessionEntry) {
	s.mu.Lock()
	s.entries = entries
	s.mu.Unlock()
	s.notify()
}

// Entries returns a snapshot copy of the current session list.
func (s *SessionListState) Entries() []SessionEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SessionEntry, len(s.entries))
	copy(out, s.entries)
	return out
}

// Select records id as the resumed/selected session, clearing any
// previously-fetched Config -- covers "resume: click session to attach"
// at the model level; the widget layer fetches messages/config
// separately and calls SetConfig once they arrive.
func (s *SessionListState) Select(id string) {
	s.mu.Lock()
	s.selectedID = id
	s.config = SessionConfig{}
	s.mu.Unlock()
	s.notify()
}

// SelectedID returns the currently selected/resumed session id, "" if none.
func (s *SessionListState) SelectedID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.selectedID
}

// SetConfig records the resumed session's model/mode/root, e.g. from
// client.SessionConfig -- covers "session config: model, mode, root
// displayed on resume."
func (s *SessionListState) SetConfig(cfg SessionConfig) {
	s.mu.Lock()
	s.config = cfg
	s.mu.Unlock()
	s.notify()
}

// Config returns the selected session's last-fetched config.
func (s *SessionListState) Config() SessionConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config
}

// RenameLocal updates entry id's display Name after a successful
// client.RenameSession call -- covers "rename: ... list updates" without
// requiring a full re-fetch of the list.
func (s *SessionListState) RenameLocal(id, name string) {
	s.mu.Lock()
	for i := range s.entries {
		if s.entries[i].Info.ID == id {
			s.entries[i].Info.Name = name
			break
		}
	}
	s.mu.Unlock()
	s.notify()
}

// RemoveLocal drops entry id from the list after a successful
// client.DeleteSession call -- covers "delete: ... list updates."
func (s *SessionListState) RemoveLocal(id string) {
	s.mu.Lock()
	for i, e := range s.entries {
		if e.Info.ID == id {
			s.entries = append(s.entries[:i], s.entries[i+1:]...)
			break
		}
	}
	if s.selectedID == id {
		s.selectedID = ""
		s.config = SessionConfig{}
	}
	s.mu.Unlock()
	s.notify()
}

// NewRoot returns the staged root path for creating a new session, set via
// SetNewRoot (the widget layer's native folder-picker result).
func (s *SessionListState) NewRoot() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.newRoot
}

// SetNewRoot stages root for the next "create new" call.
func (s *SessionListState) SetNewRoot(root string) {
	s.mu.Lock()
	s.newRoot = root
	s.mu.Unlock()
	s.notify()
}

// NewMode returns the staged mode for creating a new session.
func (s *SessionListState) NewMode() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.newMode
}

// SetNewMode stages mode for the next "create new" call. Does not validate
// against SessionModes: the widget layer's mode selector only ever offers
// SessionModes' values, so an invalid mode here would be this package's own
// bug, not bad input to guard against.
func (s *SessionListState) SetNewMode(mode string) {
	s.mu.Lock()
	s.newMode = mode
	s.mu.Unlock()
	s.notify()
}
