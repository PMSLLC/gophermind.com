package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
)

// webviewOriginHosts are the exact non-wails-scheme origins the WebView can
// present. On Windows, WebView2 serves the frontend over http(s) rather than a
// custom scheme, so those need naming explicitly.
var webviewOriginHosts = map[string]bool{
	"http://wails.localhost":        true,
	"https://wails.localhost":       true,
	"http://wails.localhost:34115":  true, // wails dev
	"https://wails.localhost:34115": true,
}

// allowedOrigin reports whether origin is one the Wails WebView can
// legitimately present.
//
// Any "wails:" scheme origin is accepted. That scheme is minted by the Wails
// runtime and cannot be produced by a page in an ordinary browser, so matching
// on it is as tight as naming a host, and it is robust to the exact host Wails
// chooses. That matters: this shipped allowing only "wails://wails.localhost",
// while Wails v2.13 on macOS actually sends "wails://wails", so every preflight
// was refused and the window showed nothing but "Load failed".
//
// An empty origin is not a CORS request at all (curl, the health probe, the
// tests) and is left alone.
func allowedOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	origin = strings.TrimSuffix(origin, "/")
	if strings.HasPrefix(origin, "wails://") {
		return true
	}
	return webviewOriginHosts[origin]
}

// withCORS allows the Wails WebView to call the embedded server.
//
// This wrapper lives in the desktop app, deliberately NOT in internal/serve:
// `gophermind serve` is a network service whose CORS posture is a separate
// decision, and giving it these headers because the desktop app needs them
// would widen a surface nobody asked to widen.
//
// Authorization is a non-simple header, so every real call is preceded by an
// OPTIONS preflight. Answering that preflight is most of what this does; the
// bearer token still gates every route behind it, so CORS here controls which
// page may ask, never whether the caller is authorized.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if lg := os.Getenv("GOPHERMIND_DESKTOP_REQLOG"); lg != "" {
			if f, err := os.OpenFile(lg, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
				fmt.Fprintf(f, "%s %s origin=%q allowed=%v\n", r.Method, r.URL.Path, origin, allowedOrigin(origin))
				f.Close()
			}
		}
		if allowedOrigin(origin) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			// Vary keeps a cache from serving one origin's allow header to
			// another origin.
			h.Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			// Preflight. Answer it here rather than letting it fall through to
			// a route that would reject the missing bearer token with a 401,
			// which the WebView would surface as the same opaque failure.
			if allowedOrigin(origin) {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
