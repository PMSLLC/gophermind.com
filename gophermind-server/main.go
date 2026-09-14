// Command gophermind-server is the standalone server binary for the
// gophermind-osx project (.planning/ROADMAP.md Phase 2): it imports
// gophermind-lib and serves every HTTP endpoint gophermind-osx needs
// (session CRUD, SSE streaming, approvals, models, skills, pipeline, run)
// via serve.NewMux's Deps contract (see server.go's buildDeps). WireGuard
// peer registration is not yet wired -- that's plan 02-03.
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
	Root        string
}

// parseServerConfig parses flags against args (typically os.Args[1:]), using
// getenv to resolve each flag's env-var default (typically os.Getenv) so
// GOPHERMIND_PORT/GOPHERMIND_TOKEN/GOPHERMIND_WG_INTERFACE/
// GOPHERMIND_LLM_ENDPOINT/GOPHERMIND_ROOT are respected without an explicit
// flag. A flag passed on the command line always overrides its env var.
// Returns an error if Token is empty either way: an unauthenticated
// task-running server must never start (mirrors gophermind-lib/serve's own
// webhook.serveToken check). Root defaults to getwd (typically the caller's
// cwd) when neither the flag nor GOPHERMIND_ROOT is set -- getwd is a
// parameter rather than a direct os.Getwd() call so tests can inject a
// fixed value instead of depending on the test process's actual cwd.
func parseServerConfig(args []string, getenv func(string) string, getwd func() (string, error)) (serverConfig, error) {
	fs := flag.NewFlagSet("gophermind-server", flag.ContinueOnError)
	port := fs.Int("port", intEnvOr(getenv, "GOPHERMIND_PORT", defaultPort), "HTTP listen port")
	token := fs.String("token", getenv("GOPHERMIND_TOKEN"), "bearer token required on every task-running route")
	wgIface := fs.String("wg-interface", strEnvOr(getenv, "GOPHERMIND_WG_INTERFACE", "wg0"), "WireGuard interface name")
	llmEndpoint := fs.String("llm-endpoint", getenv("GOPHERMIND_LLM_ENDPOINT"), "LLM endpoint base URL (falls back to the shared config's GOPHERMIND_BASE_URL when unset)")
	root := fs.String("root", getenv("GOPHERMIND_ROOT"), "workspace root for file/shell tools and the pipeline view (default: cwd)")
	if err := fs.Parse(args); err != nil {
		return serverConfig{}, err
	}

	cfg := serverConfig{Port: *port, Token: *token, WGInterface: *wgIface, LLMEndpoint: *llmEndpoint, Root: *root}
	if cfg.Token == "" {
		return serverConfig{}, fmt.Errorf("refusing to start without a bearer token: set --token or GOPHERMIND_TOKEN")
	}
	if cfg.Root == "" {
		wd, err := getwd()
		if err != nil {
			return serverConfig{}, fmt.Errorf("determine working directory: %w", err)
		}
		cfg.Root = wd
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
	cfg, err := parseServerConfig(os.Args[1:], os.Getenv, os.Getwd)
	if err != nil {
		return err
	}
	cfg.LLMEndpoint = resolveLLMEndpoint(cfg, config.Load)

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
	if err != nil {
		return fmt.Errorf("listen on port %d: %w", cfg.Port, err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return runWithConfig(cfg, ln, logger, ctx)
}

// runWithConfig is run's testable core: everything after config
// resolution/listener binding/signal-handler installation, none of which
// (os.Args, a real port bind, a real signal handler) belongs in a unit
// test. Takes cfg, ln, and ctx as parameters -- rather than resolving them
// itself -- for exactly that reason, the same pattern parseServerConfig's
// injected getenv/getwd and runServer's injected ln/ctx already use.
func runWithConfig(cfg serverConfig, ln net.Listener, logger *slog.Logger, ctx context.Context) error {
	logger.Info("starting", "port", cfg.Port, "wg_interface", cfg.WGInterface, "llm_endpoint_configured", cfg.LLMEndpoint != "", "root", cfg.Root)

	svcDeps, pipelineHub, err := buildDeps(cfg, cfg.Root, logger)
	if err != nil {
		return fmt.Errorf("build deps: %w", err)
	}
	mux, err := serve.NewMux(svcDeps, serve.Options{Token: cfg.Token})
	if err != nil {
		return fmt.Errorf("build mux: %w", err)
	}

	// wgCloser stays nil (WireGuard disabled) when cfg.WGInterface is empty;
	// startWireGuard logs that and returns a nil Server/tracker too, so the
	// /wg/register route below is simply never registered.
	wg, tracker, err := startWireGuard(cfg, logger)
	if err != nil {
		return fmt.Errorf("start wireguard: %w", err)
	}
	var wgCloser io.Closer
	if wg != nil {
		wgCloser = wg
		mux.Handle("POST /wg/register", wgRegisterHandler(tracker, stubTokenValidator, logger))
		go tracker.runSweeper(ctx, peerSweepInterval)
	}
	httpSrv := &http.Server{Handler: mux}

	// Feeds the pipeline hub by watching cfg.Root/.planning/assignments.json
	// for changes, so GET /pipeline/events has something to stream -- the
	// same mechanism the CLI's own "serve" command uses (a phase execute run
	// happens in a different process; the assignments file is the only
	// channel between them).
	serve.StartPipelineWatcher(ctx, cfg.Root, pipelineHub)

	return runServer(ln, httpSrv, wgCloser, logger, ctx)
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
