package serve

import (
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"time"

	"gophermind/gophermind-lib/runview"
)

// maxEventStreams caps the open GET /app/briefs/{id}/events streams.
const maxEventStreams = 8

// defaultEventEvery is how often a stream looks at the run's files. A test
// shortens it.
var defaultEventEvery = time.Second

// keepAliveEvery is how often an idle stream writes a comment line so a proxy
// does not close it.
const keepAliveEvery = 15 * time.Second

// stateHash is a content hash of a state, not counting the generation time.
func stateHash(st BriefState) [32]byte {
	st.GeneratedAt = ""
	b, _ := json.Marshal(st)
	return sha256.Sum256(b)
}

// finalState reports whether nothing more will change in the run.
func finalState(st BriefState) bool {
	if st.Run.Active {
		return false
	}
	switch st.Run.Phase {
	case phaseDone, phaseFailed, phaseStopped, phaseInterrupted:
		return true
	}
	return false
}

// eventsHandler handles GET /app/briefs/{id}/events: a server-sent event
// stream. It sends a "state" event first, then another whenever the run's
// files (or the log) change, checked every second, and an "end" event once
// the run is final. The stream ends when the client goes away.
func (m *briefManager) eventsHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !runview.ValidRunID(id) {
		briefError(w, http.StatusNotFound, "no such run", nil)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		briefError(w, http.StatusInternalServerError, "streaming is not supported", nil)
		return
	}
	select {
	case m.streams <- struct{}{}:
		defer func() { <-m.streams }()
	default:
		w.Header().Set("Retry-After", "5")
		briefError(w, http.StatusServiceUnavailable, "too many open streams", nil)
		return
	}
	st, err := m.buildState(id)
	if err != nil {
		briefError(w, http.StatusNotFound, "no such run", nil)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func(st BriefState) {
		b, _ := json.Marshal(st)
		writeSSEEvent(w, fl, "state", string(b))
	}
	send(st)
	last := stateHash(st)
	if finalState(st) {
		writeSSEEvent(w, fl, "end", "{}")
		return
	}
	tick := time.NewTicker(m.eventEvery)
	defer tick.Stop()
	lastWrite := time.Now()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
		}
		st, err := m.buildState(id)
		if err != nil {
			return
		}
		if sum := stateHash(st); sum != last {
			last = sum
			send(st)
			lastWrite = time.Now()
		} else if time.Since(lastWrite) >= keepAliveEvery {
			_, _ = w.Write([]byte(": keep-alive\n\n"))
			fl.Flush()
			lastWrite = time.Now()
		}
		if finalState(st) {
			writeSSEEvent(w, fl, "end", "{}")
			return
		}
	}
}
