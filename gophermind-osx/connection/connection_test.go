package connection

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/curve25519"

	"gophermind/gophermind-lib/wireguard"
)

// serverBinaryPath is built once (go build takes a few seconds) and shared
// across every local-mode test in this package.
var (
	serverBinaryOnce sync.Once
	serverBinaryPath string
	serverBinaryErr  error
)

func buildServerBinary(t *testing.T) string {
	t.Helper()
	serverBinaryOnce.Do(func() {
		dir := t.TempDir()
		// t.TempDir() is per-test and cleaned up after that test returns,
		// which would remove the binary before later tests in this package
		// run it -- so this specific call uses a directory under os.TempDir
		// instead, cleaned up manually via TestMain.
		dir = filepath.Join(os.TempDir(), "gophermind-connection-test-bin")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			serverBinaryErr = err
			return
		}
		out := filepath.Join(dir, "gophermind-server")
		cmd := exec.Command("go", "build", "-o", out, "gophermind/gophermind-server")
		cmd.Dir = repoRoot(t)
		if output, err := cmd.CombinedOutput(); err != nil {
			serverBinaryErr = fmt.Errorf("build gophermind-server: %w: %s", err, output)
			return
		}
		serverBinaryPath = out
	})
	if serverBinaryErr != nil {
		t.Fatalf("build gophermind-server: %v", serverBinaryErr)
	}
	return serverBinaryPath
}

// repoRoot finds the repository root (the directory containing go.work) by
// walking up from this test file's own source location, so `go build` runs
// in the right module context regardless of the test binary's cwd.
func repoRoot(t *testing.T) string {
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

func TestMain(m *testing.M) {
	code := m.Run()
	os.RemoveAll(filepath.Join(os.TempDir(), "gophermind-connection-test-bin"))
	os.Exit(code)
}

func TestStatus_String(t *testing.T) {
	cases := map[Status]string{
		StatusDisconnected:  "disconnected",
		StatusConnecting:    "connecting",
		StatusConnected:     "connected",
		StatusReconnecting:  "reconnecting",
		StatusDisconnecting: "disconnecting",
		Status(99):          "unknown",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("Status(%d).String() = %q, want %q", s, got, want)
		}
	}
}

// TestConnection_LocalMode_ConnectsAndHealthy covers "Local mode: connects
// to localhost:port with random token, health check passes".
func TestConnection_LocalMode_ConnectsAndHealthy(t *testing.T) {
	bin := buildServerBinary(t)
	root := t.TempDir()

	var statuses []Status
	var mu sync.Mutex
	conn := New(BackendConfig{
		Name: "local",
		Mode: ModeLocal,
		Local: LocalConfig{
			ServerBinaryPath: bin,
			Root:             root,
			StartupTimeout:   10 * time.Second,
		},
		HealthInterval: 200 * time.Millisecond,
		OnStatusChange: func(s Status) {
			mu.Lock()
			statuses = append(statuses, s)
			mu.Unlock()
		},
	})
	defer conn.Disconnect()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := conn.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if got := conn.Status(); got != StatusConnected {
		t.Errorf("Status() = %v, want Connected", got)
	}
	cl := conn.Client()
	if cl == nil {
		t.Fatal("Client() = nil after successful Connect")
	}
	if !cl.Healthy(context.Background()) {
		t.Error("Client().Healthy() = false, want true")
	}

	mu.Lock()
	gotStatuses := append([]Status(nil), statuses...)
	mu.Unlock()
	if len(gotStatuses) < 2 || gotStatuses[0] != StatusConnecting || gotStatuses[1] != StatusConnected {
		t.Errorf("status sequence = %v, want it to start [Connecting, Connected]", gotStatuses)
	}
}

// TestConnection_LocalMode_RandomTokenIsEnforced covers the "random token"
// half of the acceptance criterion directly, as a real security property
// rather than just "a token field is set": a request to the spawned
// server's real port, with no (or the wrong) token, must be rejected. A
// fixed port (rather than the usual auto-picked one) lets the test dial it
// independently of the Connection's own *client.Client, whose generated
// token is intentionally not exposed outside this package.
func TestConnection_LocalMode_RandomTokenIsEnforced(t *testing.T) {
	bin := buildServerBinary(t)
	port, err := freePort()
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}

	conn := New(BackendConfig{
		Name: "local-fixed-port",
		Mode: ModeLocal,
		Local: LocalConfig{
			ServerBinaryPath: bin,
			Port:             port,
			Root:             t.TempDir(),
			StartupTimeout:   10 * time.Second,
		},
	})
	defer conn.Disconnect()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := conn.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// The connection's own client, with the correct generated token, must
	// be able to list sessions.
	if _, err := conn.Client().ListSessions(ctx); err != nil {
		t.Errorf("ListSessions with the correct token: %v", err)
	}

	// A direct request to the same port with no token must be rejected --
	// proving a real, enforced token is required, not that the endpoint is
	// open and the token is decorative.
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/session", port), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unauthenticated request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated request status = %d, want 401", resp.StatusCode)
	}
}

// genKeypair generates a real Curve25519 keypair in WireGuard's format,
// mirroring gophermind-server/wireguard_test.go's identical helper (not
// importable across module/package boundaries, so duplicated here).
func genKeypair(t *testing.T) (priv []byte, pubHex string) {
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

// TestConnection_RemoteMode_ConnectsAndHealthy covers "Remote mode:
// establishes WG tunnel, routes HTTP through tunnel, health check passes".
// A plain http.Server behind the WG server's ListenTCP stands in for
// gophermind-server -- this package's job is routing through the tunnel
// correctly, which gophermind-server/wireguard_test.go's own
// TestWgRegister_TunnelConnectivity already proves works end to end with
// the real binary; duplicating that here would test the same thing twice.
func TestConnection_RemoteMode_ConnectsAndHealthy(t *testing.T) {
	// An explicit free port, not the package default: go test can run this
	// package's tests concurrently with other packages' (e.g.
	// gophermind-lib/wireguard's own suite), and an unset ListenPort would
	// have every such server bind the same default port.
	wgPort, err := freePort()
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	wgSrv, err := wireguard.NewServer(context.Background(), wireguard.ServerConfig{
		Address:    netip.MustParseAddr("10.66.0.1"),
		ListenPort: uint16(wgPort),
	})
	if err != nil {
		t.Fatalf("wireguard.NewServer: %v", err)
	}
	defer wgSrv.Close()

	clientPriv, clientPub := genKeypair(t)
	peerCfg, err := wgSrv.RegisterPeer(clientPub)
	if err != nil {
		t.Fatalf("RegisterPeer: %v", err)
	}

	ln, err := wgSrv.ListenTCP(8090)
	if err != nil {
		t.Fatalf("ListenTCP: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	backend := &http.Server{Handler: mux}
	go backend.Serve(ln)
	defer backend.Close()

	conn := New(BackendConfig{
		Name: "remote",
		Mode: ModeRemote,
		Remote: RemoteConfig{
			ServerPublicKey:  peerCfg.ServerPublicKey,
			ServerEndpoint:   peerCfg.Endpoint, // "127.0.0.1:<wg listen port>", issued by RegisterPeer
			ClientPrivateKey: clientPriv,
			ClientAddress:    netip.MustParseAddr(peerCfg.ClientAddress),
			AllowedIPs:       peerCfg.AllowedIPs,
			RemoteAddr:       "10.66.0.1:8090",
		},
	})
	defer conn.Disconnect()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := conn.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if got := conn.Status(); got != StatusConnected {
		t.Errorf("Status() = %v, want Connected", got)
	}
	if !conn.Client().Healthy(context.Background()) {
		t.Error("Client().Healthy() = false, want true")
	}
}

// TestConnection_RemoteMode_TunnelDropReconnects covers 05-05's "WG
// reconnect: tunnel drops and re-establishes, client recovers" -- the
// remote-mode counterpart to TestConnection_Reconnection (which only
// exercises local mode's kill-the-subprocess path). Closing wgClient
// directly (an unexported-field access valid because this test file is in
// package connection, same as TestConnection_Reconnection's identical
// access to conn.cmd) simulates the tunnel dying under the connection
// without tearing down the Connection itself, the same way a real network
// interruption would: the next health check's Healthy() call fails against
// the now-dead tunnel, driving reconnect() to tear down and dial a
// brand-new WireGuard tunnel from the same peer config.
func TestConnection_RemoteMode_TunnelDropReconnects(t *testing.T) {
	wgPort, err := freePort()
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	wgSrv, err := wireguard.NewServer(context.Background(), wireguard.ServerConfig{
		Address:    netip.MustParseAddr("10.68.0.1"),
		ListenPort: uint16(wgPort),
	})
	if err != nil {
		t.Fatalf("wireguard.NewServer: %v", err)
	}
	defer wgSrv.Close()

	clientPriv, clientPub := genKeypair(t)
	peerCfg, err := wgSrv.RegisterPeer(clientPub)
	if err != nil {
		t.Fatalf("RegisterPeer: %v", err)
	}

	ln, err := wgSrv.ListenTCP(8090)
	if err != nil {
		t.Fatalf("ListenTCP: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	backend := &http.Server{Handler: mux}
	go backend.Serve(ln)
	defer backend.Close()

	var mu sync.Mutex
	var statuses []Status
	conn := New(BackendConfig{
		Name: "remote-drop-test",
		Mode: ModeRemote,
		Remote: RemoteConfig{
			ServerPublicKey:  peerCfg.ServerPublicKey,
			ServerEndpoint:   peerCfg.Endpoint,
			ClientPrivateKey: clientPriv,
			ClientAddress:    netip.MustParseAddr(peerCfg.ClientAddress),
			AllowedIPs:       peerCfg.AllowedIPs,
			RemoteAddr:       "10.68.0.1:8090",
		},
		HealthInterval: 200 * time.Millisecond,
		OnStatusChange: func(s Status) {
			mu.Lock()
			statuses = append(statuses, s)
			mu.Unlock()
		},
	})
	defer conn.Disconnect()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := conn.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Simulate the tunnel dropping: close it out from under the Connection
	// without going through Disconnect/teardown.
	conn.mu.Lock()
	wg := conn.wgClient
	conn.mu.Unlock()
	droppedAt := time.Now()
	if err := wg.Close(); err != nil {
		t.Fatalf("close wgClient: %v", err)
	}

	// Same history-based wait as TestConnection_Reconnection: Status()
	// alone can't distinguish "still holding the pre-drop Connected value"
	// from "genuinely reconnected," since both read as StatusConnected.
	deadline := droppedAt.Add(10 * time.Second)
	var gotStatuses []Status
	for {
		mu.Lock()
		gotStatuses = append([]Status(nil), statuses...)
		mu.Unlock()

		sawReconnecting := false
		reconnectedAfter := false
		for _, s := range gotStatuses {
			if s == StatusReconnecting {
				sawReconnecting = true
			} else if sawReconnecting && s == StatusConnected {
				reconnectedAfter = true
			}
		}
		if sawReconnecting && reconnectedAfter {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("did not observe Reconnecting followed by Connected within 10s of the tunnel drop; status history: %v (current: %v)", gotStatuses, conn.Status())
		}
		time.Sleep(20 * time.Millisecond)
	}

	// The reconnected client must actually work (new tunnel, healthy).
	if !conn.Client().Healthy(context.Background()) {
		t.Error("Client().Healthy() after reconnect = false, want true")
	}
}

// TestManager_MultipleTunnelsSimultaneously covers "Multiple tunnels: can
// manage N backends simultaneously".
func TestManager_MultipleTunnelsSimultaneously(t *testing.T) {
	bin := buildServerBinary(t)
	mgr := NewManager()
	defer mgr.DisconnectAll()

	const n = 3
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for i := 0; i < n; i++ {
		cfg := BackendConfig{
			Name:  fmt.Sprintf("backend-%d", i),
			Mode:  ModeLocal,
			Local: LocalConfig{ServerBinaryPath: bin, Root: t.TempDir(), StartupTimeout: 10 * time.Second},
		}
		if err := mgr.Connect(ctx, cfg); err != nil {
			t.Fatalf("Connect backend-%d: %v", i, err)
		}
	}

	if got := len(mgr.Names()); got != n {
		t.Fatalf("Names() has %d entries, want %d", got, n)
	}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("backend-%d", i)
		c, ok := mgr.Get(name)
		if !ok {
			t.Fatalf("Get(%q) not found", name)
		}
		if c.Status() != StatusConnected {
			t.Errorf("%s: Status() = %v, want Connected", name, c.Status())
		}
		if !c.Client().Healthy(ctx) {
			t.Errorf("%s: Healthy() = false", name)
		}
	}

	mgr.Disconnect("backend-1")
	if _, ok := mgr.Get("backend-1"); ok {
		t.Error("backend-1 still present after Disconnect")
	}
	if got := len(mgr.Names()); got != n-1 {
		t.Errorf("Names() has %d entries after disconnecting one, want %d", got, n-1)
	}
	// The other two must be unaffected by disconnecting one.
	for _, name := range []string{"backend-0", "backend-2"} {
		c, ok := mgr.Get(name)
		if !ok || c.Status() != StatusConnected {
			t.Errorf("%s: expected still Connected after an unrelated Disconnect", name)
		}
	}
}

// TestConnection_Reconnection covers "Reconnection: network change triggers
// reconnect within 5s". No OS-level network-change event exists to
// trigger in a test environment (see healthLoop's doc comment on why this
// package doesn't listen for one directly); killing the backend process
// out from under an established connection produces the same observable
// symptom this package actually reacts to -- the next health check
// failing -- so it's what's exercised here.
func TestConnection_Reconnection(t *testing.T) {
	bin := buildServerBinary(t)

	var mu sync.Mutex
	var statuses []Status
	conn := New(BackendConfig{
		Name:  "reconnect-test",
		Mode:  ModeLocal,
		Local: LocalConfig{ServerBinaryPath: bin, Root: t.TempDir(), StartupTimeout: 10 * time.Second},
		// A short interval so the failure is detected quickly -- this is
		// the mechanism, not a shortcut around it: DefaultHealthInterval
		// (1s) would still land well inside the 5s budget, this just makes
		// the test itself faster.
		HealthInterval: 200 * time.Millisecond,
		OnStatusChange: func(s Status) {
			mu.Lock()
			statuses = append(statuses, s)
			mu.Unlock()
		},
	})
	defer conn.Disconnect()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := conn.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Simulate a network/process failure: kill the spawned server directly
	// (an unexported-field access valid because this test file is in
	// package connection).
	conn.mu.Lock()
	proc := conn.cmd.Process
	conn.mu.Unlock()
	killedAt := time.Now()
	if err := proc.Kill(); err != nil {
		t.Fatalf("kill backend process: %v", err)
	}

	// Wait for reconnection: the status history must show Reconnecting
	// (proving the health loop actually detected the kill, not that Status
	// merely still read Connected from before it ever noticed), followed
	// by Connected again, all within 5s of the kill. Polling Status()
	// alone (without checking history) can't distinguish "still holding
	// the pre-kill Connected value" from "genuinely reconnected" -- both
	// read as StatusConnected -- so this reads the full history each poll.
	deadline := killedAt.Add(5 * time.Second)
	var gotStatuses []Status
	for {
		mu.Lock()
		gotStatuses = append([]Status(nil), statuses...)
		mu.Unlock()

		sawReconnecting := false
		reconnectedAfter := false
		for _, s := range gotStatuses {
			if s == StatusReconnecting {
				sawReconnecting = true
			} else if sawReconnecting && s == StatusConnected {
				reconnectedAfter = true
			}
		}
		if sawReconnecting && reconnectedAfter {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("did not observe Reconnecting followed by Connected within 5s of the kill; status history: %v (current: %v)", gotStatuses, conn.Status())
		}
		time.Sleep(20 * time.Millisecond)
	}
	elapsed := time.Since(killedAt)
	if elapsed > 5*time.Second {
		t.Errorf("reconnection took %v, want <= 5s", elapsed)
	}

	// The reconnected client must actually work (new subprocess, new
	// token) -- not just report Connected while broken.
	if !conn.Client().Healthy(context.Background()) {
		t.Error("reconnected Client().Healthy() = false")
	}
}
