package main

import (
	"fmt"
	"os"
)

func main() {
	app, err := NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gophermind-osx:", err)
		os.Exit(1)
	}

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
	app.Close()
}
