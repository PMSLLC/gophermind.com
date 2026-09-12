// Command desktop is the GopherMind desktop shell (Wails). It starts an
// embedded internal/serve instance on a loopback, kernel-assigned port with a
// per-launch random token, and binds exactly one method to the frontend,
// Endpoint, which returns that address and token. Every other operation is
// plain HTTP/SSE against the embedded server, see app.go and deps.go.
package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	// A macOS app with no menu has no Window > Zoom and no View > Enter Full
	// Screen, so the green button is the only way to resize and the window
	// cannot be maximised. It also has no Edit menu, which is why Cmd+C and
	// Cmd+V do nothing in the WebView: those shortcuts are menu items on
	// macOS, not something the web content handles by itself.
	//
	// These are the standard roles rather than hand-built items, so they
	// behave exactly as every other Mac app's do and pick up the system's
	// own localisation and shortcuts.
	appMenu := menu.NewMenuFromItems(
		menu.AppMenu(),
		menu.EditMenu(),
		menu.WindowMenu(),
	)

	err := wails.Run(&options.App{
		Title:  "GopherMind Desktop",
		Width:  1024,
		Height: 768,
		// A floor rather than a fixed size: the transcript, the approval bar
		// and the model picker all need room, and below this they overlap.
		MinWidth:  720,
		MinHeight: 480,
		Menu:      appMenu,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 15, G: 15, B: 17, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
