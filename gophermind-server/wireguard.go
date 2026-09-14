package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"sync"
	"time"

	"gophermind/gophermind-lib/wireguard"
)

// TokenValidator authenticates a POST /wg/register caller and returns a
// stable subject identifier (e.g. a gocloak username or user id) on success.
// Pluggable rather than hardwired to a specific identity provider: no
// gocloak/JWT/JWKS dependency exists in this module yet (.planning/
// tasks/02-03.json calls for "a valid gocloak token", but there is no
// Keycloak realm configured anywhere in this environment to validate
// against, and guessing at that wiring would be an unreviewed security
// decision). A real implementation plugs in here once one exists; see
// stubTokenValidator for why the current default is not that.
type TokenValidator func(ctx context.Context, token string) (subject string, err error)

// ErrValidatorUnconfigured is stubTokenValidator's error, and therefore
// every /wg/register response until a real TokenValidator is wired in.
var ErrValidatorUnconfigured = errors.New("gocloak token validation is not configured on this server")

// stubTokenValidator rejects every token. This is a deliberate fail-closed
// default: an always-allow stub would make /wg/register silently
// unauthenticated the moment someone forgets to wire a real validator in,
// which is a worse failure mode than the endpoint simply refusing to work
// until it's configured. Swap this for a real gocloak-backed TokenValidator
// once a realm/client exists to validate against.
func stubTokenValidator(ctx context.Context, token string) (string, error) {
	return "", ErrValidatorUnconfigured
}

// peerTracker wraps a *wireguard.Server with registration bookkeeping the
// underlying package doesn't do itself: last-seen timestamps and a sweep
// that removes peers idle past ttl. wireguard.Server intentionally stays
// low-level WG mechanics (see gophermind-lib/wireguard/wireguard.go); this
// is the server-lifecycle layer .planning/tasks/02-03.json asks for.
type peerTracker struct {
	mu       sync.Mutex
	wg       *wireguard.Server
	lastSeen map[string]time.Time // keyed by client public key (hex)
	ttl      time.Duration
	logger   *slog.Logger
}

func newPeerTracker(wg *wireguard.Server, ttl time.Duration, logger *slog.Logger) *peerTracker {
	return &peerTracker{wg: wg, lastSeen: make(map[string]time.Time), ttl: ttl, logger: logger}
}

// Register adds pubKeyHex as a peer (or refreshes its last-seen time if
// already registered -- re-registering is how a client renews before ttl
// expires it, there being no separate "renew" endpoint in the contract).
func (t *peerTracker) Register(pubKeyHex string) (*wireguard.PeerConfig, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, already := t.lastSeen[pubKeyHex]; already {
		t.lastSeen[pubKeyHex] = time.Now()
		// Re-derive the config rather than caching it: cheap (no IPC round
		// trip needed since the peer already exists on the WG device), and
		// avoids a second, divergent source of truth for the response shape.
		return t.wg.PeerConfig(pubKeyHex)
	}
	cfg, err := t.wg.RegisterPeer(pubKeyHex)
	if err != nil {
		return nil, err
	}
	t.lastSeen[pubKeyHex] = time.Now()
	return cfg, nil
}

// Remove removes pubKeyHex, whether it timed out or the caller asked
// explicitly. Removing an already-absent peer is not an error: the end
// state (not registered) is what both callers want, and racing a sweep
// with an explicit removal must not surface as a failure to either.
func (t *peerTracker) Remove(pubKeyHex string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.lastSeen, pubKeyHex)
	err := t.wg.RemovePeer(pubKeyHex)
	if err != nil && errors.Is(err, wireguard.ErrPeerNotFound) {
		return nil
	}
	return err
}

// Sweep removes every peer whose last-seen time is older than ttl,
// returning how many it removed. A ttl <= 0 disables sweeping (Sweep is a
// no-op), matching the "0 = unlimited/off" convention used elsewhere in
// this codebase (e.g. tools.ShellLimits).
func (t *peerTracker) Sweep() int {
	if t.ttl <= 0 {
		return 0
	}
	t.mu.Lock()
	cutoff := time.Now().Add(-t.ttl)
	var stale []string
	for key, seen := range t.lastSeen {
		if seen.Before(cutoff) {
			stale = append(stale, key)
		}
	}
	t.mu.Unlock()

	for _, key := range stale {
		if err := t.Remove(key); err != nil {
			t.logger.Warn("peer sweep: remove failed", "peer", key, "error", err)
			continue
		}
		t.logger.Info("peer timed out", "peer", key)
	}
	return len(stale)
}

// runSweeper calls Sweep on every tick until ctx is done. Started as a
// goroutine from main's run(); its own lifetime is exactly the server's.
func (t *peerTracker) runSweeper(ctx context.Context, interval time.Duration) {
	if t.ttl <= 0 || interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.Sweep()
		}
	}
}

// wgRegisterHandler backs POST /wg/register: validates the caller's gocloak
// token via validate, then registers (or renews) their WireGuard public key
// as a peer, returning the config they need to establish a tunnel.
func wgRegisterHandler(tracker *peerTracker, validate TokenValidator, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "use POST", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Token     string `json:"token"`
			PublicKey string `json:"public_key"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.PublicKey == "" {
			http.Error(w, "public_key is required", http.StatusBadRequest)
			return
		}

		subject, err := validate(r.Context(), req.Token)
		if err != nil {
			// The distinction between "not configured" and "token rejected"
			// matters to an operator debugging a fresh deployment, so it is
			// logged (never in the response body -- neither reason should
			// help an attacker calibrate their next attempt), but both map
			// to the same 401 either way.
			logger.Warn("wg register: token validation failed", "error", err)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		cfg, err := tracker.Register(req.PublicKey)
		if err != nil {
			logger.Warn("wg register: peer registration failed", "subject", subject, "error", err)
			http.Error(w, "registration failed", http.StatusInternalServerError)
			return
		}
		logger.Info("wg peer registered", "subject", subject, "client_address", cfg.ClientAddress)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cfg)
	}
}

// wgServerConfig derives a wireguard.ServerConfig from cfg. Address and
// ListenPort are fixed defaults for now (10.66.0.1, wireguard.DefaultListenPort)
// -- neither serverConfig nor any flag exposes them yet; cfg.WGInterface is
// accepted (--wg-interface / GOPHERMIND_WG_INTERFACE, added in plan 02-01)
// but not yet used here, since gophermind-lib/wireguard is a pure userspace
// netstack implementation with no OS-level interface name concept to bind
// it to (see wireguard.go's package doc comment). It is logged, not
// discarded, so its value is visible even though it isn't load-bearing yet.
func wgServerConfig(cfg serverConfig) wireguard.ServerConfig {
	return wireguard.ServerConfig{
		Address: netip.MustParseAddr("10.66.0.1"),
	}
}

// peerSweepInterval is how often runSweeper checks for timed-out peers.
const peerSweepInterval = 30 * time.Second

// peerTTL is how long a registered peer may go without being re-registered
// before Sweep removes it. Fixed for now (see wgServerConfig's doc comment
// on why Address/ListenPort are also fixed) -- not yet a flag.
const peerTTL = 10 * time.Minute

// startWireGuard initializes the userspace WG server at process startup and
// returns it, its peer tracker (for wiring the /wg/register route and
// starting the sweeper), and an io.Closer suitable for runServer's existing
// shutdown hook. Returns a nil Server (not an error) when cfg.WGInterface is
// empty, treating an unconfigured WG interface as "disabled" rather than
// fatal -- consistent with buildDeps' own treatment of an unreachable LLM
// endpoint: a missing optional piece should not stop the rest of the server
// from starting.
//
// wireguard.NewServer is deliberately given context.Background(), not the
// caller's shutdown ctx: NewServer already self-closes when its context is
// done, which would race independently against runServer's own ordered
// shutdown (HTTP drains, THEN wgCloser.Close()) -- two close triggers firing
// concurrently is harmless (Server.closeLocked is idempotent) but makes the
// drain-before-close guarantee meaningless. The returned Server's lifetime
// is governed solely by the explicit Close() runServer calls.
func startWireGuard(cfg serverConfig, logger *slog.Logger) (*wireguard.Server, *peerTracker, error) {
	if cfg.WGInterface == "" {
		logger.Info("wireguard disabled (no --wg-interface)")
		return nil, nil, nil
	}
	wg, err := wireguard.NewServer(context.Background(), wgServerConfig(cfg))
	if err != nil {
		return nil, nil, fmt.Errorf("start wireguard server: %w", err)
	}
	logger.Info("wireguard server started", "interface", cfg.WGInterface, "public_key", wg.PublicKey())
	tracker := newPeerTracker(wg, peerTTL, logger)
	return wg, tracker, nil
}
