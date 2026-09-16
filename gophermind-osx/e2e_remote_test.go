//go:build e2e

package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/curve25519"

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

// e2eConnectRemote is not implemented: remote mode cannot authenticate yet,
// so there is nothing for these tests to connect to.
//
// What was here tried to run a real gophermind-server behind the tunnel by
// spawning it with exec.Command and --port 8090, alongside a listener from
// wgSrv.ListenTCP(8090). Those are two different ports on two different
// network stacks. wireguard.Server.ListenTCP returns a listener on the
// userspace netstack held in THIS process's memory (wireguard.go:252,
// s.tnet.ListenTCP); a separately spawned OS process binds the host's port
// 8090 and can never appear inside it. The listener was left unserved, which
// is why the file did not compile: "declared and not used: ln".
//
// Serving the real serve.NewMux in-process on that listener fixes the network
// side, and is what this should become. It still would not pass: connectRemote
// (connection.go:181) builds its client with no Token, and RemoteConfig has no
// field to carry one. /healthz is unauthenticated so Connect reports Connected,
// but every /session route answers 401. Local mode generates a token and
// Direct mode gained a Token field in 5cecba8; remote mode is the only one
// that cannot authenticate at all.
//
// So the order is: give RemoteConfig a Token, wire it through connectRemote,
// then write this helper against serve.NewMux. That is roadmap task 05-02.
func e2eConnectRemote(t *testing.T) *connection.Connection {
	t.Helper()
	t.Skip("remote mode has no bearer token: RemoteConfig carries none and connectRemote sends none, so every /session route 401s through the tunnel. See 05-02.")
	return nil
}

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
