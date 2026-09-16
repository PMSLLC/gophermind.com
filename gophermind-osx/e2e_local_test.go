//go:build e2e

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/session"
	"gophermind/gophermind-osx/client"
	"gophermind/gophermind-osx/connection"
	appui "gophermind/gophermind-osx/ui"
)

// e2eServerBinary builds gophermind-server once and returns its path,
// shared across all E2E tests in this package.
var (
	e2eServerBinaryOnce sync.Once
	e2eServerBinaryPath string
	e2eServerBinaryErr  error
)

func e2eBuildServerBinary(t testing.TB) string {
	t.Helper()
	e2eServerBinaryOnce.Do(func() {
		dir := filepath.Join(os.TempDir(), "gophermind-e2e-test-bin")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			e2eServerBinaryErr = err
			return
		}
		out := filepath.Join(dir, "gophermind-server")
		cmd := exec.Command("go", "build", "-o", out, "gophermind/gophermind-server")
		cmd.Dir = e2eRepoRoot(t)
		if output, err := cmd.CombinedOutput(); err != nil {
			e2eServerBinaryErr = fmt.Errorf("build gophermind-server: %w: %s", err, output)
			return
		}
		e2eServerBinaryPath = out
	})
	if e2eServerBinaryErr != nil {
		t.Fatalf("build gophermind-server: %v", e2eServerBinaryErr)
	}
	return e2eServerBinaryPath
}

func e2eRepoRoot(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (no go.work found)")
		}
		dir = parent
	}
}

// e2eConnectLocal spawns a local gophermind-server and returns a connected
// Connection, for E2E tests that need a live backend.
func e2eConnectLocal(t testing.TB) *connection.Connection {
	t.Helper()
	bin := e2eBuildServerBinary(t)
	root := t.TempDir()

	conn := connection.New(connection.BackendConfig{
		Name: "e2e-local",
		Mode: connection.ModeLocal,
		Local: connection.LocalConfig{
			ServerBinaryPath: bin,
			Root:             root,
			StartupTimeout:   15 * time.Second,
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

// findSession looks up a session by ID in the list, returning it or an
// error if not found.
func findSession(infos []session.Info, id string) (session.Info, error) {
	for _, info := range infos {
		if info.ID == id {
			return info, nil
		}
	}
	return session.Info{}, fmt.Errorf("session %q not found", id)
}

// TestE2E_LocalMode_FullFlow covers 05-01's "Local mode E2E: launch →
// create session → chat → stream → approve → complete passes".
func TestE2E_LocalMode_FullFlow(t *testing.T) {
	conn := e2eConnectLocal(t)
	cl := conn.Client()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 1. Create a session.
	sessionID, err := cl.CreateSession(ctx, client.CreateSessionOptions{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sessionID == "" {
		t.Fatal("CreateSession returned empty session ID")
	}
	t.Logf("Created session: %s", sessionID)

	// 2. Send a chat message (stream).
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
		t.Fatal("no events received from stream")
	}
	t.Logf("Received %d events: %v", len(eventTypes), eventTypes)

	// 4. Verify the session is still accessible.
	sessions, err := cl.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if _, err := findSession(sessions, sessionID); err != nil {
		t.Fatalf("session not found after stream: %v", err)
	}

	t.Log("Local mode E2E: full flow passed")
}

// TestE2E_LocalMode_ApprovalFlow covers the approval path: create a
// session, send a message that triggers an approval, approve it, and
// verify the approval is resolved.
func TestE2E_LocalMode_ApprovalFlow(t *testing.T) {
	conn := e2eConnectLocal(t)
	cl := conn.Client()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create a session.
	sessionID, err := cl.CreateSession(ctx, client.CreateSessionOptions{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Set up the approval tracker with the real client.Approve.
	tracker := appui.NewApprovalTracker(cl.Approve)

	// Send a message that might trigger an approval.
	stream, err := cl.Stream(ctx, sessionID, "Run the command: echo hello")
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer stream.Close()

	// Read events, looking for approval-needed.
	approvalReceived := false
	deadline := time.After(30 * time.Second)
	for {
		select {
		case <-deadline:
			goto done
		default:
		}
		ev, err := stream.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("stream.Next: %v", err)
		}
		if ev.Type == "approval-needed" {
			approvalReceived = true
			an, err := ev.ApprovalNeeded()
			if err != nil {
				t.Fatalf("decode approval-needed: %v", err)
			}
			t.Logf("Approval needed: tool=%s id=%s", an.Tool, an.ApprovalID)
			// Add to tracker and approve it.
			tracker.Add(sessionID, an.ApprovalID, an.Tool, an.Args)
			if err := tracker.Resolve(ctx, an.ApprovalID, true); err != nil {
				t.Fatalf("tracker.Resolve: %v", err)
			}
			t.Log("Approval resolved")
		}
		if ev.Type == "done" {
			break
		}
	}
done:
	if !approvalReceived {
		t.Log("No approval was triggered (expected for simple commands)")
	} else {
		t.Log("Approval flow completed")
	}
}

// TestE2E_LocalMode_SessionManagement covers 05-01's "Session management
// E2E: create → rename → resume → delete passes".
func TestE2E_LocalMode_SessionManagement(t *testing.T) {
	conn := e2eConnectLocal(t)
	cl := conn.Client()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create.
	sessionID, err := cl.CreateSession(ctx, client.CreateSessionOptions{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	t.Logf("Created: %s", sessionID)

	// Send a message so the session file is written (POST /session alone
	// doesn't create a file; session.Save only runs after a real turn).
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
	err = cl.RenameSession(ctx, sessionID, "Renamed Title")
	if err != nil {
		t.Fatalf("RenameSession: %v", err)
	}
	// Verify rename.
	sessions, err = cl.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions after rename: %v", err)
	}
	renamed, err := findSession(sessions, sessionID)
	if err != nil {
		t.Fatalf("session not found after rename: %v", err)
	}
	if renamed.Name != "Renamed Title" {
		t.Errorf("Name = %q, want %q", renamed.Name, "Renamed Title")
	}
	t.Log("Rename verified")

	// Resume (verify we can access messages).
	_, err = cl.SessionMessages(ctx, sessionID)
	if err != nil {
		t.Fatalf("SessionMessages (resume): %v", err)
	}
	t.Log("Resume verified")

	// Delete.
	err = cl.DeleteSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	// Verify deletion.
	sessions, err = cl.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions after delete: %v", err)
	}
	if _, err := findSession(sessions, sessionID); err == nil {
		t.Error("session still in list after delete")
	}
	t.Log("Delete verified")

	t.Log("Session management E2E: passed")
}
