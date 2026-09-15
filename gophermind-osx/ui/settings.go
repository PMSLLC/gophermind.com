package ui

import (
	"encoding/json"
	"io"
	"sync"
)

// BackendProfile is one configured backend's non-secret connection
// metadata (.planning/tasks/04-07.json's "remote backends: add/remove
// (server URL, gocloak realm)"). Deliberately excludes anything secret:
// a WireGuard client private key (connection.RemoteConfig.ClientPrivateKey)
// must never round-trip through a plain JSON file the way this type does
// (see gophermind-osx/settingspanel.go's doc comment for where that
// belongs instead -- gophermind-osx/auth's existing Keychain-backed
// TokenStore, keyed separately per backend name).
type BackendProfile struct {
	Name         string
	Mode         string // "local" or "remote"
	ServerURL    string
	GocloakRealm string
	Token        string // bearer token for the server (empty = no auth)
}

// BackendListState is the settings panel's plain-Go state for the
// configured backend list: the profiles themselves, each one's live
// connection status (as connection.Status.String() reports it), and which
// one is currently active. Same split as PanelState/ModelPickerState:
// mutex-protected, cgo-free, OnChange-driven; the actual
// connection.Manager.Connect/Disconnect calls live in the widget layer.
type BackendListState struct {
	mu       sync.Mutex
	profiles []BackendProfile
	status   map[string]string
	active   string
	onChange func()
}

// NewBackendListState returns an empty backend list.
func NewBackendListState() *BackendListState {
	return &BackendListState{status: make(map[string]string)}
}

// OnChange registers f to be called after every mutation. Same
// non-blocking, non-reentrant contract as PanelState.OnChange.
func (b *BackendListState) OnChange(f func()) {
	b.mu.Lock()
	b.onChange = f
	b.mu.Unlock()
}

func (b *BackendListState) notify() {
	if b.onChange != nil {
		b.onChange()
	}
}

// Add appends p, or replaces the existing profile with the same Name in
// place (an edit, not a duplicate entry).
func (b *BackendListState) Add(p BackendProfile) {
	b.mu.Lock()
	for i, existing := range b.profiles {
		if existing.Name == p.Name {
			b.profiles[i] = p
			b.mu.Unlock()
			b.notify()
			return
		}
	}
	b.profiles = append(b.profiles, p)
	b.mu.Unlock()
	b.notify()
}

// Remove drops the profile named name, if present.
func (b *BackendListState) Remove(name string) {
	b.mu.Lock()
	for i, p := range b.profiles {
		if p.Name == name {
			b.profiles = append(b.profiles[:i], b.profiles[i+1:]...)
			break
		}
	}
	delete(b.status, name)
	b.mu.Unlock()
	b.notify()
}

// Profiles returns a snapshot copy of the current profile list.
func (b *BackendListState) Profiles() []BackendProfile {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]BackendProfile, len(b.profiles))
	copy(out, b.profiles)
	return out
}

// SetStatus records name's current connection status (e.g.
// connection.Status.String()).
func (b *BackendListState) SetStatus(name, status string) {
	b.mu.Lock()
	b.status[name] = status
	b.mu.Unlock()
	b.notify()
}

// Status returns name's last recorded status, or "disconnected" if never
// set -- a configured-but-never-connected backend is disconnected, not an
// unknown state.
func (b *BackendListState) Status(name string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s, ok := b.status[name]; ok {
		return s
	}
	return "disconnected"
}

// SetActive records which backend the app is currently using, covering
// "Endpoint: local/remote mode switchable" at the state level (the widget
// layer's radio/combobox drives this).
func (b *BackendListState) SetActive(name string) {
	b.mu.Lock()
	b.active = name
	b.mu.Unlock()
	b.notify()
}

// Active returns the currently active backend's name, or "" if none.
func (b *BackendListState) Active() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.active
}

// CacheHistorySettings is the panel's "cache/history" section
// (.planning/tasks/04-07.json). No existing spec or code in this
// repository defines what "cache/history options" means beyond the name,
// so this is a deliberately minimal, honest interpretation: how many
// transcript messages to keep, and whether chat history persists to disk
// at all. Persisted the same way as PanelState: a plain JSON file under
// the user's config dir (gophermind-osx/settingspanel.go), local-only,
// nothing secret.
type CacheHistorySettings struct {
	MaxHistoryMessages int
	PersistHistory     bool
}

// DefaultCacheHistorySettings returns the settings a fresh install starts
// with: a generous but bounded history, persisted across restarts.
func DefaultCacheHistorySettings() CacheHistorySettings {
	return CacheHistorySettings{MaxHistoryMessages: 1000, PersistHistory: true}
}

type cacheHistoryJSON struct {
	MaxHistoryMessages int  `json:"max_history_messages"`
	PersistHistory     bool `json:"persist_history"`
}

// Save writes s as JSON to w.
func (s CacheHistorySettings) Save(w io.Writer) error {
	return json.NewEncoder(w).Encode(cacheHistoryJSON{s.MaxHistoryMessages, s.PersistHistory})
}

// LoadCacheHistorySettings reads settings previously written by Save. An
// empty r (no saved file yet) returns DefaultCacheHistorySettings, same
// first-run contract as LoadPanelState.
func LoadCacheHistorySettings(r io.Reader) (CacheHistorySettings, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return CacheHistorySettings{}, err
	}
	if len(data) == 0 {
		return DefaultCacheHistorySettings(), nil
	}
	var decoded cacheHistoryJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		return CacheHistorySettings{}, err
	}
	return CacheHistorySettings{decoded.MaxHistoryMessages, decoded.PersistHistory}, nil
}
