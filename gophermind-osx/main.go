package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"gophermind/gophermind-lib/llm"
	"gophermind/gophermind-lib/phaseflow"
	"gophermind/gophermind-osx/client"
	"gophermind/gophermind-osx/connection"
	appui "gophermind/gophermind-osx/ui"
)

// newSessionOptions builds the options a session should be created with,
// from what the Sessions panel currently holds.
//
// Root is the part that matters and the part that was missing: a session
// created with an empty Root runs at the server's own --root, which is the
// user's home directory. Every relative path the agent is given then
// resolves against $HOME, so reading a brief at docs/briefs/x.md fails with
// "/Users/<user>/docs/briefs/x.md: no such file or directory" while the file
// sits perfectly well inside the project the user chose.
//
// fallbackRoot is used when the panel has no folder chosen. Callers that know
// which file the work is about pass a root derived from it -- the breakdown
// passes projectRootFor(brief) -- so the common case needs no extra click.
func newSessionOptions(sessions *appui.SessionListState, fallbackRoot string) client.CreateSessionOptions {
	opts := client.CreateSessionOptions{
		Mode: sessions.NewMode(),
		Root: sessions.NewRoot(),
	}
	if opts.Root == "" {
		opts.Root = fallbackRoot
	}
	return opts
}

// projectRootFor guesses which project a brief belongs to, for use when the
// Sessions panel has no folder chosen.
//
// The nearest ancestor holding a .git, because that is what "the project"
// means in practice and it is what the brief's own relative paths are
// written against. The brief's directory is the last resort: it is wrong for
// a repo checkout (the seed prompt writes .planning/ into the root, which
// would land inside docs/briefs/) but it is still closer than $HOME.
func projectRootFor(briefPath string) string {
	dir := filepath.Dir(briefPath)
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return dir
		}
		d = parent
	}
}

// liveClient returns the client of the first connected backend, or an error
// naming what the user has to do about it. The send path in main inlines
// this same search against chat.Transcript; callers that are not the
// transcript (the pipeline panel) need it as a value they can return.
func liveClient(connMgr *connection.Manager) (*client.Client, error) {
	// Sorted, with "local" first, because Manager.Names ranges over a map
	// and Go randomises that order. With more than one backend connected --
	// the app registers "local" plus every saved profile -- each call landed
	// on a different one at random, so the same click could reach the local
	// server once and a remote one the next time, which is where
	// "status 401: unauthorized" came from: a token minted for one server
	// presented to another.
	//
	// "local" wins because the app spawns it and holds its token by
	// construction, so it is the one backend that cannot be misauthenticated.
	// Choosing a different one is a decision the user should make explicitly;
	// there is no active-backend selector yet, and guessing is what this
	// replaces.
	names := connMgr.Names()
	sort.Slice(names, func(i, j int) bool {
		if (names[i] == "local") != (names[j] == "local") {
			return names[i] == "local"
		}
		return names[i] < names[j]
	})
	for _, name := range names {
		c, ok := connMgr.Get(name)
		if !ok || c.Status() != connection.StatusConnected {
			continue
		}
		if cl := c.Client(); cl != nil {
			return cl, nil
		}
		return nil, fmt.Errorf("backend %q is connected but its client is unavailable (reconnecting?)", name)
	}
	return nil, fmt.Errorf("not connected to a backend: open Settings (gear icon) and click Connect")
}

// The dock icon and "clicking it focuses the window" (.planning/tasks/
// 04-08.json) need no code here: a normal windowed macOS app already gets
// a dock icon and standard reactivation-on-click behavior from AppKit for
// free, the same way libui-ng's native controls already follow the
// system's light/dark appearance for free (see NewApp's doc comment on
// Toggle Dark Mode). Both are properties of being a real windowed app on
// this platform, not something gophermind-osx implements.
func main() {
	// Cocoa requires every NSWindow/NSStatusItem to be created on the
	// same OS thread the process started on. Without this, a blocking
	// call that parks the main goroutine (e.g. the HTTP health checks in
	// connMgr.Connect below) can resume it on a different OS thread, and
	// newStatusItem's NSStatusBar call later in this function crashes
	// with "NSWindow should only be instantiated on the main thread!".
	runtime.LockOSThread()

	// One-time move of any local state from before it was consolidated
	// under gophermind-lib/config.Dir() (~/.gophermind) -- see
	// localstate.go. Must run before anything below reads window state,
	// panel state, cache/history settings, or the backend list.
	migrateLegacyOSXState()

	app, err := NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gophermind-osx:", err)
		os.Exit(1)
	}

	// Window state (.planning/tasks/04-08.json): restore before Show, save
	// after Run returns (the window still exists then; Close destroys it).
	windowState := loadWindowState()
	app.SetContentSize(windowState.Width, windowState.Height)
	app.SetPosition(windowState.X, windowState.Y)

	// Connection manager: spawns gophermind-server as a local subprocess.
	connMgr := connection.NewManager()

	// sendTurn uses the active connection's client to stream the task.
	// The closure captures `chat` before its own declaration finishes: it
	// only runs later, from a real button click, by which point the
	// assignment below has long since completed.
	var chat *ChatWindow
	chat = NewChatWindow(app, func(text string) {
		cl, err := liveClient(connMgr)
		if err != nil {
			chat.Transcript.AddUserMessage(text)
			chat.Transcript.AddSystem(err.Error())
			return
		}
		// Continue the session already in play rather than starting a new
		// one per message. Every send used to call CreateSession, so each
		// message landed in a fresh session with no history: answering a
		// question the agent had just asked -- "yes" to a breakdown's first
		// question -- reached a model that had never seen the question.
		ctx := context.Background()
		sessionID := chat.CurrentSession
		if sessionID == "" {
			// The options carry the folder and mode chosen in the Sessions
			// panel; without them the turn runs at the server's root.
			id, cerr := cl.CreateSession(ctx, newSessionOptions(chat.Sessions, ""))
			if cerr != nil {
				chat.Transcript.AddUserMessage(text)
				chat.Transcript.AddSystem("error creating session: " + cerr.Error())
				return
			}
			sessionID = id
			chat.CurrentSession = id
		}
		chat.RunTurn(ctx, sessionID, text, func(ctx context.Context, task string) (*client.EventStream, error) {
			return cl.Stream(ctx, sessionID, task)
		})
	})

	// Wire the settings panel's connect/disconnect to the manager.
	chat.Backends.Add(appui.BackendProfile{Name: "local", Mode: "local", ServerURL: "auto"})
	chat.settingsUI.connectFunc = func(ctx context.Context, p appui.BackendProfile) error {
		// Disconnect first if already connected (reconnect semantics).
		connMgr.Disconnect(p.Name)

		var cfg connection.BackendConfig
		cfg.Name = p.Name
		cfg.OnStatusChange = func(s connection.Status) {
			chat.Backends.SetStatus(p.Name, s.String())
		}

		// Determine the connection mode. A backend with a real server URL
		// (anything other than "auto", "local", or empty) connects directly
		// over HTTP, regardless of the mode radio — this makes it work even
		// if the user forgot to select "remote" in the settings panel.
		isDirect := p.Mode == "remote" ||
			(p.ServerURL != "" && p.ServerURL != "auto" && p.ServerURL != "local")

		if isDirect {
			if p.ServerURL == "" {
				return fmt.Errorf("remote backend %q: no server URL configured", p.Name)
			}
			baseURL := p.ServerURL
			if len(baseURL) < 7 || baseURL[:7] != "http://" {
				baseURL = "http://" + baseURL
			}
			cfg.Mode = connection.ModeDirect
			cfg.Direct = connection.DirectConfig{BaseURL: baseURL, Token: p.Token}
		} else {
			binPath := findServerBinary()
			if binPath == "" {
				return fmt.Errorf("gophermind-server binary not found (set GOPHERMIND_SERVER or place it next to the app)")
			}
			cfg.Mode = connection.ModeLocal
			cfg.Local = connection.LocalConfig{
				ServerBinaryPath: binPath,
			}
		}
		return connMgr.Connect(ctx, cfg)
	}
	chat.settingsUI.disconnectFunc = func(name string) {
		connMgr.Disconnect(name)
	}

	// Connect on startup. If the local binary isn't found, don't show an
	// error — just a hint. The user can connect a remote backend from
	// Settings (gear icon) without needing a local binary.
	if err := chat.settingsUI.connectFunc(context.Background(), appui.BackendProfile{Name: "local", Mode: "local"}); err != nil {
		if strings.Contains(err.Error(), "binary not found") {
			chat.Transcript.AddSystem("No local server found. Open Settings (gear icon) to connect a remote backend.")
		} else {
			chat.Transcript.AddSystem("Connection failed: " + err.Error())
		}
	} else {
		chat.Transcript.AddSystem("Connected to local gophermind-server.")
	}

	// Let approvals actually resolve. The tracker is built in chatinput.go
	// with a no-op ApproveFunc, so until this runs an approve or deny click
	// updates the card and tells the server nothing, leaving the turn
	// blocked until the gate's five-minute timeout auto-denies it.
	chat.Approvals.SetApproveFunc(func(ctx context.Context, sessionID, approvalID string, approved bool) error {
		cl, err := liveClient(connMgr)
		if err != nil {
			return err
		}
		return cl.Approve(ctx, sessionID, approvalID, approved)
	})

	// Wire the Sessions panel to the live connection. Built in chatinput.go
	// with all five callbacks nil, so until this runs the dropdown is empty
	// and Resume, Rename, Delete and New Session do nothing at all.
	chat.sessionsUI.setLiveFuncs(
		func(ctx context.Context) ([]appui.SessionEntry, error) {
			cl, err := liveClient(connMgr)
			if err != nil {
				return nil, err
			}
			infos, err := cl.ListSessions(ctx)
			if err != nil {
				return nil, err
			}
			out := make([]appui.SessionEntry, 0, len(infos))
			for _, info := range infos {
				out = append(out, appui.SessionEntry{Info: info, Backend: "local"})
			}
			return out, nil
		},
		func(ctx context.Context, mode, root string) (string, error) {
			cl, err := liveClient(connMgr)
			if err != nil {
				return "", err
			}
			id, err := cl.CreateSession(ctx, client.CreateSessionOptions{Mode: mode, Root: root})
			if err != nil {
				return "", err
			}
			// A session the user just made is the one they mean to talk to.
			chat.CurrentSession = id
			return id, nil
		},
		func(ctx context.Context, id, name string) error {
			cl, err := liveClient(connMgr)
			if err != nil {
				return err
			}
			return cl.RenameSession(ctx, id, name)
		},
		func(ctx context.Context, id string) error {
			cl, err := liveClient(connMgr)
			if err != nil {
				return err
			}
			if err := cl.DeleteSession(ctx, id); err != nil {
				return err
			}
			if chat.CurrentSession == id {
				chat.CurrentSession = "" // do not keep talking to a deleted session
			}
			return nil
		},
		func(ctx context.Context, id string) (appui.SessionConfig, []llm.Message, error) {
			cl, err := liveClient(connMgr)
			if err != nil {
				return appui.SessionConfig{}, nil, err
			}
			cfg, err := cl.SessionConfig(ctx, id)
			if err != nil {
				return appui.SessionConfig{}, nil, err
			}
			raw, err := cl.SessionMessages(ctx, id)
			if err != nil {
				return appui.SessionConfig{}, nil, err
			}
			msgs := make([]llm.Message, 0, len(raw))
			for _, r := range raw {
				var m llm.Message
				if err := json.Unmarshal(r, &m); err != nil {
					continue // skip a frame we cannot read rather than losing the rest
				}
				msgs = append(msgs, m)
			}
			// Resuming means typed messages continue this session.
			chat.CurrentSession = id
			return appui.SessionConfig{Model: cfg.Model, Mode: cfg.Mode, Root: cfg.Root}, msgs, nil
		},
	)

	// Wire the pipeline panel to the live connection. It is built in
	// chatinput.go with both callbacks nil, because no connection exists
	// that early; until this runs, Start Breakdown has nothing to call.
	chat.pipelineUI.setLiveFuncs(
		func(ctx context.Context, briefPath, prompt string) (string, error) {
			cl, err := liveClient(connMgr)
			if err != nil {
				return "", err
			}
			// The brief's own directory is the fallback root, so a
			// breakdown works without first picking a folder: the seed
			// prompt talks about the brief and its siblings, and those
			// paths only resolve inside the project holding it.
			opts := newSessionOptions(chat.Sessions, projectRootFor(briefPath))
			sessionID, err := cl.CreateSession(ctx, opts)
			if err != nil {
				return "", fmt.Errorf("create session: %w", err)
			}
			// Typed replies continue the breakdown, which is the whole point
			// of an interview that asks one question at a time.
			chat.CurrentSession = sessionID
			// Stream the seed prompt through the transcript, the same way
			// a typed message runs, so the breakdown is visible while it
			// works rather than only landing in the pipeline view.
			chat.RunTurn(ctx, sessionID, prompt, func(ctx context.Context, task string) (*client.EventStream, error) {
				return cl.Stream(ctx, sessionID, task)
			})
			return sessionID, nil
		},
		func(ctx context.Context) ([]phaseflow.Task, error) {
			cl, err := liveClient(connMgr)
			if err != nil {
				// Not an error worth showing: with no backend the panel
				// just starts empty, which is its documented state, and
				// the user already gets a connect hint above.
				return nil, nil
			}
			tasks, _, err := cl.PipelineState(ctx)
			return tasks, err
		},
	)

	// Reconnect every other configured backend restored from
	// backends.json/the Keychain (see chatinput.go's NewChatWindow) --
	// persisting settings across restarts should also restore the
	// connections those settings describe, not just an inert profile
	// list the user would have to manually reconnect by hand every
	// launch. "local" is excluded: it already connected unconditionally
	// above and is never itself persisted (see main.go's own Backends.Add
	// call for it, versus settingspanel.go's doAddBackend/doRemoveBackend,
	// which are the only paths that write backends.json).
	for _, p := range chat.Backends.Profiles() {
		if p.Name == "local" {
			continue
		}
		if err := chat.settingsUI.connectFunc(context.Background(), p); err != nil {
			chat.Transcript.AddSystem(fmt.Sprintf("Could not reconnect backend %q: %s", p.Name, err))
		}
	}

	// Menu-bar (status bar) item listing every configured backend and its
	// live status, plus Show Window / Quit -- a separate surface from the
	// app's own File/Edit/View menu bar (see statusitem.go's top doc
	// comment). Created after every backend above is in place so its
	// first refresh already shows the full list, not just whatever was
	// added before this line happened to run.
	newStatusItem(chat.Backends, app.window)

	app.Show()
	if windowState.Maximized {
		// Zoom after Show, not before: zooming a window that has never
		// been shown is unreliable on Cocoa (see App.Maximize's doc
		// comment). The explicit SetContentSize/SetPosition above still
		// runs first regardless, giving the window a sane frame to
		// restore to if the user later un-maximizes it by hand.
		app.Maximize()
	}
	app.Run()

	// Cleanup: disconnect all backends (kills the server subprocess).
	connMgr.DisconnectAll()

	w, h := app.ContentSize()
	x, y := app.Position()
	saveWindowState(appui.WindowState{Width: w, Height: h, X: x, Y: y, Maximized: app.IsMaximized()})

	app.Close()
}

// findServerBinary locates the gophermind-server binary. It checks, in
// order: the GOPHERMIND_SERVER env var, then the same directory as the
// current executable, then ../gophermind-server relative to cwd (the
// common dev layout where both modules are siblings).
func findServerBinary() string {
	if p := os.Getenv("GOPHERMIND_SERVER"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		// Check the executable's own directory, the bundle's Resources
		// directory (build-app.sh copies gophermind-server to
		// Contents/Resources, sibling to Contents/MacOS/GopherMind -- see
		// that script's "Bundle server binary" step), then two levels up
		// (the bare build/ directory, for a build that hasn't been bundled).
		for _, candidate := range []string{
			filepath.Join(dir, "gophermind-server"),
			filepath.Join(dir, "..", "Resources", "gophermind-server"),
			filepath.Join(dir, "..", "..", "gophermind-server"),
		} {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	// Dev layout: gophermind.com/gophermind-server/gophermind-server
	// (built binary in the server module's own directory).
	cwd, err := os.Getwd()
	if err == nil {
		candidate := filepath.Join(cwd, "..", "gophermind-server", "gophermind-server")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}
