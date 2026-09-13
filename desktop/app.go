package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"io"
	"os"
	"strings"
	"sync"
)

// App is the Wails-bound application struct. It exposes exactly two methods to
// the frontend: Endpoint, and PickFolder.
//
// The design decision the first one establishes still holds: the frontend
// speaks HTTP to the server for everything, never a Wails binding per
// operation. PickFolder does not weaken that. It reaches an OS capability the
// WebView cannot reach by itself, which is what a native bridge is for, and
// carries no business logic: the path it returns goes to the server over HTTP
// like every other setting, and the server decides whether to accept it.
type App struct {
	// mu guards ctx and server. startup assigns both on the Wails startup
	// goroutine while Endpoint and PickFolder read them from the frontend's
	// binding calls. The frontend mounts and calls Endpoint before startup
	// finishes, so this is a real race, not a theoretical one.
	mu     sync.RWMutex
	ctx    context.Context
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
	// Under the lock, like server: PickFolder reads ctx from a binding call
	// that can arrive before this returns.
	a.mu.Lock()
	a.ctx = ctx
	a.mu.Unlock()

	server, err := startEmbeddedServer(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "gophermind desktop: embedded server failed to start:", err)
		os.Exit(1)
	}
	a.mu.Lock()
	a.server = server
	a.mu.Unlock()
}

// NewProjectEvent is the event name the frontend listens on. The menu item
// carries the chosen brief's path in it.
const NewProjectEvent = "new-project"

// maxBriefBytes caps how much of a chosen brief is read. A brief is a
// document a person wrote; anything past this is not one, and pasting it into
// a prompt would crowd out the conversation it is supposed to start.
const maxBriefBytes = 256 << 10

// NewProjectBrief is what the menu handler sends the frontend.
type NewProjectBrief struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// newProject is the File > New Project menu handler.
//
// The dialog is opened here, in Go, and the result travels to the frontend as
// an event rather than through a binding. A menu item is already native code,
// so doing the file pick here costs nothing and keeps the bridge at two
// methods instead of three.
//
// The file is READ here too, and the contents are what travel, not just the
// path. Sending a path does not work: the agent's file tools are contained to
// the project root by safety.SafeJoin, and a brief almost never lives inside
// the project it describes. Picking one in ~/Downloads and handing the agent
// the path gets "path escapes repo root", which is containment doing its job.
//
// Reading it here is not a hole in that. The user just chose this exact file
// in a native dialog, which is what authorisation looks like; containment
// still covers every file they did not choose.
//
// Cancelling emits nothing: there is no project to start, and a "you
// cancelled" event would only give the frontend something to ignore.
func (a *App) newProject(_ *menu.CallbackData) {
	a.mu.RLock()
	ctx := a.ctx
	a.mu.RUnlock()
	if ctx == nil {
		return
	}
	path, err := runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{
		Title: "Choose a project brief",
		Filters: []runtime.FileFilter{
			{DisplayName: "Briefs (*.md, *.txt)", Pattern: "*.md;*.txt"},
			{DisplayName: "All files", Pattern: "*"},
		},
	})
	if err != nil || strings.TrimSpace(path) == "" {
		return
	}

	body, err := readBrief(path)
	if err != nil {
		// Still emit, so the window says why nothing happened rather than the
		// menu item appearing to do nothing at all.
		body = "(could not read this file: " + err.Error() + ")"
	}
	runtime.EventsEmit(ctx, NewProjectEvent, NewProjectBrief{Path: path, Content: body})
}

// readBrief reads a chosen brief, truncated at maxBriefBytes. Truncating
// rather than refusing is deliberate: a very long brief is still worth
// starting from, and losing its tail beats starting nothing.
func readBrief(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, maxBriefBytes))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// PickFolder opens the system's directory chooser and returns the chosen
// path, or "" if the user cancelled.
//
// Cancelling is not an error: Wails returns an empty path and the caller
// treats that as "leave it as it was". The path is not validated here; the
// server does that when it is set, so there is one place that decides what a
// usable root is rather than two that can disagree.
func (a *App) PickFolder() (string, error) {
	a.mu.RLock()
	ctx := a.ctx
	a.mu.RUnlock()
	if ctx == nil {
		return "", errors.New("window is still starting")
	}
	return runtime.OpenDirectoryDialog(ctx, runtime.OpenDialogOptions{
		Title: "Choose a folder for this session",
	})
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
