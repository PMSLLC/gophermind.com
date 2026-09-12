package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// backendPrefix is the path segment that selects a backend: a request to
// /b/<name>/session reaches that backend's /session.
const backendPrefix = "/b/"

// newRouter builds the desktop's front door.
//
// The frontend runs in a WebView and reaches gophermind with fetch and
// EventSource, which go through the OS network stack rather than any Go
// dialer. That is why this exists as a real local HTTP server rather than a
// dialer swap: it is the only place a request from the WebView can be
// redirected to a different machine, and later, through a tunnel.
//
// It keeps the desktop's one-binding rule intact. Endpoint() still hands the
// frontend a single base URL and a single token; which machine a session
// actually runs on is a path prefix, not a second binding and not a second
// credential in the WebView.
//
// frontToken is the credential the frontend presents. It is NOT forwarded:
// each backend is called with its own token, so a backend's credential never
// exists inside the WebView and one backend's token cannot be replayed
// against another.
func newRouter(reg *backendRegistry, frontToken string) (http.Handler, error) {
	if reg == nil {
		return nil, fmt.Errorf("router needs a backend registry")
	}
	if frontToken == "" {
		return nil, fmt.Errorf("router needs a front-door token")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/backends", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "use GET", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(reg.Public())
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		backend, rest, err := resolve(reg, r.URL.Path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		proxyTo(backend, rest).ServeHTTP(w, r)
	})

	return requireToken(frontToken, mux), nil
}

// resolve picks the backend for a path and returns the path the backend
// should see. An unprefixed path goes to the default backend unchanged, which
// is what lets the frontend keep working without knowing routing exists.
func resolve(reg *backendRegistry, path string) (Backend, string, error) {
	if !strings.HasPrefix(path, backendPrefix) {
		b, ok := reg.Default()
		if !ok {
			return Backend{}, "", fmt.Errorf("no backends configured")
		}
		return b, path, nil
	}
	rest := strings.TrimPrefix(path, backendPrefix)
	name, tail, found := strings.Cut(rest, "/")
	if name == "" {
		return Backend{}, "", fmt.Errorf("no backend named in path")
	}
	b, ok := reg.Get(name)
	if ok && !b.Available {
		// Never route to a backend with no usable credential: the request
		// would go out with an empty Authorization header and be refused
		// upstream, which reads as a server fault rather than a local
		// misconfiguration.
		return Backend{}, "", fmt.Errorf("backend %q is unavailable: %s", name, b.Reason)
	}
	if !ok {
		// Deliberately does not say which names exist: this is reachable by
		// anything holding the front token, and the backend list is a map of
		// which machines this desktop can reach.
		return Backend{}, "", fmt.Errorf("unknown backend")
	}
	if !found {
		return b, "/", nil
	}
	return b, "/" + tail, nil
}

// proxyTo builds a reverse proxy that rewrites the request onto one backend
// and presents that backend's own credential.
func proxyTo(b Backend, path string) http.Handler {
	target, err := url.Parse(b.BaseURL)
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "backend has an unusable base URL", http.StatusBadGateway)
		})
	}
	return &httputil.ReverseProxy{
		// -1 flushes every write straight through, which is what an agent
		// turn needs: it is SSE, one frame at a time over many seconds.
		//
		// Set as explicit intent rather than as a fix for an observed bug. I
		// could not construct a case where leaving it at the default buffered
		// anything: net/http special-cases text/event-stream and streams it
		// regardless, and a chunked non-SSE response streamed too, measured
		// both ways. It is here so that streaming does not depend on that
		// special case continuing to apply to every content type this router
		// carries.
		FlushInterval: -1,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = target.Scheme
			pr.Out.URL.Host = target.Host
			pr.Out.URL.Path = strings.TrimRight(target.Path, "/") + path
			pr.Out.Host = target.Host
			// The frontend's token is replaced, never forwarded.
			pr.Out.Header.Set("Authorization", "Bearer "+b.Token)
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			// The backend's address is not echoed: on a tunnel backend it
			// names a machine the frontend is not supposed to learn.
			http.Error(w, "backend unreachable: "+err.Error(), http.StatusBadGateway)
		},
	}
}

// openPaths are reachable without the router's token, matching exactly what
// internal/serve leaves open on the server behind it. A liveness probe that
// needs a credential is not a liveness probe, and the frontend polls /healthz
// during startup precisely because it does not have the endpoint yet.
//
// They disclose nothing: healthz and readyz are fixed strings, and metrics is
// counters. Anything that reads or writes a session is not in this set.
var openPaths = map[string]bool{
	"/healthz": true,
	"/readyz":  true,
	"/metrics": true,
}

// requireToken gates the router on its own bearer token.
//
// Without it the router would be an unauthenticated local port that drives
// every configured backend, including remote ones, so any process on the
// machine could run shell commands on another machine through it. The
// comparison is constant time for the same reason the servers behind it use
// one.
func requireToken(token string, next http.Handler) http.Handler {
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if openPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
