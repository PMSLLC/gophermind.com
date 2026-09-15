package main

import (
	"fmt"
	"os"

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

	// sendTurn has no backend wired yet -- gophermind-osx/connection (plan
	// 03-03) manages backend connections, but choosing/connecting one and
	// starting a session is a later integration step (a settings/connect
	// flow, Phase 4's later panels), not 04-01's job. Until then, Send
	// just reports that plainly rather than silently doing nothing. The
	// closure legitimately captures `chat` before its own declaration
	// finishes: it only runs later, from a real button click, by which
	// point the assignment below has long since completed.
	var chat *ChatWindow
	chat = NewChatWindow(app, func(text string) {
		chat.Transcript.AddUserMessage(text)
		chat.Transcript.AddSystem("Not connected to a backend yet -- nothing was sent.")
	})
	chat.Transcript.AddSystem("Not connected to a backend yet. Chat sending will work once a connection is configured.")

	app.Show()
	app.Run()

	w, h := app.ContentSize()
	x, y := app.Position()
	saveWindowState(appui.WindowState{Width: w, Height: h, X: x, Y: y})

	app.Close()
}
