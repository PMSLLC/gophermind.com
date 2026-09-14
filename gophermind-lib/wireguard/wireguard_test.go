package wireguard

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"testing"
	"time"
)

// freePort finds an available UDP port for the WG interface.
func freePort(t *testing.T) uint16 {
	t.Helper()
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	defer pc.Close()
	addr := pc.LocalAddr().(*net.UDPAddr)
	return uint16(addr.Port)
}

// generateTestKeyPair generates a random 32-byte private key and returns
// its hex encoding (64 chars). The public key is derived by WireGuard
// internally from the private key.
func generateTestKeyPair(t *testing.T) (privHex string) {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return hex.EncodeToString(key)
}

func TestServerCreation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	srv, err := NewServer(ctx, ServerConfig{
		ListenPort: freePort(t),
		Address:    netip.MustParseAddr("10.66.0.1"),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	if srv.PublicKey() == "" {
		t.Error("expected non-empty public key")
	}
	if srv.PeerCount() != 0 {
		t.Errorf("expected 0 peers, got %d", srv.PeerCount())
	}
}

func TestPeerRegistration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	srv, err := NewServer(ctx, ServerConfig{
		ListenPort: freePort(t),
		Address:    netip.MustParseAddr("10.66.0.1"),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	// Generate a client key pair. The public key is derived by WireGuard
	// from the private key, so we need to use the actual public key.
	// For registration, we pass the client's public key (hex).
	// We generate a random 32-byte value as a stand-in public key for
	// the IPC registration (WireGuard accepts any 32-byte key).
	clientPub := generateTestKeyPair(t)

	cfg, err := srv.RegisterPeer(clientPub)
	if err != nil {
		t.Fatalf("RegisterPeer: %v", err)
	}

	if cfg.PublicKey != clientPub {
		t.Errorf("expected public key %q, got %q", clientPub, cfg.PublicKey)
	}
	if cfg.ServerPublicKey == "" {
		t.Error("expected non-empty server public key")
	}
	if cfg.ClientAddress == "" {
		t.Error("expected non-empty client address")
	}
	if srv.PeerCount() != 1 {
		t.Errorf("expected 1 peer, got %d", srv.PeerCount())
	}

	// Duplicate registration should fail.
	_, err = srv.RegisterPeer(clientPub)
	if err == nil {
		t.Error("expected error for duplicate peer")
	}

	// Remove the peer.
	if err := srv.RemovePeer(clientPub); err != nil {
		t.Fatalf("RemovePeer: %v", err)
	}
	if srv.PeerCount() != 0 {
		t.Errorf("expected 0 peers after removal, got %d", srv.PeerCount())
	}
}

func TestTunnelEstablishment(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Start server on a known port.
	srvPort := freePort(t)
	srv, err := NewServer(ctx, ServerConfig{
		ListenPort: srvPort,
		Address:    netip.MustParseAddr("10.66.0.1"),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	// Generate a real client key pair. We need the private key for the
	// client and the public key for server registration.
	clientPriv := make([]byte, 32)
	if _, err := rand.Read(clientPriv); err != nil {
		t.Fatalf("generate client key: %v", err)
	}

	// The public key is derived from the private key by WireGuard.
	// We need to get it by creating a temporary device or using the
	// client's PublicKey() after creation. For registration, we need
	// the public key before creating the client.
	//
	// WireGuard derives the public key from the private key using
	// Curve25519. We can compute it using the same method the
	// wireguard library uses internally.
	//
	// For simplicity, we create the client first (which derives the
	// public key), then register that key with the server.
	// But the server needs the peer registered before the client can
	// handshake. So we use a two-step approach:
	// 1. Create a temporary device to derive the public key
	// 2. Register with the server
	// 3. Create the real client

	// Derive the public key from the private key.
	clientPub := derivePublicKey(clientPriv)

	// Register the client peer with the server.
	peerCfg, err := srv.RegisterPeer(clientPub)
	if err != nil {
		t.Fatalf("RegisterPeer: %v", err)
	}

	// Start an HTTP server on the tunnel.
	ln, err := srv.ListenTCP(8080)
	if err != nil {
		t.Fatalf("ListenTCP: %v", err)
	}
	httpSrv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, "hello from tunnel")
		}),
	}
	go httpSrv.Serve(ln)
	defer httpSrv.Close()

	// Create a client using the same private key.
	client, err := NewClient(ctx, ClientConfig{
		ServerPublicKey: peerCfg.ServerPublicKey,
		ServerEndpoint:  fmt.Sprintf("127.0.0.1:%d", srvPort),
		PrivateKey:      clientPriv,
		Address:         netip.MustParseAddr(peerCfg.ClientAddress),
		AllowedIPs:      peerCfg.AllowedIPs,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	// Wait for the tunnel to establish (handshake takes a moment).
	time.Sleep(1 * time.Second)

	// Try to connect through the tunnel.
	conn, err := client.DialTCP(&net.TCPAddr{
		IP:   net.ParseIP("10.66.0.1"),
		Port: 8080,
	})
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	defer conn.Close()

	// Send an HTTP request.
	fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: 10.66.0.1\r\nConnection: close\r\n\r\n")

	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("ReadResponse: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if len(body) == 0 {
		t.Error("expected non-empty response body")
	}
}

func TestMultipleTunnels(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	srv, err := NewServer(ctx, ServerConfig{
		ListenPort: freePort(t),
		Address:    netip.MustParseAddr("10.66.0.1"),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	// Register multiple peers.
	for i := 0; i < 3; i++ {
		pub := generateTestKeyPair(t)
		if _, err := srv.RegisterPeer(pub); err != nil {
			t.Fatalf("RegisterPeer %d: %v", i, err)
		}
	}

	if srv.PeerCount() != 3 {
		t.Errorf("expected 3 peers, got %d", srv.PeerCount())
	}
}

func TestServerClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	srv, err := NewServer(ctx, ServerConfig{
		ListenPort: freePort(t),
		Address:    netip.MustParseAddr("10.66.0.1"),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	pub := generateTestKeyPair(t)
	if _, err := srv.RegisterPeer(pub); err != nil {
		t.Fatalf("RegisterPeer: %v", err)
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Double close should not panic.
	if err := srv.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestClientClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	srv, err := NewServer(ctx, ServerConfig{
		ListenPort: freePort(t),
		Address:    netip.MustParseAddr("10.66.0.1"),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	clientPriv := make([]byte, 32)
	if _, err := rand.Read(clientPriv); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	clientPub := derivePublicKey(clientPriv)

	peerCfg, err := srv.RegisterPeer(clientPub)
	if err != nil {
		t.Fatalf("RegisterPeer: %v", err)
	}

	client, err := NewClient(ctx, ClientConfig{
		ServerPublicKey: peerCfg.ServerPublicKey,
		ServerEndpoint:  peerCfg.Endpoint,
		PrivateKey:      clientPriv,
		Address:         netip.MustParseAddr(peerCfg.ClientAddress),
		AllowedIPs:      peerCfg.AllowedIPs,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Double close should not panic.
	if err := client.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
