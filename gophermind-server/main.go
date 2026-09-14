// Command gophermind-server is the standalone server binary for the
// gophermind-osx project (.planning/ROADMAP.md Phase 2): it imports
// gophermind-lib and will eventually serve every HTTP endpoint gophermind-osx
// needs (session CRUD, SSE streaming, approvals, models, skills, pipeline,
// run, WireGuard peer registration) via serve.NewMux's Deps contract.
//
// This entry point (.planning/tasks/02-01.json) is deliberately narrow: flag
// and env parsing, graceful shutdown, logging and metrics initialization.
// It intentionally does NOT call serve.NewMux or wire any Deps yet -- with
// Deps.Run left nil, the always-registered POST /run route would panic on
// first use, and wiring the full contract is explicitly plan 02-02's job.
// Until then, this binary serves only the unauthenticated health/ready/
// metrics probes, so it is a real, runnable server rather than a stub that
// merely compiles.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"gophermind/gophermind-lib/config"
	"gophermind/gophermind-lib/serve"
)

// defaultPort matches gophermind-lib/serve's usual local dev port choice
// elsewhere in the codebase, kept distinct from the CLI's own webhook usage.
const defaultPort = 8090

// shutdownTimeout bounds how long graceful shutdown waits for in-flight
// requests to drain and the WireGuard interface to close before giving up.
const shutdownTimeout = 10 * time.Second

// serverConfig holds gophermind-server's own settings. Kept separate from
// gophermind-lib/config.Config (the CLI's shared provider/session settings):
// port, bearer token, and WG interface name are server-only concerns that
// don't belong on a struct also used by the desktop app and CLI.
type serverConfig struct {
	Port        int
	Token       string
	WGInterface string
	LLMEndpoint string
}

// parseServerConfig parses flags against args (typically os.Args[1:]), using
// getenv to resolve each flag's env-var default (typically os.Getenv) so
// GOPHERMIND_PORT/GOPHERMIND_TOKEN/GOPHERMIND_WG_INTERFACE/
// GOPHERMIND_LLM_ENDPOINT are respected without an explicit flag. A flag
// passed on the command line always overrides its env var. Returns an error
// if Token is empty either way: an unauthenticated task-running server must
// never start (mirrors gophermind-lib/serve's own webhook.serveToken check).
func parseServerConfig(args []string, getenv func(string) string) (serverConfig, error) {
	fs := flag.NewFlagSet("gophermind-server", flag.ContinueOnError)
	port := fs.Int("port", intEnvOr(getenv, "GOPHERMIND_PORT", defaultPort), "HTTP listen port")
	token := fs.String("token", getenv("GOPHERMIND_TOKEN"), "bearer token required on every task-running route")
	wgIface := fs.String("wg-interface", strEnvOr(getenv, "GOPHERMIND_WG_INTERFACE", "wg0"), "WireGuard interface name")
	llmEndpoint := fs.String("llm-endpoint", getenv("GOPHERMIND_LLM_ENDPOINT"), "LLM endpoint base URL (falls back to the shared config's GOPHERMIND_BASE_URL when unset)")
	if err := fs.Parse(args); err != nil {
		return serverConfig{}, err
	}

	cfg := serverConfig{Port: *port, Token: *token, WGInterface: *wgIface, LLMEndpoint: *llmEndpoint}
	if cfg.Token == "" {
		return serverConfig{}, fmt.Errorf("refusing to start without a bearer token: set --token or GOPHERMIND_TOKEN")
	}
	return cfg, nil
}

func intEnvOr(getenv func(string) string, key string, fallback int) int {
	v := getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func strEnvOr(getenv func(string) string, key, fallback string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return fallback
}

// resolveLLMEndpoint returns cfg.LLMEndpoint when set; otherwise it falls
// back to the shared gophermind-lib/config loader's BaseURL (GOPHERMIND_BASE_URL
// plus .env), so operators who already configure the CLI/desktop don't need a
// separate server-only endpoint setting. loadShared's error is not fatal here
// -- an endpoint-less server can still serve health/ready/metrics -- so a
// failure just leaves LLMEndpoint empty rather than aborting startup.
func resolveLLMEndpoint(cfg serverConfig, loadShared func() (config.Config, error)) string {
	if cfg.LLMEndpoint != "" {
		return cfg.LLMEndpoint
	}
	shared, err := loadShared()
	if err != nil {
		return ""
	}
	return shared.BaseURL
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("gophermind-server", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := parseServerConfig(os.Args[1:], os.Getenv)
	if err != nil {
		return err
	}
	cfg.LLMEndpoint = resolveLLMEndpoint(cfg, config.Load)
	logger.Info("starting", "port", cfg.Port, "wg_interface", cfg.WGInterface, "llm_endpoint_configured", cfg.LLMEndpoint != "")

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
	if err != nil {
		return fmt.Errorf("listen on port %d: %w", cfg.Port, err)
	}

	metrics := &serve.ServeMetrics{}
	httpSrv := &http.Server{Handler: probeMux(metrics)}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// wgCloser is nil until plan 02-03 wires WireGuard server init into this
	// entry point; runServer's shutdown path already has the hook ready.
	var wgCloser io.Closer
	return runServer(ln, httpSrv, wgCloser, logger, ctx)
}

// probeMux serves only the unauthenticated liveness/readiness/metrics
// routes. Session/run/pipeline/skills routes are wired in plan 02-02 via
// serve.NewMux once real Deps exist.
func probeMux(metrics *serve.ServeMetrics) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte(metrics.Render()))
	})
	return mux
}

// runServer serves httpSrv on ln and blocks until ctx is done (in
// production, signal.NotifyContext firing on SIGINT/SIGTERM; tests inject a
// context they control instead of delivering a real OS signal) or httpSrv
// fails to start, then drains the server and closes wgCloser -- the
// WireGuard interface, nil until plan 02-03 -- within shutdownTimeout. ln
// and ctx are parameters (rather than runServer binding its own port and
// installing its own signal handler) specifically so tests can exercise
// both the graceful-shutdown and listen-failure paths without binding a
// fixed port or depending on the test process receiving a real signal.
func runServer(ln net.Listener, httpSrv *http.Server, wgCloser io.Closer, logger *slog.Logger, ctx context.Context) error {
	serveErr := make(chan error, 1)
	go func() {
		err := httpSrv.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	err := httpSrv.Shutdown(shutdownCtx)

	if wgCloser != nil {
		if cerr := wgCloser.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}
	return err
}
