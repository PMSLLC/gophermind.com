package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
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
func runServe(run func(ctx context.Context, task string) (string, error)) error {
	token, err := serveToken()
	if err != nil {
		return err
	}
	addr := serveAddr()
	mux := http.NewServeMux()
	mux.HandleFunc("/run", webhookHandler(run, token))
	// Unauthenticated liveness/readiness probes for load balancers / k8s.
	mux.HandleFunc("/healthz", healthHandler())
	mux.HandleFunc("/readyz", readyHandler(func() bool { return true }))
	fmt.Fprintf(os.Stderr, "gophermind serving webhook on %s (POST /run, GET /healthz, /readyz)\n", addr)
	return http.ListenAndServe(addr, mux)
}
