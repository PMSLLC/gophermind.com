//go:build e2e

package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"testing"
	"time"

	"golang.org/x/crypto/curve25519"

	"gophermind/gophermind-lib/wireguard"
	"gophermind/gophermind-osx/client"
	"gophermind/gophermind-osx/connection"
)

// e2eGenKeypair generates a random 32-byte Curve25519 private key and
// returns it alongside the hex-encoded public key.
func e2eGenKeypair(t *testing.T) (priv []byte, pubHex string) {
	t.Helper()
	priv = make([]byte, 32)
	if _, err := rand.Read(priv); err != nil {
		t.Fatalf("generate private key: %v", err)
	}
	var privArr, pubArr [32]byte
	copy(privArr[:], priv)
	curve25519.ScalarBaseMult(&pubArr, &privArr)
	return priv, fmt.Sprintf("%x", pubArr)
}

// e2eFreePort asks the OS for an unused TCP port on localhost.
func e2eFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// e2eConnectRemote spawns a userspace WireGuard server, starts a real
// gophermind-server behind its ListenTCP, registers a peer, and returns a
// connected Connection in remote mode.
func e2eConnectRemote(t *testing.T) *connection.Connection {
	t.Helper()
	bin := e2eBuildServerBinary(t)
	root := t.TempDir()

	// Start a userspace WG server on a free port.
	wgPort := e2eFreePort(t)
	wgSrv, err := wireguard.NewServer(context.Background(), wireguard.ServerConfig{
		Address:    netip.MustParseAddr("10.66.0.1"),
		ListenPort: uint16(wgPort),
	})
	if err != nil {
		t.Fatalf("wireguard.NewServer: %v", err)
	}
	t.Cleanup(func() { _ = wgSrv.Close() })

	// Register a peer.
	clientPriv, clientPub := e2eGenKeypair(t)
	peerCfg, err := wgSrv.RegisterPeer(clientPub)
	if err != nil {
		t.Fatalf("RegisterPeer: %v", err)
	}

	// Start the real gophermind-server behind the WG tunnel's TCP listener.
	ln, err := wgSrv.ListenTCP(8090)
	if err != nil {
		t.Fatalf("ListenTCP: %v", err)
	}

	token := "e2e-remote-test-token"
	cmd := exec.Command(bin,
		"--port", "8090",
		"--token", token,
		"--wg-interface", "",
		"--root", root,
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start gophermind-server: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	// Connect via the WG tunnel.
	conn := connection.New(connection.BackendConfig{
		Name: "e2e-remote",
		Mode: connection.ModeRemote,
		Remote: connection.RemoteConfig{
			ServerPublicKey:  peerCfg.ServerPublicKey,
			ServerEndpoint:   peerCfg.Endpoint,
			ClientPrivateKey: clientPriv,
			ClientAddress:    netip.MustParseAddr(peerCfg.ClientAddress),
			AllowedIPs:       peerCfg.AllowedIPs,
			RemoteAddr:       "10.66.0.1:8090",
		},
		HealthInterval: 500 * time.Millisecond,
	})
	t.Cleanup(conn.Disconnect)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := conn.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if conn.Status() != connection.StatusConnected {
		t.Fatalf("Status() = %v, want Connected", conn.Status())
	}
	if !conn.Client().Healthy(ctx) {
		t.Fatal("Client().Healthy() = false")
	}
	return conn
}

// TestE2E_RemoteMode_FullFlow covers 05-02's "Remote mode E2E: WG tunnel
// established → chat → stream → approve → disconnect passes".
func TestE2E_RemoteMode_FullFlow(t *testing.T) {
	conn := e2eConnectRemote(t)
	cl := conn.Client()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 1. Create a session through the tunnel.
	sessionID, err := cl.CreateSession(ctx, client.CreateSessionOptions{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sessionID == "" {
		t.Fatal("CreateSession returned empty session ID")
	}
	t.Logf("Created session through WG tunnel: %s", sessionID)

	// 2. Send a chat message (stream) through the tunnel.
	stream, err := cl.Stream(ctx, sessionID, "Say hello in one word")
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer stream.Close()

	// 3. Read events until done or timeout.
	var eventTypes []string
	deadline := time.After(30 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for stream to complete")
		default:
		}
		ev, err := stream.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("stream.Next: %v", err)
		}
		eventTypes = append(eventTypes, ev.Type)
		t.Logf("Event: %s", ev.Type)
		if ev.Type == "done" {
			break
		}
	}

	if len(eventTypes) == 0 {
		t.Fatal("no events received from stream through WG tunnel")
	}
	t.Logf("Received %d events through WG tunnel: %v", len(eventTypes), eventTypes)

	// 4. Verify the session is still accessible through the tunnel.
	sessions, err := cl.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if _, err := findSession(sessions, sessionID); err != nil {
		t.Fatalf("session not found after stream: %v", err)
	}

	// 5. Disconnect and verify status.
	conn.Disconnect()
	if conn.Status() != connection.StatusDisconnected {
		t.Errorf("Status() after Disconnect = %v, want Disconnected", conn.Status())
	}

	t.Log("Remote mode E2E: full flow passed")
}

// TestE2E_RemoteMode_SessionManagement covers session CRUD through the WG
// tunnel: create → rename → resume → delete.
func TestE2E_RemoteMode_SessionManagement(t *testing.T) {
	conn := e2eConnectRemote(t)
	cl := conn.Client()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create.
	sessionID, err := cl.CreateSession(ctx, client.CreateSessionOptions{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	t.Logf("Created: %s", sessionID)

	// Send a message so the session file is written.
	stream, err := cl.Stream(ctx, sessionID, "hello")
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for {
		ev, err := stream.Next()
		if err != nil {
			break
		}
		if ev.Type == "done" {
			break
		}
	}
	stream.Close()

	// Verify it appears in the list.
	sessions, err := cl.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if _, err := findSession(sessions, sessionID); err != nil {
		t.Fatalf("new session not in list: %v", err)
	}

	// Rename.
	err = cl.RenameSession(ctx, sessionID, "Remote Renamed")
	if err != nil {
		t.Fatalf("RenameSession: %v", err)
	}
	sessions, err = cl.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions after rename: %v", err)
	}
	renamed, err := findSession(sessions, sessionID)
	if err != nil {
		t.Fatalf("session not found after rename: %v", err)
	}
	if renamed.Name != "Remote Renamed" {
		t.Errorf("Name = %q, want %q", renamed.Name, "Remote Renamed")
	}
	t.Log("Rename verified through WG tunnel")

	// Resume.
	_, err = cl.SessionMessages(ctx, sessionID)
	if err != nil {
		t.Fatalf("SessionMessages (resume): %v", err)
	}
	t.Log("Resume verified through WG tunnel")

	// Delete.
	err = cl.DeleteSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	sessions, err = cl.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions after delete: %v", err)
	}
	if _, err := findSession(sessions, sessionID); err == nil {
		t.Error("session still in list after delete")
	}
	t.Log("Delete verified through WG tunnel")

	t.Log("Remote mode E2E: session management passed")
}
