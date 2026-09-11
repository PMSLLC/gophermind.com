package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
)

// App is the Wails-bound application struct. It exposes exactly one method to
// the frontend, Endpoint, per the desktop app's central design decision: the
// frontend always speaks HTTP to the embedded (or, in a later task, remote)
// server, never Wails bindings for individual operations.
type App struct {
	ctx context.Context

	// mu guards server, which startup assigns on the Wails startup goroutine
	// while Endpoint reads it from the frontend's binding call. The frontend
	// mounts and calls Endpoint before startup finishes, so this is a real
	// race, not a theoretical one.
	mu     sync.RWMutex
	server *embeddedServer
}

// NewApp constructs an unstarted App. The embedded server is started in
// startup, once Wails has a runtime context to bind lifecycle to.
func NewApp() *App {
	return &App{}
}

// startup is called by Wails once the application window is ready. It starts
// the embedded internal/serve instance; a failure here is fatal and loud
// (printed to stderr, then the process exits) rather than silent, per the
// desktop app spec's error-handling table. Building an in-window error screen
// for this case is left to a later task.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	server, err := startEmbeddedServer(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "gophermind desktop: embedded server failed to start:", err)
		os.Exit(1)
	}
	a.mu.Lock()
	a.server = server
	a.mu.Unlock()
}

// shutdown is called by Wails when the application is closing. It shuts the
// embedded server down gracefully so a turn in flight is not orphaned and the
// loopback listener is released.
func (a *App) shutdown(ctx context.Context) {
	if a.server != nil {
		if err := a.server.Shutdown(); err != nil {
			fmt.Fprintln(os.Stderr, "gophermind desktop: server shutdown:", err)
		}
	}
}

// EndpointInfo is what Endpoint returns to the frontend: the embedded
// server's base URL and bearer token. The frontend uses these for every
// fetch/EventSource call; nothing else is bound from Go to JavaScript.
type EndpointInfo struct {
	BaseURL string `json:"baseURL"`
	Token   string `json:"token"`
}

// Endpoint returns the embedded server's base URL and bearer token. This is
// the ONLY method Wails binds to JavaScript (see main.go's Bind list), every
// other operation (creating a session, streaming a turn, listing models, ...)
// goes over HTTP to the address this returns, so embedded and remote modes
// share the exact same frontend code path.
func (a *App) Endpoint() (EndpointInfo, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.server == nil {
		// Returning a zero EndpointInfo here instead of an error is what made
		// the window show an opaque "Load failed": the frontend got an empty
		// base URL, fetched a relative path, and the WebView resolved it
		// against its own wails:// origin. An explicit error lets the frontend
		// wait for startup rather than misreport it as a network failure.
		return EndpointInfo{}, errors.New("embedded server is still starting")
	}
	return EndpointInfo{BaseURL: a.server.BaseURL, Token: a.server.Token}, nil
}
