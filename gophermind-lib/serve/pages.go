package serve

import (
	"embed"
	"io"
	"net/http"
)

// pagesAssets embeds the signed-in browser pages: the home page and the chat
// page. Both are static HTML with an inline script and no build step; their
// data comes from the session API and GET /auth/me, which the browser calls
// with its session cookie (the middleware adds the bearer token).
//
//go:embed assets/home.html assets/chat.html
var pagesAssets embed.FS

// pageHandler serves one embedded page on GET.
func pageHandler(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f, err := pagesAssets.Open(name)
		if err != nil {
			http.Error(w, "page asset missing", http.StatusInternalServerError)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.Copy(w, f)
	}
}
