package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/config"
	"gophermind/gophermind-lib/serve"
)

func envMap(m map[string]string) func(string) string {
	return func(key string) string { return m[key] }
}

func TestParseServerConfig_FlagsOverrideEnv(t *testing.T) {
	getenv := envMap(map[string]string{
		"GOPHERMIND_PORT":         "9000",
		"GOPHERMIND_TOKEN":        "env-token",
		"GOPHERMIND_WG_INTERFACE": "wg-env",
		"GOPHERMIND_LLM_ENDPOINT": "http://env-endpoint:8000",
	})
	cfg, err := parseServerConfig([]string{
		"--port", "9100",
		"--token", "flag-token",
		"--wg-interface", "wg-flag",
		"--llm-endpoint", "http://flag-endpoint:8000",
	}, getenv)
	if err != nil {
		t.Fatalf("parseServerConfig: %v", err)
	}
	want := serverConfig{Port: 9100, Token: "flag-token", WGInterface: "wg-flag", LLMEndpoint: "http://flag-endpoint:8000"}
	if cfg != want {
		t.Errorf("cfg = %+v, want %+v", cfg, want)
	}
}

func TestParseServerConfig_EnvVarsRespectedWithoutFlags(t *testing.T) {
	getenv := envMap(map[string]string{
		"GOPHERMIND_PORT":         "9000",
		"GOPHERMIND_TOKEN":        "env-token",
		"GOPHERMIND_WG_INTERFACE": "wg-env",
	})
	cfg, err := parseServerConfig(nil, getenv)
	if err != nil {
		t.Fatalf("parseServerConfig: %v", err)
	}
	want := serverConfig{Port: 9000, Token: "env-token", WGInterface: "wg-env", LLMEndpoint: ""}
	if cfg != want {
		t.Errorf("cfg = %+v, want %+v", cfg, want)
	}
}

func TestParseServerConfig_DefaultsWithoutEnvOrFlags(t *testing.T) {
	getenv := envMap(map[string]string{"GOPHERMIND_TOKEN": "t"})
	cfg, err := parseServerConfig(nil, getenv)
	if err != nil {
		t.Fatalf("parseServerConfig: %v", err)
	}
	if cfg.Port != defaultPort {
		t.Errorf("Port = %d, want default %d", cfg.Port, defaultPort)
	}
	if cfg.WGInterface != "wg0" {
		t.Errorf("WGInterface = %q, want %q", cfg.WGInterface, "wg0")
	}
}

func TestParseServerConfig_RefusesEmptyToken(t *testing.T) {
	_, err := parseServerConfig(nil, envMap(nil))
	if err == nil {
		t.Fatal("expected an error when no token is configured")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("error = %v, want it to mention the missing token", err)
	}
}

func TestParseServerConfig_MalformedPortEnvFallsBackToDefault(t *testing.T) {
	getenv := envMap(map[string]string{"GOPHERMIND_PORT": "not-a-number", "GOPHERMIND_TOKEN": "t"})
	cfg, err := parseServerConfig(nil, getenv)
	if err != nil {
		t.Fatalf("parseServerConfig: %v", err)
	}
	if cfg.Port != defaultPort {
		t.Errorf("Port = %d, want fallback default %d for a malformed env value", cfg.Port, defaultPort)
	}
}

func TestResolveLLMEndpoint_ExplicitWins(t *testing.T) {
	cfg := serverConfig{LLMEndpoint: "http://explicit:8000"}
	loadShared := func() (config.Config, error) {
		t.Fatal("loadShared should not be called when LLMEndpoint is already set")
		return config.Config{}, nil
	}
	got := resolveLLMEndpoint(cfg, loadShared)
	if got != "http://explicit:8000" {
		t.Errorf("got %q", got)
	}
}

func TestResolveLLMEndpoint_FallsBackToSharedConfig(t *testing.T) {
	cfg := serverConfig{}
	loadShared := func() (config.Config, error) {
		return config.Config{BaseURL: "http://shared:8000"}, nil
	}
	got := resolveLLMEndpoint(cfg, loadShared)
	if got != "http://shared:8000" {
		t.Errorf("got %q, want the shared config's BaseURL", got)
	}
}

func TestResolveLLMEndpoint_SharedConfigErrorIsNotFatal(t *testing.T) {
	cfg := serverConfig{}
	loadShared := func() (config.Config, error) { return config.Config{}, fmt.Errorf("boom") }
	got := resolveLLMEndpoint(cfg, loadShared)
	if got != "" {
		t.Errorf("got %q, want empty string on a shared-config load error", got)
	}
}

func TestProbeMux_HealthReadyMetrics(t *testing.T) {
	metrics := &serve.ServeMetrics{}
	metrics.PromptTokens.Add(7)
	mux := probeMux(metrics)
	ln := mustListen(t)
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()
	base := fmt.Sprintf("http://%s", ln.Addr())

	for _, path := range []string{"/healthz", "/readyz"} {
		resp, err := http.Get(base + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", path, resp.StatusCode)
		}
		resp.Body.Close()
	}

	resp, err := http.Get(base + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "gophermind_prompt_tokens_total 7") {
		t.Errorf("metrics body missing prompt token count: %s", body)
	}
}

// TestRunServer_GracefulShutdown covers the acceptance criterion directly:
// when ctx is cancelled -- standing in for signal.NotifyContext's ctx.Done()
// firing on SIGINT/SIGTERM, since a unit test cannot portably deliver a real
// signal and have every OS/test-runner handle it identically -- the server
// drains and the WireGuard interface stub is closed.
func TestRunServer_GracefulShutdown(t *testing.T) {
	ln := mustListen(t)
	httpSrv := &http.Server{Handler: http.NewServeMux()}
	wg := &fakeCloser{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	done := make(chan error, 1)
	go func() { done <- runServer(ln, httpSrv, wg, logger, ctx) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runServer: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runServer did not return after its context was cancelled")
	}
	if !wg.closed {
		t.Error("WireGuard closer was not closed on shutdown")
	}
}

// TestRunServer_ListenFailureReturnsError covers the other exit path: if
// httpSrv.Serve fails for a reason other than a graceful Shutdown (here, the
// listener is closed out from under it), runServer returns that error
// immediately rather than blocking until ctx is done.
func TestRunServer_ListenFailureReturnsError(t *testing.T) {
	ln := mustListen(t)
	ln.Close() // Serve on an already-closed listener fails immediately.

	httpSrv := &http.Server{Handler: http.NewServeMux()}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	err := runServer(ln, httpSrv, nil, logger, context.Background())
	if err == nil {
		t.Fatal("expected an error when the listener is already closed")
	}
}

// fakeCloser records whether Close was called, standing in for the
// WireGuard interface until plan 02-03 wires a real one.
type fakeCloser struct{ closed bool }

func (f *fakeCloser) Close() error {
	f.closed = true
	return nil
}

func mustListen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln
}
