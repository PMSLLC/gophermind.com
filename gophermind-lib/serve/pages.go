package serve

import (
	"embed"
	"io"
	"net/http"
	"strings"
)

// pagesAssets embeds the signed-in browser pages: the home page, the chat page
// and the brief pages. They are static HTML with an inline script and no build
// step; their data comes from the session API and GET /auth/me, which the
// browser calls with its session cookie (the middleware adds the bearer token).
// gm.css is the one stylesheet; pageHandler inlines it and the navigation rail
// into each page (the page's CSP allows inline styles and scripts only).
//
//go:embed assets/home.html assets/chat.html assets/briefs.html assets/brief_new.html assets/brief.html assets/gm.css
var pagesAssets embed.FS

// Markers a page carries where the shared parts go.
const (
	pageCSSMarker  = "/*GM_CSS*/"
	pageRailMarker = "<!--GM_RAIL-->"
)

// pageNav is the page's entry in the navigation rail ("" for none).
var pageNav = map[string]string{
	"assets/home.html":      "home",
	"assets/briefs.html":    "runs",
	"assets/brief.html":     "runs",
	"assets/brief_new.html": "upload",
	"assets/chat.html":      "chat",
}

// navItems are the rail's links, in order.
var navItems = []struct{ key, href, label string }{
	{"runs", "/app/briefs", "Runs"},
	{"upload", "/app/briefs/new", "Upload brief"},
	{"chat", "/app/chat", "Chat"},
	{"pipeline", "/pipeline", "Pipeline"},
}

// railHTML is the navigation rail with the page's own entry marked current.
// Everything in it is static; the signed-in address is set as text by a script.
func railHTML(active string) string {
	var b strings.Builder
	b.WriteString(`<aside class="rail"><a class="brand" href="/"><i></i>GopherMind</a><nav aria-label="Main">`)
	for _, n := range navItems {
		b.WriteString(`<a href="` + n.href + `"`)
		if n.key == active {
			b.WriteString(` aria-current="page"`)
		}
		b.WriteString(`>` + n.label + `</a>`)
	}
	b.WriteString(`</nav><div class="who"><span class="email" id="who-email"></span>` +
		`<form method="post" action="/auth/logout"><button type="submit">Sign out</button></form></div></aside>` +
		`<script>fetch('/auth/me',{credentials:'same-origin'}).then(function(r){return r.ok?r.json():null}).then(function(j){` +
		`if(j&&j.email)document.getElementById('who-email').textContent=j.email}).catch(function(){})</script>`)
	return b.String()
}

// pageHandler serves one embedded page on GET.
func pageHandler(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f, err := pagesAssets.Open(name)
		if err != nil {
			http.Error(w, "page asset missing", http.StatusInternalServerError)
			return
		}
		defer f.Close()
		page, err := io.ReadAll(f)
		if err != nil {
			http.Error(w, "page asset unreadable", http.StatusInternalServerError)
			return
		}
		css, err := pagesAssets.ReadFile("assets/gm.css")
		if err != nil {
			http.Error(w, "page asset missing", http.StatusInternalServerError)
			return
		}
		out := strings.Replace(string(page), pageCSSMarker, string(css), 1)
		out = strings.Replace(out, pageRailMarker, railHTML(pageNav[name]), 1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, out)
	}
}
