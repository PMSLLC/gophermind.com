package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"

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
	// Token is the bearer token every request (other than /healthz, /readyz,
	// /metrics) must present as "Authorization: Bearer <token>". Generated
	// fresh per launch; never persisted, never logged.
	Token string

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

	reg := newToolRegistry(cfg)

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

	holder := &clientHolder{}
	status := &backendStatus{}

	mux, err := serve.NewMux(newServeDeps(holder.Get, reg, cfg, basePrompt), serve.Options{Token: token})
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

	s := &embeddedServer{
		BaseURL: "http://" + ln.Addr().String(),
		Token:   token,
		cancel:  cancel,
		done:    make(chan error, 1),
	}
	go func() { s.done <- serve.Serve(ctx, ln, mux) }()
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
