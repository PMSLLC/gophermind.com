package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"gophermind/internal/session"
)

// webhookHandler builds an HTTP handler that runs a one-shot task from an
// inbound POST and returns the agent's answer. The body is either raw text or a
// JSON object {"task": "..."}. When token is non-empty, a matching
// "Authorization: Bearer <token>" header is required.
func webhookHandler(run func(ctx context.Context, task string) (string, error), token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "use POST", http.StatusMethodNotAllowed)
			return
		}
		// Constant-time bearer-token check (avoids leaking the token via response
		// timing). An empty token means the handler itself is unauthenticated —
		// runServe refuses to start in that case, so this path is test-only.
		if token != "" {
			want := "Bearer " + token
			got := r.Header.Get("Authorization")
			if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		// Optional HMAC payload verification (GitHub/Stripe style): when a shared
		// secret is configured, the request must carry a matching signature so the
		// trigger source is trusted, not just the bearer token.
		if secret := serveHMACSecret(); secret != "" {
			if !verifyHMAC(secret, body, r.Header.Get("X-Hub-Signature-256")) {
				http.Error(w, "bad signature", http.StatusUnauthorized)
				return
			}
		}
		task := strings.TrimSpace(string(body))
		if strings.Contains(r.Header.Get("Content-Type"), "json") {
			var j struct {
				Task string `json:"task"`
			}
			if json.Unmarshal(body, &j) == nil && j.Task != "" {
				task = j.Task
			}
		}
		if task == "" {
			http.Error(w, "empty task", http.StatusBadRequest)
			return
		}

		answer, err := run(r.Context(), task)
		if err != nil {
			// Log details server-side; return a generic message so internal error
			// text (endpoints, paths) is not disclosed to the caller.
			fmt.Fprintln(os.Stderr, "serve: run failed:", err)
			http.Error(w, "run failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"result": answer})
	}
}

// healthHandler is a liveness probe: it always returns 200 while the process is
// running. Unauthenticated so a load balancer / k8s can reach it.
func healthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "ok\n")
	}
}

// readyHandler is a readiness probe: 200 when ready returns true, else 503, so
// traffic is only routed once the server can serve it.
func readyHandler(ready func() bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		if ready() {
			_, _ = io.WriteString(w, "ready\n")
			return
		}
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	}
}

// sseHandler streams a run's tokens to the caller as Server-Sent Events, so
// remote UIs see output live. Each token is sent as a `data:` frame and the
// stream ends with an `event: done` frame. Auth mirrors webhookHandler.
func sseHandler(run func(ctx context.Context, task string, emit func(string)) error, token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "use POST", http.StatusMethodNotAllowed)
			return
		}
		if token != "" {
			want := "Bearer " + token
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(want)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		// Enforce the same optional HMAC payload verification as /run, so the
		// streaming endpoint is not a weaker authentication path.
		if secret := serveHMACSecret(); secret != "" {
			if !verifyHMAC(secret, body, r.Header.Get("X-Hub-Signature-256")) {
				http.Error(w, "bad signature", http.StatusUnauthorized)
				return
			}
		}
		task := strings.TrimSpace(string(body))
		if task == "" {
			http.Error(w, "empty task", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, _ := w.(http.Flusher)
		emit := func(s string) {
			// Normalize newlines and prefix every line with "data: " so token
			// content can never inject additional SSE fields or events.
			writeSSEData(w, s)
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err := run(r.Context(), task, emit); err != nil {
			fmt.Fprintln(os.Stderr, "serve: stream run failed:", err)
			fmt.Fprintf(w, "event: error\ndata: run failed\n\n")
			return
		}
		fmt.Fprintf(w, "event: done\ndata: \n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}
}

// writeSSEData writes s as one or more SSE `data:` lines. Every line of s is
// prefixed with "data: " and CR/LF are normalized, so content (e.g. model
// tokens) can never inject additional SSE fields or events (frame injection).
func writeSSEData(w io.Writer, s string) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	for _, line := range strings.Split(s, "\n") {
		fmt.Fprintf(w, "data: %s\n", line)
	}
	fmt.Fprint(w, "\n")
}

// serveHMACSecret returns the configured HMAC secret for inbound payload
// verification (GOPHERMIND_SERVE_HMAC_SECRET), or "" to disable it.
func serveHMACSecret() string {
	return strings.TrimSpace(os.Getenv("GOPHERMIND_SERVE_HMAC_SECRET"))
}

// verifyHMAC reports whether sigHeader is a valid HMAC-SHA256 signature of body
// under secret. The header may be bare hex or "sha256=<hex>" (GitHub style).
// The comparison is constant-time.
func verifyHMAC(secret string, body []byte, sigHeader string) bool {
	sig := strings.TrimPrefix(strings.TrimSpace(sigHeader), "sha256=")
	got, err := hex.DecodeString(sig)
	if err != nil || len(got) == 0 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

// serveAddr returns the webhook listen address (GOPHERMIND_SERVE_ADDR or :8080).
func serveAddr() string {
	if a := strings.TrimSpace(os.Getenv("GOPHERMIND_SERVE_ADDR")); a != "" {
		return a
	}
	return ":8080"
}

// serveToken returns the configured webhook token, or an error when it is
// unset. A webhook that can run tools (shell, file writes) must never be exposed
// unauthenticated, so serve refuses to start without a token.
func serveToken() (string, error) {
	token := strings.TrimSpace(os.Getenv("GOPHERMIND_SERVE_TOKEN"))
	if token == "" {
		return "", fmt.Errorf("refusing to start webhook without GOPHERMIND_SERVE_TOKEN: it can run tools (shell, file writes)")
	}
	return token, nil
}

// runServe starts the webhook HTTP server, dispatching each POST /run to run.
// metrics (when non-nil) counts requests/errors and is exposed on /metrics.
// stream (when non-nil) backs a POST /run/stream Server-Sent-Events endpoint.
// sessionTurn (when non-nil) backs the session-backed multi-turn endpoints
// (POST /session, POST /session/{id}/stream, GET /session, DELETE
// /session/{id}); nil skips registering them, mirroring metrics/stream.
func runServe(run func(ctx context.Context, task string) (string, error), metrics *serveMetrics, stream func(ctx context.Context, task string, emit func(string)) error, sessionTurn SessionTurn) error {
	token, err := serveToken()
	if err != nil {
		return err
	}
	addr := serveAddr()
	// Wrap run to record request/error counters for the metrics endpoint.
	if metrics != nil {
		inner := run
		run = func(ctx context.Context, task string) (string, error) {
			metrics.requests.Add(1)
			out, err := inner(ctx, task)
			if err != nil {
				metrics.errors.Add(1)
			}
			return out, err
		}
	}
	mux := http.NewServeMux()
	// A single rate limiter (GOPHERMIND_SERVE_RATE req/min), keyed by the bearer
	// token, SHARED across every task-running endpoint so /run/stream cannot be
	// used to bypass /run's budget.
	rl := serveRateLimiter()
	keyOf := func(r *http.Request) string { return r.Header.Get("Authorization") }
	limited := func(h http.Handler) http.Handler {
		if rl == nil {
			return h
		}
		return rateLimitMiddleware(h, rl, keyOf)
	}

	mux.Handle("/run", limited(webhookHandler(run, token)))
	// Unauthenticated liveness/readiness probes for load balancers / k8s.
	mux.HandleFunc("/healthz", healthHandler())
	mux.HandleFunc("/readyz", readyHandler(func() bool { return true }))
	if metrics != nil {
		mux.HandleFunc("/metrics", metricsHandler(metrics))
	}
	if stream != nil {
		// Same auth (bearer + HMAC) and rate limit as /run — full sibling parity.
		mux.Handle("/run/stream", limited(sseHandler(stream, token)))
	}
	if sessionTurn != nil {
		// Session endpoints share /run's bearer+HMAC auth (via sessionAuth) and
		// rate limit (via limited), applied uniformly at registration since the
		// handlers themselves take no auth params (see session_serve.go).
		locks := newSessionLocks()
		sessionWrap := func(h http.Handler) http.Handler { return limited(sessionAuth(token, h)) }
		mux.Handle("POST /session", sessionWrap(sessionCreateHandler()))
		mux.Handle("POST /session/{id}/stream", sessionWrap(sessionStreamHandler(sessionTurn, locks)))
		mux.Handle("GET /session", sessionWrap(sessionListHandler(session.List)))
		mux.Handle("DELETE /session/{id}", sessionWrap(sessionDeleteHandler(session.Remove)))
	}
	fmt.Fprintf(os.Stderr, "gophermind serving webhook on %s (POST /run, GET /healthz, /readyz)\n", addr)
	return http.ListenAndServe(addr, mux)
}
