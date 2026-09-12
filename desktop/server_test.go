package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestNewTokenLooksRandom checks the shape newToken's callers rely on: 32
// bytes hex-encoded (64 hex characters), and two calls never collide.
func TestNewTokenLooksRandom(t *testing.T) {
	a, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}
	b, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}
	if len(a) != 64 || len(b) != 64 {
		t.Fatalf("expected 64 hex chars (32 bytes), got %d and %d", len(a), len(b))
	}
	if a == b {
		t.Fatalf("two calls to newToken produced the same token")
	}
}

// localLLMEndpoint is the developer-machine LLM endpoint used to prove the
// embedded server for real, end to end. It is intentionally not
// configurable: this test either finds a real OpenAI-compatible endpoint
// there or skips, so it never depends on network access to anything beyond
// localhost.
const localLLMEndpoint = "127.0.0.1:8080"

// requireLocalLLM skips the test when no endpoint answers at
// localLLMEndpoint. The embedded-server proof below needs a real
// OpenAI-compatible endpoint to construct an llm.Client against (model
// discovery/validation happens at startup); it is not gophermind-specific
// infrastructure, so its absence is a skip, not a failure.
func requireLocalLLM(t *testing.T) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", localLLMEndpoint, 500*time.Millisecond)
	if err != nil {
		t.Skipf("no local endpoint at %s; skipping embedded-server integration test", localLLMEndpoint)
	}
	_ = conn.Close()
}

// TestEmbeddedServerHealthzAndSession is the proof this task's verification
// step asks for: start the same embedded-server code path app.go uses, hit
// /healthz unauthenticated, confirm the protected routes reject a missing or
// wrong token, then create a session and assert the response. It never opens
// a window, so it runs headlessly.
func TestEmbeddedServerHealthzAndSession(t *testing.T) {
	requireLocalLLM(t)

	t.Setenv("GOPHERMIND_BASE_URL", "http://"+localLLMEndpoint)
	t.Setenv("GOPHERMIND_MODEL", "") // let it auto-discover from the endpoint
	t.Setenv("GOPHERMIND_ROOT", t.TempDir())
	isolate(t)
	t.Setenv("GOPHERMIND_APPROVAL", "auto")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv, err := startEmbeddedServer(ctx)
	if err != nil {
		t.Fatalf("startEmbeddedServer: %v", err)
	}
	defer func() {
		if err := srv.Shutdown(); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	}()

	if !strings.HasPrefix(srv.BaseURL, "http://127.0.0.1:") {
		t.Fatalf("expected a loopback base URL, got %q", srv.BaseURL)
	}
	if len(srv.Token) != 64 {
		t.Fatalf("expected a 64-char hex token, got %d chars", len(srv.Token))
	}

	// /healthz requires no auth and always answers 200 while the process is
	// running.
	resp, err := http.Get(srv.BaseURL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz: want 200, got %d", resp.StatusCode)
	}

	// POST /session without a bearer token is rejected: the loopback listener
	// is still local-network-reachable, so auth must hold even though no
	// window has opened yet.
	resp, err = http.Post(srv.BaseURL+"/session", "", nil)
	if err != nil {
		t.Fatalf("POST /session (no token): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST /session with no token: want 401, got %d", resp.StatusCode)
	}

	// POST /session with a wrong token is rejected too, not just a missing
	// one.
	req, _ := http.NewRequest(http.MethodPost, srv.BaseURL+"/session", nil)
	req.Header.Set("Authorization", "Bearer not-the-real-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /session (wrong token): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST /session with a wrong token: want 401, got %d", resp.StatusCode)
	}

	// POST /session with the real token creates a session.
	req, _ = http.NewRequest(http.MethodPost, srv.BaseURL+"/session", nil)
	req.Header.Set("Authorization", "Bearer "+srv.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /session: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /session: want 200, got %d", resp.StatusCode)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode /session response: %v", err)
	}
	if created.ID == "" {
		t.Fatal("POST /session returned an empty session id")
	}
}

// TestEmbeddedServerLoopbackOnly asserts the listener is bound to 127.0.0.1,
// not every interface: any address reported by the OS for a non-loopback
// interface must fail to connect on the resolved port.
func TestEmbeddedServerLoopbackOnly(t *testing.T) {
	requireLocalLLM(t)

	t.Setenv("GOPHERMIND_BASE_URL", "http://"+localLLMEndpoint)
	t.Setenv("GOPHERMIND_MODEL", "")
	t.Setenv("GOPHERMIND_ROOT", t.TempDir())
	isolate(t)
	t.Setenv("GOPHERMIND_APPROVAL", "auto")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv, err := startEmbeddedServer(ctx)
	if err != nil {
		t.Fatalf("startEmbeddedServer: %v", err)
	}
	defer srv.Shutdown()

	_, port, err := net.SplitHostPort(strings.TrimPrefix(srv.BaseURL, "http://"))
	if err != nil {
		t.Fatalf("parse base URL: %v", err)
	}

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Skipf("could not enumerate interfaces: %v", err)
	}
	tried := 0
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() || ipNet.IP.To4() == nil {
			continue
		}
		tried++
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(ipNet.IP.String(), port), 300*time.Millisecond)
		if err == nil {
			conn.Close()
			t.Fatalf("connected to the embedded server on non-loopback address %s; expected refusal", ipNet.IP.String())
		}
	}
	if tried == 0 {
		t.Skip("no non-loopback IPv4 interface available to test against")
	}
}
