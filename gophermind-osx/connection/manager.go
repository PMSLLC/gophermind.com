// Package connection manages gophermind-osx's connections to one or more
// gophermind-server backends (.planning/tasks/03-03.json): local mode
// spawns a gophermind-server subprocess on localhost with a freshly
// generated token; remote mode establishes a userspace WireGuard tunnel
// (gophermind-lib/wireguard) and routes every request through it. Each
// Connection health-checks itself and reconnects automatically on failure;
// Manager holds any number of named Connections simultaneously.
package connection

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"
)

// Status is a Connection's current lifecycle state.
type Status int

const (
	StatusDisconnected Status = iota
	StatusConnecting
	StatusConnected
	StatusReconnecting
	StatusDisconnecting
)

func (s Status) String() string {
	switch s {
	case StatusDisconnected:
		return "disconnected"
	case StatusConnecting:
		return "connecting"
	case StatusConnected:
		return "connected"
	case StatusReconnecting:
		return "reconnecting"
	case StatusDisconnecting:
		return "disconnecting"
	default:
		return "unknown"
	}
}

// Mode selects how a Connection reaches its backend.
type Mode int

const (
	// ModeLocal spawns gophermind-server as a child process on localhost
	// with a freshly generated random bearer token, and talks to it over
	// a plain (untunneled) loopback connection -- same machine, same user,
	// so a WireGuard tunnel would add nothing.
	ModeLocal Mode = iota
	// ModeRemote establishes a userspace WireGuard tunnel to an
	// already-running, already-registered remote gophermind-server and
	// routes every request through it.
	ModeRemote
	// ModeDirect connects to a remote gophermind-server over plain HTTP
	// (no WireGuard tunnel, no subprocess). Used when the user provides
	// a reachable host:port directly.
	ModeDirect
)

// LocalConfig configures a ModeLocal Connection.
type LocalConfig struct {
	// ServerBinaryPath is the path to a built gophermind-server binary.
	ServerBinaryPath string
	// Port is the localhost port to bind. 0 picks a free port
	// automatically (the common case; a fixed port is mainly useful for
	// tests that want a predictable address).
	Port int
	// Root is passed to gophermind-server as --root. Empty uses
	// gophermind-server's own default (cwd).
	Root string
	// StartupTimeout bounds how long Connect waits for the spawned
	// server's /healthz to succeed. <= 0 uses DefaultStartupTimeout.
	StartupTimeout time.Duration
	// ClientTimeout overrides the per-request timeout (including the full
	// lifetime of a streamed response body -- see net/http.Client.Timeout's
	// own doc comment) on the client.Client this Connection builds. <= 0
	// uses client.DefaultTimeout, which is too short for a long-running
	// session turn (e.g. a phaseflow breakdown) that legitimately takes
	// longer than an ordinary chat reply.
	ClientTimeout time.Duration
}

// RemoteConfig configures a ModeRemote Connection: everything needed to
// dial a WireGuard tunnel to an already-registered peer (see
// gophermind-lib/wireguard.ClientConfig, and gophermind-server's
// POST /wg/register, which is what issues these values to a real client --
// obtaining them is outside this package's scope, matching 03-03's
// description: "establish a WG tunnel per backend," not "register one").
type RemoteConfig struct {
	ServerPublicKey  string
	ServerEndpoint   string // host:port
	ClientPrivateKey []byte
	ClientAddress    netip.Addr
	AllowedIPs       string
	// RemoteAddr is where gophermind-server listens on the far side of the
	// tunnel, e.g. "10.66.0.1:8090" (the WG server's tunnel address, not
	// ServerEndpoint, which is the WG handshake endpoint).
	RemoteAddr string
	// Token is the bearer token the server on the far side of the tunnel
	// expects. Empty means no auth -- every /session route will 401. See
	// DirectConfig.Token, which connectDirect already wires the same way.
	Token string
}

// DirectConfig configures a ModeDirect Connection: a plain HTTP endpoint
// to an already-running gophermind-server.
type DirectConfig struct {
	// BaseURL is the server's HTTP address, e.g. "http://<server-host>:8090".
	BaseURL string
	// Token is the bearer token the server expects. Empty means no auth.
	Token string
}

// BackendConfig configures one Connection. Exactly one of Local/Remote
// should be set, matching Mode.
type BackendConfig struct {
	Name   string
	Mode   Mode
	Local  LocalConfig
	Remote RemoteConfig
	Direct DirectConfig
	// HealthInterval is how often Connect's background loop health-checks
	// the connection once established. <= 0 uses DefaultHealthInterval.
	HealthInterval time.Duration
	// OnStatusChange, if non-nil, is called (from the health-check
	// goroutine, so it must not block) every time Status changes.
	OnStatusChange func(Status)
}

// DefaultStartupTimeout bounds how long local-mode Connect waits for the
// spawned gophermind-server to become healthy.
const DefaultStartupTimeout = 10 * time.Second

// DefaultHealthInterval is how often an established Connection
// health-checks itself. Short enough that a failure is detected and
// reconnection is underway well within the 5s the acceptance criteria
// wants, while not so short it floods the backend with health-check
// traffic.
const DefaultHealthInterval = 1 * time.Second

// maxConsecutiveHealthFailures is how many health-check failures in a row
// trigger a reconnect. 1 (react immediately) rather than a debounced count:
// the 5s reconnection budget doesn't leave room to wait out several
// intervals hoping a failure was a fluke.
const maxConsecutiveHealthFailures = 1

// randomToken returns a 32-byte (64 hex char) random bearer token, the
// same strength gophermind-server's own key generation uses.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// freePort asks the OS for an unused TCP port on localhost by briefly
// binding to port 0 and reading back what it was assigned, then releasing
// it. Racy in principle (another process could grab the same port before
// gophermind-server binds it) but this is the standard Go idiom for it and
// the race window is a few milliseconds.
func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// Manager holds any number of named Connections, so gophermind-osx can be
// connected to several gophermind-server backends at once -- e.g. a local
// dev server plus one or more remote ones, each independently connected,
// health-checked, and reconnected.
type Manager struct {
	mu    sync.Mutex
	conns map[string]*Connection
}

// NewManager returns an empty Manager.
func NewManager() *Manager {
	return &Manager{conns: make(map[string]*Connection)}
}

// Connect adds and connects a new backend named cfg.Name. Returns an error
// if that name is already in use (call Disconnect first to replace it) or
// if the connection attempt itself fails.
func (m *Manager) Connect(ctx context.Context, cfg BackendConfig) error {
	m.mu.Lock()
	if _, exists := m.conns[cfg.Name]; exists {
		m.mu.Unlock()
		return fmt.Errorf("connection %q already exists", cfg.Name)
	}
	conn := New(cfg)
	m.conns[cfg.Name] = conn
	m.mu.Unlock()

	if err := conn.Connect(ctx); err != nil {
		m.mu.Lock()
		delete(m.conns, cfg.Name)
		m.mu.Unlock()
		return err
	}
	return nil
}

// Get returns the named Connection, or (nil, false) if it doesn't exist.
func (m *Manager) Get(name string) (*Connection, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.conns[name]
	return c, ok
}

// Disconnect disconnects and removes the named connection. A no-op (not an
// error) if the name doesn't exist, so callers can call it unconditionally
// during cleanup.
func (m *Manager) Disconnect(name string) {
	m.mu.Lock()
	conn, ok := m.conns[name]
	delete(m.conns, name)
	m.mu.Unlock()
	if ok {
		conn.Disconnect()
	}
}

// Names returns the currently managed connection names, in no particular
// order.
func (m *Manager) Names() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.conns))
	for name := range m.conns {
		out = append(out, name)
	}
	return out
}

// DisconnectAll disconnects and removes every managed connection.
func (m *Manager) DisconnectAll() {
	m.mu.Lock()
	names := make([]string, 0, len(m.conns))
	for name := range m.conns {
		names = append(names, name)
	}
	m.mu.Unlock()
	for _, name := range names {
		m.Disconnect(name)
	}
}
