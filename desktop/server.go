package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"

	"path/filepath"

	"gophermind/internal/config"
	"gophermind/internal/prompt"
	"gophermind/internal/serve"
)

// embeddedServer is a running instance of internal/serve, bound to a
// loopback, kernel-assigned port with a per-launch random bearer token. It is
// the thing app.go's Endpoint() binding describes to the frontend.
type embeddedServer struct {
	// BaseURL is "http://127.0.0.1:<port>", the address the frontend should
	// send every fetch/EventSource request to.
	BaseURL string
	// Token is the bearer token every request must present as
	// "Authorization: Bearer <token>". This is the ROUTER's token, not any
	// backend's: see newRouter for why the two are kept apart. Generated
	// fresh per launch; never persisted, never logged.
	Token string

	// backends is what the router routes to. The local embedded server is
	// always present and is the default.
	backends *backendRegistry

	cancel context.CancelFunc
	done   chan error
}

// newToken returns a 32-byte cryptographically random token, hex-encoded (64
// hex characters). It is generated fresh every call, so two launches never
// share a token.
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// startEmbeddedServer builds serve.Deps from the environment (see deps.go),
// binds a loopback listener on an OS-assigned port, and starts serving in a
// background goroutine. It deliberately does this BEFORE the LLM client is
// built: the client's liveness probe can be slow or fail outright (an
// unreachable LAN endpoint, say), and none of that may block the window from
// getting a working embedded server. serve.Deps is wired through a
// clientHolder (see backend.go) so its closures look up the current client
// at call time rather than needing one up front; resolveLLMBackend fills the
// holder in, and falls back to a free provider, in a background goroutine
// started after the listener is already serving. The returned
// *embeddedServer stays valid until Shutdown is called. parent bounds the
// server's lifetime: cancelling parent (or calling Shutdown) triggers
// internal/serve's graceful shutdown.
func startEmbeddedServer(parent context.Context) (*embeddedServer, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(parent)

	// The holder is created before the registry so the humanize tool can look
	// the client up at call time. The registry itself is still built before
	// the LLM resolves, which is what keeps a slow or unreachable endpoint
	// from blocking the window.
	holder := &clientHolder{}
	reg := newToolRegistry(cfg, holder.Get)

	pb, err := prompt.NewBuilder()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("prompt template: %w", err)
	}
	basePrompt := pb.Build()

	token, err := newToken()
	if err != nil {
		cancel()
		return nil, err
	}

	status := &backendStatus{}

	mux, err := serve.NewMux(newServeDeps(holder.Get, holder.Profile, holder.Set, reg, cfg, basePrompt), serve.Options{Token: token})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("build mux: %w", err)
	}
	mux.HandleFunc("/backend-status", backendStatusHandler(token, status))

	// Loopback only, kernel-assigned port: no other host can reach this
	// listener, and no fixed port can collide with another instance.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		cancel()
		return nil, fmt.Errorf("listen: %w", err)
	}

	// The embedded server keeps its own token and stays on loopback. What the
	// frontend is given is the router in front of it, not this.
	go func() { _ = serve.Serve(ctx, ln, mux) }()

	// Register the embedded server as the "local" backend, and therefore the
	// default: it is the one that works with no network at all, so it is what
	// an unprefixed request routes to and what the app falls back to.
	backends := &backendRegistry{}
	if err := backends.Add(Backend{
		Name:      "local",
		Kind:      BackendLocal,
		BaseURL:   "http://" + ln.Addr().String(),
		Token:     token,
		Available: true,
	}); err != nil {
		cancel()
		return nil, err
	}

	// Remote backends from ~/.gophermind/backends.json, registered after
	// local so local stays the default: the app must still work with no
	// network, and an unprefixed request has to go somewhere that does.
	//
	// A broken config fails startup rather than starting without the
	// backends it names. Silently dropping them would leave the user
	// wondering where their remote sessions went, and the failure mode of a
	// missing backend is worse than the failure mode of a loud error.
	cfgDir, err := config.Dir()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("config dir: %w", err)
	}
	remotes, err := loadBackendConfig(filepath.Join(cfgDir, backendConfigFile))
	if err != nil {
		cancel()
		return nil, err
	}
	for _, b := range remotes {
		if err := backends.Add(b); err != nil {
			cancel()
			return nil, err
		}
	}

	// The router gets its OWN front-door token, never the embedded server's.
	// The frontend holds only this one, so a backend's credential is not
	// present in the WebView even for the local backend, and the same code
	// path covers a remote backend whose token would authorize shell
	// execution on another machine.
	frontToken, err := newToken()
	if err != nil {
		cancel()
		return nil, err
	}
	router, err := newRouter(backends, frontToken)
	if err != nil {
		cancel()
		return nil, err
	}
	rln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		cancel()
		return nil, fmt.Errorf("listen (router): %w", err)
	}

	s := &embeddedServer{
		BaseURL:  "http://" + rln.Addr().String(),
		Token:    frontToken,
		backends: backends,
		cancel:   cancel,
		done:     make(chan error, 1),
	}
	// The Wails WebView serves the frontend from its own origin, so every
	// call it makes here is cross-origin. withCORS allows exactly that origin.
	go func() { s.done <- serve.Serve(ctx, rln, withCORS(router)) }()
	go resolveLLMBackend(ctx, cfg, holder, status)
	return s, nil
}

// Shutdown cancels the server's context, which triggers internal/serve.Serve's
// graceful shutdown, and waits for it to finish. Safe to call once; a nil
// receiver check is not needed by callers since startEmbeddedServer never
// returns a nil *embeddedServer alongside a nil error.
func (s *embeddedServer) Shutdown() error {
	s.cancel()
	return <-s.done
}
