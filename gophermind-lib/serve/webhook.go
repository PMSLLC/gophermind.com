package serve

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"gophermind/gophermind-lib/session"
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
		// Run refuses to start in that case, so this path is test-only.
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
			// Send detailed error but sanitize newlines to prevent SSE injection
			errMsg := strings.ReplaceAll(strings.ReplaceAll(err.Error(), "\r", " "), "\n", " ")
			fmt.Fprintf(w, "event: error\ndata: error: %s\n\n", errMsg)
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

// Deps is everything the mux needs. A nil func or pointer disables the
// routes that depend on it, exactly as the old positional nils did.
type Deps struct {
	// Run backs POST /run: it executes one task and returns the answer.
	Run func(ctx context.Context, task string) (string, error)
	// Stream backs POST /run/stream. Nil skips registering that route.
	Stream func(ctx context.Context, task string, emit func(string)) error
	// Metrics, when non-nil, counts requests/errors and is exposed on
	// /metrics.
	Metrics *ServeMetrics
	// SessionTurn, when non-nil, backs the session-backed multi-turn
	// endpoints (POST /session, POST /session/{id}/stream, GET /session,
	// DELETE /session/{id}); nil skips registering them.
	SessionTurn SessionTurn
	// Approvals, when non-nil alongside SessionTurn, additionally registers
	// POST /session/{id}/approve, resolving a pending remote tool-approval
	// gate.
	Approvals *approvalRegistry
	// Devices, when non-nil, registers POST /devices (S4 APNs device
	// registration), behind the same sessionAuth as the /session routes.
	Devices *deviceStore
	// SessionMessages, when non-nil alongside SessionTurn, additionally
	// registers GET /session/{id}/messages, returning a session's stored
	// conversation for history replay.
	SessionMessages func(id string) ([]json.RawMessage, bool, error)
	// ListModels, when non-nil alongside SessionTurn, additionally
	// registers GET /models.
	ListModels func() ([]string, error)
	// EndpointModels, when non-nil alongside SessionTurn, supplies the ids
	// the active configured endpoint serves to GET /models/catalogue. Nil
	// means the catalogue omits local-endpoint entries.
	EndpointModels func() []string
	// Pipeline, when non-nil, registers the live pipeline view routes: GET
	// /pipeline/state, GET /pipeline/events (SSE) and GET /pipeline/report,
	// behind the same bearer-token auth as the session routes. Nil skips
	// registering them.
	Pipeline *PipelineDeps

	// Skills, when non-nil, registers the skill catalogue and source routes
	// behind the same bearer-token auth as everything else. Nil skips them.
	//
	// These routes clone repositories and change what goes into an agent's
	// system prompt, so they are gated exactly like the session routes are.
	Skills *SkillsDeps
}

// Options carries per-deployment settings that used to be read from the
// environment inside Run.
type Options struct {
	// Token is the bearer token every task-running route requires. Empty
	// means "read GOPHERMIND_SERVE_TOKEN". NewMux returns an error if both
	// are empty: this endpoint runs shell commands and must never be open.
	Token string
}

// resolveToken determines the bearer token NewMux enforces: an explicit
// Options.Token takes precedence over GOPHERMIND_SERVE_TOKEN. It returns
// serveToken's own error, unchanged, when neither is set.
func resolveToken(opt Options) (string, error) {
	if opt.Token != "" {
		return opt.Token, nil
	}
	return serveToken()
}

// NewMux builds and validates the webhook HTTP handler from d and opt but
// does not listen. It returns an error when no bearer token is available
// (see Options.Token), since this endpoint runs shell commands and file
// writes and must never start unauthenticated.
// NewMux builds the server's route table from d, registering only the routes
// whose backing Deps field is non-nil (see Deps' own field comments for which
// field gates which routes). This is the API contract gophermind-server and
// gophermind-osx both build against (.planning/tasks/01-03.json):
//
//	Method  Path                          Auth         Gated by
//	POST    /run                          bearer+HMAC  always
//	GET     /healthz                      none         always
//	GET     /readyz                       none         always
//	GET     /metrics                      none         d.Metrics
//	POST    /run/stream                   bearer+HMAC  d.Stream
//	POST    /session                      bearer+HMAC  d.SessionTurn
//	POST    /session/{id}/stream          bearer+HMAC  d.SessionTurn
//	GET     /session                      bearer+HMAC  d.SessionTurn
//	DELETE  /session/{id}                 bearer+HMAC  d.SessionTurn
//	PATCH   /session/{id}                 bearer+HMAC  d.SessionTurn
//	GET     /modes                        bearer+HMAC  d.SessionTurn
//	GET     /session/{id}/config           bearer+HMAC  d.SessionTurn
//	GET     /session/{id}/messages         bearer+HMAC  d.SessionTurn && d.SessionMessages
//	POST    /session/{id}/approve          bearer+HMAC  d.SessionTurn && d.Approvals
//	GET     /models                        bearer+HMAC  d.SessionTurn && d.ListModels
//	GET     /models/catalogue              bearer+HMAC  d.SessionTurn
//	GET     /models/settings               bearer+HMAC  d.SessionTurn
//	PATCH   /models/settings               bearer+HMAC  d.SessionTurn
//	POST    /devices                       bearer+HMAC  d.Devices
//	GET     /skills                        bearer+HMAC  d.Skills
//	PATCH   /skills                        bearer+HMAC  d.Skills
//	POST    /skills/sources                bearer+HMAC  d.Skills
//	DELETE  /skills/sources/{id...}        bearer+HMAC  d.Skills
//	GET     /pipeline                      none         d.Pipeline (HTML shell only; carries no data)
//	GET     /pipeline/state                bearer+HMAC  d.Pipeline
//	GET     /pipeline/events               bearer+HMAC  d.Pipeline (SSE)
//	GET     /pipeline/report               bearer+HMAC  d.Pipeline
//
// All bearer+HMAC routes share one rate limiter (GOPHERMIND_SERVE_RATE
// req/min) keyed by the Authorization header, so no route can be used to
// bypass another's budget. SSE routes (/run/stream, /session/{id}/stream,
// /pipeline/events) emit the typed events defined in events.go.
func NewMux(d Deps, opt Options) (*http.ServeMux, error) {
	token, err := resolveToken(opt)
	if err != nil {
		return nil, err
	}
	run := d.Run
	// Wrap run to record request/error counters for the metrics endpoint.
	if d.Metrics != nil {
		inner := run
		run = func(ctx context.Context, task string) (string, error) {
			d.Metrics.requests.Add(1)
			out, err := inner(ctx, task)
			if err != nil {
				d.Metrics.errors.Add(1)
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
	if d.Metrics != nil {
		mux.HandleFunc("/metrics", metricsHandler(d.Metrics))
	}
	if d.Stream != nil {
		// Same auth (bearer + HMAC) and rate limit as /run — full sibling parity.
		mux.Handle("/run/stream", limited(sseHandler(d.Stream, token)))
	}
	if d.SessionTurn != nil {
		// Session endpoints share /run's bearer+HMAC auth (via sessionAuth) and
		// rate limit (via limited), applied uniformly at registration since the
		// handlers themselves take no auth params (see session_serve.go).
		locks := newSessionLocks()
		sessionWrap := func(h http.Handler) http.Handler { return limited(sessionAuth(token, h)) }
		mux.Handle("POST /session", sessionWrap(sessionCreateHandler()))
		mux.Handle("POST /session/{id}/stream", sessionWrap(sessionStreamHandler(d.SessionTurn, locks)))
		mux.Handle("GET /session", sessionWrap(sessionListHandler(session.List)))
		mux.Handle("DELETE /session/{id}", sessionWrap(sessionDeleteHandler(session.Remove)))
		mux.Handle("PATCH /session/{id}", sessionWrap(sessionRenameHandler(session.SetName)))
		mux.Handle("GET /modes", sessionWrap(http.HandlerFunc(modesHandler)))
		mux.Handle("GET /session/{id}/config", sessionWrap(sessionConfigHandler()))
		if d.SessionMessages != nil {
			mux.Handle("GET /session/{id}/messages", sessionWrap(sessionMessagesHandler(d.SessionMessages)))
		}
		if d.Approvals != nil {
			mux.Handle("POST /session/{id}/approve", sessionWrap(sessionApproveHandler(d.Approvals)))
		}
		if d.ListModels != nil {
			mux.Handle("GET /models", sessionWrap(modelsHandler(d.ListModels)))
		}
		mux.Handle("GET /models/catalogue", sessionWrap(catalogueHandler(d.EndpointModels)))
		mux.Handle("GET /models/settings", sessionWrap(settingsGetHandler()))
		mux.Handle("PATCH /models/settings", sessionWrap(settingsPatchHandler()))
	}
	if d.Devices != nil {
		// S4 APNs device registration, same bearer+HMAC auth as /session.
		mux.Handle("POST /devices", limited(sessionAuth(token, devicesHandler(d.Devices))))
	}
	if d.Skills != nil {
		skillWrap := func(h http.Handler) http.Handler { return limited(sessionAuth(token, h)) }
		mux.Handle("GET /skills", skillWrap(skillsListHandler(*d.Skills)))
		mux.Handle("PATCH /skills", skillWrap(skillsPatchHandler(*d.Skills)))
		mux.Handle("POST /skills/sources", skillWrap(skillsSourceAddHandler(*d.Skills)))
		mux.Handle("DELETE /skills/sources/{id...}", skillWrap(skillsSourceDeleteHandler(*d.Skills)))
	}

	if d.Pipeline != nil {
		// Live pipeline view (pipeline piece 5). The data routes share the
		// same bearer-token auth as the rest, via sessionAuth, applied
		// through the same wrap style as sessionWrap above. The dashboard
		// page itself (GET /pipeline) is deliberately not behind that auth:
		// a plain browser navigation cannot attach an Authorization header,
		// so gating the HTML shell the same way would make it unreachable.
		// It carries no task data of its own - its JS fetches the data
		// routes with the token entered in the page - so serving it
		// unauthenticated discloses nothing.
		hub := d.Pipeline.Hub
		if hub == nil {
			hub = NewPipelineHub()
		}
		mux.HandleFunc("GET /pipeline", pipelineDashboardHandler())
		pipeWrap := func(h http.Handler) http.Handler { return limited(sessionAuth(token, h)) }
		mux.Handle("GET /pipeline/state", pipeWrap(pipelineStateHandler(d.Pipeline.Root)))
		mux.Handle("GET /pipeline/events", pipeWrap(pipelineEventsHandler(hub)))
		mux.Handle("GET /pipeline/report", pipeWrap(pipelineReportHandler(d.Pipeline.Root)))
	}
	return mux, nil
}

// Serve listens on ln and serves h until ctx is cancelled, at which point it
// shuts the server down gracefully (10 second timeout) and returns. It also
// returns promptly, without waiting for ctx, if the listener itself fails
// (for example a port conflict or the listener being closed out from under
// it) before ctx is ever cancelled.
func Serve(ctx context.Context, ln net.Listener, h http.Handler) error {
	srv := &http.Server{Handler: h}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		// The server stopped on its own, before ctx was cancelled. Report
		// that instead of blocking on a ctx.Done() that may never fire.
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		<-serveErr
		return nil
	}
}

// Addr returns the webhook listen address (GOPHERMIND_SERVE_ADDR or :8080).
func Addr() string {
	return serveAddr()
}
