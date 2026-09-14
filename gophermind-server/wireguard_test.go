package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/curve25519"

	"gophermind/gophermind-lib/wireguard"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// freeUDPPort returns a currently-unused UDP port on localhost, so each
// test's wireguard.Server binds its own port instead of all colliding on
// wireguard.DefaultListenPort -- go test runs different packages'
// binaries concurrently, and this package's WG-heavy tests run alongside
// gophermind-osx/connection's, which also starts real wireguard.Server
// instances; both used to silently share the default port and
// intermittently fail/hang against each other.
func freeUDPPort(t *testing.T) uint16 {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("find a free UDP port: %v", err)
	}
	defer conn.Close()
	return uint16(conn.LocalAddr().(*net.UDPAddr).Port)
}

// fakeValidator accepts exactly one token, standing in for a real
// gocloak-backed TokenValidator until one exists to wire in (see
// TokenValidator's doc comment in wireguard.go for why).
func fakeValidator(validToken, subject string) TokenValidator {
	return func(ctx context.Context, token string) (string, error) {
		if token != validToken {
			return "", fmt.Errorf("invalid token")
		}
		return subject, nil
	}
}

// TestStubTokenValidator_RejectsEverything covers the fail-closed default:
// with no real gocloak validator wired in, /wg/register must refuse every
// token rather than silently accepting all of them.
func TestStubTokenValidator_RejectsEverything(t *testing.T) {
	for _, token := range []string{"", "anything", "Bearer x"} {
		_, err := stubTokenValidator(context.Background(), token)
		if err == nil {
			t.Errorf("stubTokenValidator(%q) = nil error, want ErrValidatorUnconfigured", token)
		}
	}
}

// TestStartWireGuard_DisabledWithoutInterface covers "disabled" as a
// legitimate configuration, not an error: an operator who never asked for
// WireGuard shouldn't have server startup fail over it.
func TestStartWireGuard_DisabledWithoutInterface(t *testing.T) {
	wg, tracker, err := startWireGuard(serverConfig{WGInterface: ""}, discardLogger())
	if err != nil {
		t.Fatalf("startWireGuard: %v", err)
	}
	if wg != nil || tracker != nil {
		t.Errorf("wg = %v, tracker = %v, want both nil when WGInterface is empty", wg, tracker)
	}
}

// TestStartWireGuard_CreatesInterface covers "WG interface is created at
// server startup".
func TestStartWireGuard_CreatesInterface(t *testing.T) {
	wg, tracker, err := startWireGuard(serverConfig{WGInterface: "wg0", WGListenPort: freeUDPPort(t)}, discardLogger())
	if err != nil {
		t.Fatalf("startWireGuard: %v", err)
	}
	defer wg.Close()
	if wg == nil || tracker == nil {
		t.Fatal("expected a non-nil Server and tracker when WGInterface is set")
	}
	if wg.PublicKey() == "" {
		t.Error("started server has no public key")
	}
}

// TestWgRegisterHandler_EndToEnd covers "POST /wg/register accepts a valid
// gocloak token and returns peer config (public key, endpoint, allowed
// IPs)" plus the auth-rejection path.
func TestWgRegisterHandler_EndToEnd(t *testing.T) {
	wg, tracker, err := startWireGuard(serverConfig{WGInterface: "wg0", WGListenPort: freeUDPPort(t)}, discardLogger())
	if err != nil {
		t.Fatalf("startWireGuard: %v", err)
	}
	defer wg.Close()

	validate := fakeValidator("good-token", "alice")
	h := wgRegisterHandler(tracker, validate, discardLogger())

	// Wrong token: rejected, no peer registered.
	badBody, _ := json.Marshal(map[string]string{"token": "bad", "public_key": "deadbeef"})
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/wg/register", bytes.NewReader(badBody)))
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("bad token: status = %d, want 401", rr.Code)
	}
	if wg.PeerCount() != 0 {
		t.Errorf("peer count = %d after a rejected registration, want 0", wg.PeerCount())
	}

	// Valid token, real client keypair: registration should succeed and
	// return a usable config.
	clientPriv, clientPub := genKeypair(t)
	goodBody, _ := json.Marshal(map[string]string{"token": "good-token", "public_key": clientPub})
	rr = httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/wg/register", bytes.NewReader(goodBody)))
	if rr.Code != http.StatusOK {
		t.Fatalf("good token: status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var cfg wireguard.PeerConfig
	if err := json.Unmarshal(rr.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if cfg.PublicKey != clientPub || cfg.ServerPublicKey == "" || cfg.Endpoint == "" || cfg.ClientAddress == "" {
		t.Errorf("incomplete peer config: %+v", cfg)
	}
	if wg.PeerCount() != 1 {
		t.Errorf("peer count = %d after registration, want 1", wg.PeerCount())
	}
	_ = clientPriv // used by TestWgRegister_TunnelConnectivity below
}

// TestPeerTracker_Lifecycle covers "Peer lifecycle: add, remove, timeout
// all work" directly against peerTracker, without going through HTTP.
func TestPeerTracker_Lifecycle(t *testing.T) {
	wg, err := wireguard.NewServer(context.Background(), wireguard.ServerConfig{ListenPort: freeUDPPort(t)})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer wg.Close()

	_, pub := genKeypair(t)
	tracker := newPeerTracker(wg, time.Hour, discardLogger()) // long TTL: no auto-timeout in this sub-test

	// Add.
	cfg, err := tracker.Register(pub)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if cfg.PublicKey != pub {
		t.Errorf("registered config public key = %q, want %q", cfg.PublicKey, pub)
	}
	if wg.PeerCount() != 1 {
		t.Fatalf("peer count = %d after Register, want 1", wg.PeerCount())
	}

	// Re-registering (renewal) must not error and must return the same peer.
	cfg2, err := tracker.Register(pub)
	if err != nil {
		t.Fatalf("Register (renew): %v", err)
	}
	if cfg2.ClientAddress != cfg.ClientAddress {
		t.Errorf("renewal changed client address: %q -> %q", cfg.ClientAddress, cfg2.ClientAddress)
	}
	if wg.PeerCount() != 1 {
		t.Errorf("peer count = %d after renewal, want still 1 (not a duplicate)", wg.PeerCount())
	}

	// Remove.
	if err := tracker.Remove(pub); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if wg.PeerCount() != 0 {
		t.Errorf("peer count = %d after Remove, want 0", wg.PeerCount())
	}

	// Removing an already-removed peer is not an error (idempotent).
	if err := tracker.Remove(pub); err != nil {
		t.Errorf("Remove (already gone): %v, want nil (idempotent)", err)
	}
}

// TestPeerTracker_Timeout covers the "timeout" third of the lifecycle
// acceptance criterion: a peer not renewed within ttl is swept away.
func TestPeerTracker_Timeout(t *testing.T) {
	wg, err := wireguard.NewServer(context.Background(), wireguard.ServerConfig{ListenPort: freeUDPPort(t)})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer wg.Close()

	_, pub := genKeypair(t)
	tracker := newPeerTracker(wg, 10*time.Millisecond, discardLogger())
	if _, err := tracker.Register(pub); err != nil {
		t.Fatalf("Register: %v", err)
	}

	time.Sleep(20 * time.Millisecond)
	removed := tracker.Sweep()
	if removed != 1 {
		t.Errorf("Sweep removed %d peers, want 1", removed)
	}
	if wg.PeerCount() != 0 {
		t.Errorf("peer count = %d after timeout sweep, want 0", wg.PeerCount())
	}
}

// TestPeerTracker_ZeroTTLDisablesSweep confirms the documented "ttl <= 0
// means off" convention: a never-renewed peer must survive Sweep when TTL
// tracking is disabled, matching tools.ShellLimits' 0-means-unlimited
// convention used elsewhere in this codebase.
func TestPeerTracker_ZeroTTLDisablesSweep(t *testing.T) {
	wg, err := wireguard.NewServer(context.Background(), wireguard.ServerConfig{ListenPort: freeUDPPort(t)})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer wg.Close()

	_, pub := genKeypair(t)
	tracker := newPeerTracker(wg, 0, discardLogger())
	if _, err := tracker.Register(pub); err != nil {
		t.Fatalf("Register: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if removed := tracker.Sweep(); removed != 0 {
		t.Errorf("Sweep removed %d peers with ttl<=0, want 0 (disabled)", removed)
	}
}

// TestWgRegister_TunnelConnectivity is the "Integration test: register
// peer, verify config, test tunnel connectivity" acceptance criterion,
// literally: register through the real HTTP handler, then use the
// returned config to build a wireguard.Client and prove an HTTP request
// actually round-trips through the tunnel end to end.
func TestWgRegister_TunnelConnectivity(t *testing.T) {
	wg, tracker, err := startWireGuard(serverConfig{WGInterface: "wg0", WGListenPort: freeUDPPort(t)}, discardLogger())
	if err != nil {
		t.Fatalf("startWireGuard: %v", err)
	}
	defer wg.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	clientPriv, clientPub := genKeypair(t)
	h := wgRegisterHandler(tracker, fakeValidator("t", "alice"), discardLogger())
	body, _ := json.Marshal(map[string]string{"token": "t", "public_key": clientPub})
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/wg/register", bytes.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("register status = %d: %s", rr.Code, rr.Body.String())
	}
	var peerCfg wireguard.PeerConfig
	if err := json.Unmarshal(rr.Body.Bytes(), &peerCfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}

	// Serve something on the WG server's side of the tunnel.
	ln, err := wg.ListenTCP(8080)
	if err != nil {
		t.Fatalf("ListenTCP: %v", err)
	}
	httpSrv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "hello over registered tunnel")
	})}
	go httpSrv.Serve(ln)
	defer httpSrv.Close()

	client, err := wireguard.NewClient(ctx, wireguard.ClientConfig{
		ServerPublicKey: peerCfg.ServerPublicKey,
		ServerEndpoint:  peerCfg.Endpoint,
		PrivateKey:      clientPriv,
		Address:         mustParseAddr(t, peerCfg.ClientAddress),
		AllowedIPs:      peerCfg.AllowedIPs,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	time.Sleep(1 * time.Second) // handshake

	resp, err := client.HTTPClient().Get("http://" + strings.TrimSuffix(peerCfg.AllowedIPs, "/32") + ":8080/")
	if err != nil {
		t.Fatalf("tunnel HTTP request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// genKeypair generates a real Curve25519 keypair in WireGuard's format for
// tests, returning (32-byte private key for wireguard.ClientConfig.PrivateKey,
// hex-encoded public key for registration). Mirrors wireguard.go's own
// unexported derivePublicKey, which isn't reachable from this package.
func genKeypair(t *testing.T) ([]byte, string) {
	t.Helper()
	priv := make([]byte, 32)
	if _, err := rand.Read(priv); err != nil {
		t.Fatalf("generate private key: %v", err)
	}
	var privArr, pubArr [32]byte
	copy(privArr[:], priv)
	curve25519.ScalarBaseMult(&pubArr, &privArr)
	return priv, hex.EncodeToString(pubArr[:])
}

func mustParseAddr(t *testing.T, s string) netip.Addr {
	t.Helper()
	addr, err := netip.ParseAddr(s)
	if err != nil {
		t.Fatalf("parse address %q: %v", s, err)
	}
	return addr
}
