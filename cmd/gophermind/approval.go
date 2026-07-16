package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"gophermind/internal/safety"
)

// approvalRegistry tracks gated tool calls awaiting a remote (phone) decision.
// Each pending approval owns a buffered(1) channel so resolve never blocks the
// caller (e.g. the HTTP handler serving the phone's decision) even if nobody
// is currently receiving from it (already timed out / ctx cancelled).
type approvalRegistry struct {
	mu      sync.Mutex
	pending map[string]chan bool
}

// newApprovalRegistry builds an empty registry.
func newApprovalRegistry() *approvalRegistry {
	return &approvalRegistry{pending: make(map[string]chan bool)}
}

// register creates and stores a buffered(1) decision channel for id, ready to
// receive at most one decision.
func (r *approvalRegistry) register(id string) chan bool {
	ch := make(chan bool, 1)
	r.mu.Lock()
	r.pending[id] = ch
	r.mu.Unlock()
	return ch
}

// resolve delivers approved to id's pending channel and reports whether id was
// found. It never blocks: the channel is buffered(1) and resolve removes the
// entry before sending, so a second resolve (or a resolve racing a
// timeout/cancel cleanup) simply reports "not found" instead of double-sending
// or blocking.
func (r *approvalRegistry) resolve(id string, approved bool) bool {
	r.mu.Lock()
	ch, ok := r.pending[id]
	if ok {
		delete(r.pending, id)
	}
	r.mu.Unlock()
	if !ok {
		return false
	}
	ch <- approved
	return true
}

// cancel removes id's pending entry without sending, for timeout/disconnect
// cleanup once the gate has already stopped waiting.
func (r *approvalRegistry) cancel(id string) {
	r.mu.Lock()
	delete(r.pending, id)
	r.mu.Unlock()
}

// remoteApprovalGate returns a safety.ApprovalFunc bound to one turn: each
// gated tool call registers a fresh pending approval, emits an
// "approval-needed" SSE frame carrying the approval id/tool/args, then blocks
// until the phone resolves it, the timeout elapses, or ctx is cancelled
// (client disconnect) — defaulting to deny on every path except an explicit
// true decision.
func remoteApprovalGate(reg *approvalRegistry, ctx context.Context, timeout time.Duration, emit func(event, data string) error, newID func() string) safety.ApprovalFunc {
	return func(tool, argsJSON string) bool {
		id := newID()
		ch := reg.register(id)
		defer reg.cancel(id)

		b, _ := json.Marshal(struct {
			ApprovalID string `json:"approval_id"`
			Tool       string `json:"tool"`
			Args       string `json:"args"`
		}{ApprovalID: id, Tool: tool, Args: argsJSON})
		_ = emit("approval-needed", string(b))

		select {
		case ok := <-ch:
			return ok
		case <-time.After(timeout):
			return false // auto-deny on timeout
		case <-ctx.Done():
			return false // client disconnect -> deny
		}
	}
}

// newApprovalID generates a random hex approval id (crypto/rand is fine here:
// this runs in normal server code, not a hot loop).
func newApprovalID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// serveApprovalRemote reports whether GOPHERMIND_SERVE_APPROVAL=remote is set,
// switching session turns from the shared auto-approve to the phone
// tool-approval gate. Read once at serve startup.
func serveApprovalRemote() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("GOPHERMIND_SERVE_APPROVAL")), "remote")
}

// serveApprovalTimeout returns how long a remote approval waits for the
// phone's decision before auto-denying: GOPHERMIND_SERVE_APPROVAL_TIMEOUT_S
// (seconds) when set to a positive integer, else a 5-minute default.
func serveApprovalTimeout() time.Duration {
	const def = 5 * time.Minute
	v := strings.TrimSpace(os.Getenv("GOPHERMIND_SERVE_APPROVAL_TIMEOUT_S"))
	if v == "" {
		return def
	}
	secs, err := strconv.Atoi(v)
	if err != nil || secs <= 0 {
		return def
	}
	return time.Duration(secs) * time.Second
}

// sessionApproveHandler handles POST /session/{id}/approve: the phone posts
// its decision for a pending gated tool call as JSON
// {"approval_id":"...","approved":true|false}. Resolving an unknown or
// already-resolved id reports 404, so the caller can tell a stale/duplicate
// decision from a live one.
func sessionApproveHandler(reg *approvalRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "use POST", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		var req struct {
			ApprovalID string `json:"approval_id"`
			Approved   bool   `json:"approved"`
		}
		if err := json.Unmarshal(body, &req); err != nil || req.ApprovalID == "" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if !reg.resolve(req.ApprovalID, req.Approved) {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "no pending approval"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}
}
