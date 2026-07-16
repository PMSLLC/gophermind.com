package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"

	"gophermind/internal/session"
	"gophermind/internal/stream"
)

// sessionLocks serializes turns per session id so two concurrent requests can
// never race on the same session's history file, while distinct ids proceed
// in parallel.
type sessionLocks struct {
	mu   sync.Mutex
	held map[string]bool
}

// newSessionLocks builds an empty lock registry.
func newSessionLocks() *sessionLocks {
	return &sessionLocks{held: make(map[string]bool)}
}

// TryAcquire claims id for the caller, reporting false if it is already held.
func (l *sessionLocks) TryAcquire(id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held[id] {
		return false
	}
	l.held[id] = true
	return true
}

// Release frees id so a later request may acquire it.
func (l *sessionLocks) Release(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.held, id)
}

// SessionTurn runs one session-backed agent turn: it loads/creates the
// session, runs the task, forwards typed progress frames via emit, and saves
// the session. The real implementation (main.go) wires this to a fresh agent
// per call; tests inject a fake.
type SessionTurn func(ctx context.Context, id, task string, emit func(event, data string) error) error

// validSessionID reports whether id is safe to use as a session id, reusing
// internal/session's own validation (via Path, since validID is unexported)
// so the server never accepts an id that could escape the sessions directory.
func validSessionID(id string) error {
	_, err := session.Path(id)
	return err
}

// sessionCreateHandler handles POST /session: it returns a freshly generated
// session id, or echoes back a caller-supplied id from a JSON body
// (`{"id":"..."}`) once validated.
func sessionCreateHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "use POST", http.StatusMethodNotAllowed)
			return
		}
		id := ""
		if r.Body != nil {
			body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err != nil {
				http.Error(w, "read body", http.StatusBadRequest)
				return
			}
			if len(bytes.TrimSpace(body)) > 0 {
				var j struct {
					ID string `json:"id"`
				}
				if json.Unmarshal(body, &j) == nil && j.ID != "" {
					if err := validSessionID(j.ID); err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					id = j.ID
				}
			}
		}
		if id == "" {
			id = stream.NewSessionID()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
	}
}

// sessionStreamHandler handles POST /session/{id}/stream: it runs one turn
// against the named session via turn, forwarding every emitted frame to the
// client as SSE, and serializes concurrent turns on the same id via locks.
func sessionStreamHandler(turn SessionTurn, locks *sessionLocks) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "use POST", http.StatusMethodNotAllowed)
			return
		}
		id := r.PathValue("id")
		if err := validSessionID(id); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !locks.TryAcquire(id) {
			http.Error(w, "session busy", http.StatusConflict)
			return
		}
		defer locks.Release(id)

		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
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
		emit := func(event, data string) error {
			writeSSEEvent(w, flusher, event, data)
			return nil
		}
		if err := turn(r.Context(), id, task, emit); err != nil {
			fmt.Fprintln(os.Stderr, "serve: session turn failed:", err)
			writeSSEEvent(w, flusher, "error", "run failed")
		}
		writeSSEEvent(w, flusher, "done", "")
	}
}

// sessionListHandler handles GET /session: it returns the injected list of
// saved sessions as a JSON array.
func sessionListHandler(list func() ([]session.Info, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "use GET", http.StatusMethodNotAllowed)
			return
		}
		infos, err := list()
		if err != nil {
			http.Error(w, "list failed", http.StatusInternalServerError)
			return
		}
		if infos == nil {
			infos = []session.Info{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(infos)
	}
}

// sessionDeleteHandler handles DELETE /session/{id}: it removes the named
// session via remove.
func sessionDeleteHandler(remove func(string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "use DELETE", http.StatusMethodNotAllowed)
			return
		}
		id := r.PathValue("id")
		if err := validSessionID(id); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := remove(id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// sessionAuth wraps h with the same bearer-token and optional HMAC checks
// webhookHandler/sseHandler apply to /run and /run/stream, so the session
// endpoints share one auth story. HMAC verification consumes and restores the
// body so the wrapped handler can still read it.
func sessionAuth(token string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" {
			want := "Bearer " + token
			got := r.Header.Get("Authorization")
			if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		if secret := serveHMACSecret(); secret != "" {
			body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err != nil {
				http.Error(w, "read body", http.StatusBadRequest)
				return
			}
			if !verifyHMAC(secret, body, r.Header.Get("X-Hub-Signature-256")) {
				http.Error(w, "bad signature", http.StatusUnauthorized)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		h.ServeHTTP(w, r)
	})
}
