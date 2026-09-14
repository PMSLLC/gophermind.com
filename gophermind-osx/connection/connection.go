package connection

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"gophermind/gophermind-lib/wireguard"
	"gophermind/gophermind-osx/client"
)

// Connection manages one backend's lifecycle: connecting (local subprocess
// or remote WG tunnel), health-checking, and automatic reconnection.
type Connection struct {
	cfg BackendConfig

	mu       sync.Mutex
	status   Status
	client   *client.Client
	cmd      *exec.Cmd         // local mode only
	wgClient *wireguard.Client // remote mode only

	cancel context.CancelFunc // stops the health-check loop; set by Connect
	done   chan struct{}      // closed when the health-check loop exits
}

// New returns a Connection for cfg. It does not connect -- call Connect.
func New(cfg BackendConfig) *Connection {
	if cfg.HealthInterval <= 0 {
		cfg.HealthInterval = DefaultHealthInterval
	}
	return &Connection{cfg: cfg, status: StatusDisconnected}
}

// Status returns the connection's current state.
func (c *Connection) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

// Client returns the client.Client for this connection, or nil if not
// currently connected. Safe to call from any goroutine; the returned
// *client.Client may become stale if a reconnect replaces it immediately
// after -- callers doing more than one call in a row should re-fetch
// Client() rather than caching it across a reconnect boundary.
func (c *Connection) Client() *client.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.client
}

func (c *Connection) setStatus(s Status) {
	c.mu.Lock()
	c.status = s
	cb := c.cfg.OnStatusChange
	c.mu.Unlock()
	if cb != nil {
		cb(s)
	}
}

// Connect establishes the connection (spawning gophermind-server for local
// mode, or dialing the WG tunnel for remote mode), waits for it to become
// healthy, and starts the background health-check/reconnect loop. Returns
// once the initial connection succeeds or ctx is done/times out.
func (c *Connection) Connect(ctx context.Context) error {
	c.setStatus(StatusConnecting)
	if err := c.connectOnce(ctx); err != nil {
		c.setStatus(StatusDisconnected)
		return err
	}
	c.setStatus(StatusConnected)

	loopCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	c.mu.Lock()
	c.cancel = cancel
	c.done = done
	c.mu.Unlock()
	go c.healthLoop(loopCtx, done)
	return nil
}

// connectOnce performs exactly one connection attempt (no retry, no health
// loop) and blocks until the backend is confirmed healthy or ctx/its own
// startup timeout expires. Used by both Connect and the reconnect path in
// healthLoop, so both go through identical setup logic.
func (c *Connection) connectOnce(ctx context.Context) error {
	switch c.cfg.Mode {
	case ModeLocal:
		return c.connectLocal(ctx)
	case ModeRemote:
		return c.connectRemote(ctx)
	default:
		return fmt.Errorf("unknown connection mode %d", c.cfg.Mode)
	}
}

func (c *Connection) connectLocal(ctx context.Context) error {
	lc := c.cfg.Local
	if lc.ServerBinaryPath == "" {
		return fmt.Errorf("local mode: ServerBinaryPath is required")
	}
	port := lc.Port
	if port == 0 {
		p, err := freePort()
		if err != nil {
			return fmt.Errorf("pick a free port: %w", err)
		}
		port = p
	}
	token, err := randomToken()
	if err != nil {
		return fmt.Errorf("generate token: %w", err)
	}

	// --wg-interface "" disables gophermind-server's own internal WireGuard
	// server: local mode is same-machine, same-user, so a tunnel adds
	// nothing (see ModeLocal's doc comment) -- and gophermind-server's
	// --wg-interface flag defaults to "wg0" (non-empty) when omitted, which
	// would make every spawned instance try to bind the same default WG
	// port (51820) and collide, breaking exactly the "manage N backends
	// simultaneously" and repeated-reconnect cases this package exists for.
	args := []string{"--port", fmt.Sprintf("%d", port), "--token", token, "--wg-interface", ""}
	if lc.Root != "" {
		args = append(args, "--root", lc.Root)
	}
	cmd := exec.Command(lc.ServerBinaryPath, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start gophermind-server: %w", err)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	cl := client.New(client.Config{BaseURL: baseURL, Token: token})

	startupTimeout := lc.StartupTimeout
	if startupTimeout <= 0 {
		startupTimeout = DefaultStartupTimeout
	}
	if err := waitHealthy(ctx, cl, startupTimeout); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("gophermind-server did not become healthy: %w", err)
	}

	c.mu.Lock()
	c.client = cl
	c.cmd = cmd
	c.mu.Unlock()
	return nil
}

func (c *Connection) connectRemote(ctx context.Context) error {
	rc := c.cfg.Remote
	if rc.RemoteAddr == "" {
		return fmt.Errorf("remote mode: RemoteAddr is required")
	}

	wgClient, err := wireguard.NewClient(context.Background(), wireguard.ClientConfig{
		ServerPublicKey: rc.ServerPublicKey,
		ServerEndpoint:  rc.ServerEndpoint,
		PrivateKey:      rc.ClientPrivateKey,
		Address:         rc.ClientAddress,
		AllowedIPs:      rc.AllowedIPs,
	})
	if err != nil {
		return fmt.Errorf("establish WireGuard tunnel: %w", err)
	}
	// wgClient is given context.Background(), not ctx, for the same reason
	// gophermind-server's own startWireGuard does (see gophermind-server/
	// wireguard.go): the tunnel's lifetime is governed by this
	// Connection's own Disconnect/reconnect logic, not by ctx, which may
	// be a short-lived per-call context.

	baseURL := "http://" + rc.RemoteAddr
	cl := client.New(client.Config{
		BaseURL:   baseURL,
		Transport: wgClient.HTTPClient().Transport,
	})

	startupTimeout := DefaultStartupTimeout
	if err := waitHealthy(ctx, cl, startupTimeout); err != nil {
		wgClient.Close()
		return fmt.Errorf("tunnel established but backend did not become healthy: %w", err)
	}

	c.mu.Lock()
	c.client = cl
	c.wgClient = wgClient
	c.mu.Unlock()
	return nil
}

// waitHealthy polls cl.Healthy until it returns true, ctx is done, or
// timeout elapses.
func waitHealthy(ctx context.Context, cl *client.Client, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if cl.Healthy(ctx) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("not healthy after %s", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// healthLoop runs until ctx is done, periodically checking the connection's
// health and triggering a reconnect when it fails. This is also the
// package's answer to "reconnection on network change": a network change
// (WiFi switch, VPN toggle, sleep/wake) manifests here as the next health
// check failing, not as a distinct OS-level event this package listens
// for separately -- see BackendConfig's doc comment for why.
//
// done is the channel to close on exit, passed explicitly (captured once
// by Connect at goroutine-launch time) rather than read back from c.done:
// Disconnect clears c.done to nil under c.mu before this goroutine's defer
// runs, so reading c.done here raced with that and occasionally closed a
// nil channel, panicking.
func (c *Connection) healthLoop(ctx context.Context, done chan struct{}) {
	defer close(done)

	ticker := time.NewTicker(c.cfg.HealthInterval)
	defer ticker.Stop()

	failures := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cl := c.Client()
			if cl == nil || !cl.Healthy(ctx) {
				failures++
			} else {
				failures = 0
				continue
			}
			if failures >= maxConsecutiveHealthFailures {
				failures = 0
				c.reconnect(ctx)
			}
		}
	}
}

// reconnect tears down the current connection and establishes a fresh one,
// reporting StatusReconnecting for the duration. Failures are swallowed
// (not returned) since this runs from the unattended health-check loop --
// Status staying at StatusReconnecting (rather than settling back to
// StatusConnected) is itself the signal a caller polling Status() sees; the
// loop simply tries again on its next tick.
func (c *Connection) reconnect(ctx context.Context) {
	c.setStatus(StatusReconnecting)
	c.teardown()
	if err := c.connectOnce(ctx); err != nil {
		return // stay in StatusReconnecting; the next health tick retries
	}
	c.setStatus(StatusConnected)
}

// teardown releases whatever connectLocal/connectRemote acquired, without
// touching the health-check loop (the caller -- reconnect or Disconnect --
// controls that separately).
func (c *Connection) teardown() {
	c.mu.Lock()
	cmd, wgClient := c.cmd, c.wgClient
	c.cmd, c.wgClient, c.client = nil, nil, nil
	c.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	if wgClient != nil {
		wgClient.Close()
	}
}

// Disconnect stops the health-check loop and tears down the connection.
// Safe to call on an already-disconnected Connection.
func (c *Connection) Disconnect() {
	c.setStatus(StatusDisconnecting)

	c.mu.Lock()
	cancel, done := c.cancel, c.done
	c.cancel, c.done = nil, nil
	c.mu.Unlock()

	if cancel != nil {
		cancel()
		<-done
	}
	c.teardown()
	c.setStatus(StatusDisconnected)
}
