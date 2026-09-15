package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"gophermind/gophermind-osx/client"
	"gophermind/gophermind-osx/connection"
	appui "gophermind/gophermind-osx/ui"
)

// The dock icon and "clicking it focuses the window" (.planning/tasks/
// 04-08.json) need no code here: a normal windowed macOS app already gets
// a dock icon and standard reactivation-on-click behavior from AppKit for
// free, the same way libui-ng's native controls already follow the
// system's light/dark appearance for free (see NewApp's doc comment on
// Toggle Dark Mode). Both are properties of being a real windowed app on
// this platform, not something gophermind-osx implements.
func main() {
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
		conn, ok := connMgr.Get("local")
		if !ok || conn.Status() != connection.StatusConnected {
			chat.Transcript.AddUserMessage(text)
			chat.Transcript.AddSystem("Not connected to a backend. Open Settings and click Connect.")
			return
		}
		cl := conn.Client()
		if cl == nil {
			chat.Transcript.AddUserMessage(text)
			chat.Transcript.AddSystem("Connection is up but client is unavailable (reconnecting?).")
			return
		}
		// Create a session for this turn, then stream.
		ctx := context.Background()
		sessionID, err := cl.CreateSession(ctx, client.CreateSessionOptions{})
		if err != nil {
			chat.Transcript.AddUserMessage(text)
			chat.Transcript.AddSystem("error creating session: " + err.Error())
			return
		}
		chat.RunTurn(ctx, text, func(ctx context.Context, task string) (*client.EventStream, error) {
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
			cfg.Direct = connection.DirectConfig{BaseURL: baseURL}
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

	// Connect on startup.
	if err := chat.settingsUI.connectFunc(context.Background(), appui.BackendProfile{Name: "local", Mode: "local"}); err != nil {
		chat.Transcript.AddSystem("Connection failed: " + err.Error())
	} else {
		chat.Transcript.AddSystem("Connected to local gophermind-server.")
	}

	app.Show()
	app.Run()

	// Cleanup: disconnect all backends (kills the server subprocess).
	connMgr.DisconnectAll()

	w, h := app.ContentSize()
	x, y := app.Position()
	saveWindowState(appui.WindowState{Width: w, Height: h, X: x, Y: y})

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
		// Check the executable's own directory, then two levels up
		// (the .app bundle layout: build/GopherMind.app/Contents/MacOS/
		// -> build/ where gophermind-server lives).
		for _, candidate := range []string{
			filepath.Join(dir, "gophermind-server"),
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
