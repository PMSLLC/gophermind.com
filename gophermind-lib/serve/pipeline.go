package serve

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"gophermind/gophermind-lib/runview"
)

// pipelineAssets embeds the dashboard page (pipeline piece 5, task 3). It
// adapts docs/design/pipeline_view_mockup.html: same layout, typography and
// status vocabulary, but its data comes from GET /pipeline/state and GET
// /pipeline/events instead of the mockup's hardcoded `const tasks`.
//
//go:embed assets/pipeline.html
var pipelineAssets embed.FS

// pipelineDashboardHandler handles GET /pipeline: it serves the embedded
// dashboard page. The page itself carries no data and is not behind the
// bearer-token auth the data routes require - a plain browser navigation
// cannot attach an Authorization header, so gating the shell the same way
// would make it unreachable. The page's own JS prompts for the token and
// sends it on every /pipeline/state, /pipeline/events and /pipeline/report
// call, which are the routes that actually carry task data.
func pipelineDashboardHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "use GET", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		f, err := pipelineAssets.Open("assets/pipeline.html")
		if err != nil {
			http.Error(w, "dashboard asset missing", http.StatusInternalServerError)
			return
		}
		defer f.Close()
		_, _ = io.Copy(w, f)
	}
}

// This file holds the dashboard page and the shared pieces of the pipeline
// routes. pipeline_api.go has the JSON routes; the event stream is below. Every
// route reads the run folder through gophermind-lib/runview: there is no
// cache, no hub and no watcher, so a stream and a fresh state call cannot
// disagree about what happened.

// PipelineDeps configures the pipeline routes. Root is the repo root whose
// .gophermind/<run>/ folders the routes read. MaxStreams caps concurrent event
// streams (zero means 16; the server reads GOPHERMIND_PIPELINE_MAX_STREAMS).
type PipelineDeps struct {
	Root       string
	MaxStreams int
}

// PipelineMaxStreamsFromEnv reads GOPHERMIND_PIPELINE_MAX_STREAMS. Unset,
// invalid or below 1 gives 0, which means the default.
func PipelineMaxStreamsFromEnv() int {
	n, err := strconv.Atoi(os.Getenv("GOPHERMIND_PIPELINE_MAX_STREAMS"))
	if err != nil || n < 1 {
		return 0
	}
	return n
}

// pipelineKeepalive and pipelinePoll are variables so tests can shorten them.
var (
	pipelineKeepalive = 15 * time.Second
	pipelinePoll      = 250 * time.Millisecond
)

// sinceOf is where an event stream starts. ?since=N wins, then Last-Event-ID;
// with neither the stream starts at the current end of the log (live only).
func sinceOf(r *http.Request, v *runview.View) (int64, error) {
	raw := r.URL.Query().Get("since")
	if raw == "" {
		raw = r.Header.Get("Last-Event-ID")
	}
	if raw == "" {
		return v.EventSeq(), nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0, runview.ErrBadID
	}
	return n, nil
}

// events handles GET /pipeline/events: an SSE stream of the run's events, each
// with its line number as id. At most MaxStreams streams run at once; one more
// gets 503. A client that goes away (the request context ends) frees its slot.
func (a *pipelineAPI) events(w http.ResponseWriter, r *http.Request) {
	select {
	case a.slots <- struct{}{}:
		defer func() { <-a.slots }()
	default:
		w.Header().Set("Retry-After", "5")
		pipelineJSON(w, http.StatusServiceUnavailable,
			map[string]any{"schema_version": runview.SchemaVersion, "error": "too many event streams"})
		return
	}
	v, err := a.view(r, runview.WithPoll(pipelinePoll))
	if err != nil {
		pipelineError(w, err)
		return
	}
	since, err := sinceOf(r, v)
	if err != nil {
		pipelineError(w, err)
		return
	}
	ctx := r.Context()
	ch, err := v.Follow(ctx, since)
	if err != nil {
		pipelineError(w, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)
	// Headers go out before the first event so a client whose Do blocks until a
	// response arrives is connected at once.
	w.WriteHeader(http.StatusOK)
	if flusher != nil {
		flusher.Flush()
	}
	ka := time.NewTicker(pipelineKeepalive)
	defer ka.Stop()
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return
			}
			var compact bytes.Buffer
			data := string(e.Data)
			if json.Compact(&compact, e.Data) == nil {
				data = compact.String()
			}
			writeSSEFrame(w, flusher, e.Seq, e.Type, data)
		case <-ka.C:
			fmt.Fprint(w, ": keepalive\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		case <-ctx.Done():
			return
		}
	}
}
