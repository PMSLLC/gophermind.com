package main

import (
	"context"
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
		if token != "" && r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
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
			http.Error(w, "run failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"result": answer})
	}
}

// serveAddr returns the webhook listen address (GOPHERMIND_SERVE_ADDR or :8080).
func serveAddr() string {
	if a := strings.TrimSpace(os.Getenv("GOPHERMIND_SERVE_ADDR")); a != "" {
		return a
	}
	return ":8080"
}

// runServe starts the webhook HTTP server, dispatching each POST /run to run.
func runServe(run func(ctx context.Context, task string) (string, error)) error {
	addr := serveAddr()
	mux := http.NewServeMux()
	mux.HandleFunc("/run", webhookHandler(run, strings.TrimSpace(os.Getenv("GOPHERMIND_SERVE_TOKEN"))))
	fmt.Fprintf(os.Stderr, "gophermind serving webhook on %s (POST /run)\n", addr)
	return http.ListenAndServe(addr, mux)
}
