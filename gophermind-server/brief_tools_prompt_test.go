package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gophermind/gophermind-lib/serve"
)

// recordingLLM is fakeLLM that also keeps the body of every completion request.
func recordingLLM(t *testing.T, answer string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"fake-model"}]}`))
			return
		}
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, string(b))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(finalAnswerBody(answer)))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), got...)
	}
}

// firstTurnRequest runs one new-session turn and returns the model request body.
func firstTurnRequest(t *testing.T, briefsDir string) string {
	t.Helper()
	return firstTurnRequestCfg(t, serverConfig{BriefsDir: briefsDir})
}

// firstTurnRequestCfg is firstTurnRequest with the brief and Burrow settings
// taken from base.
func firstTurnRequestCfg(t *testing.T, base serverConfig) string {
	t.Helper()
	llm, bodies := recordingLLM(t, "ok")
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	cfg := base
	cfg.Token, cfg.LLMEndpoint, cfg.Root = "t", llm.URL, root
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d, err := buildDeps(cfg, root, logger)
	if err != nil {
		t.Fatalf("buildDeps: %v", err)
	}
	mux, err := serve.NewMux(d, briefOptions(cfg, serve.Options{Token: "t"}))
	if err != nil {
		t.Fatalf("serve.NewMux: %v", err)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := http.DefaultClient.Do(authedReq(t, http.MethodPost, srv.URL+"/session", "t", `{"id":"bt-1"}`))
	if err != nil {
		t.Fatalf("POST /session: %v", err)
	}
	resp.Body.Close()
	resp, err = http.DefaultClient.Do(authedReq(t, http.MethodPost, srv.URL+"/session/bt-1/stream", "t", "anything waiting on me?"))
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	got := bodies()
	if len(got) == 0 {
		t.Fatal("no model request recorded")
	}
	return got[0]
}

func TestSessionTurnHasBriefToolsAndPrompt(t *testing.T) {
	with := firstTurnRequest(t, t.TempDir())
	if !strings.Contains(with, "brief_state") {
		t.Errorf("request with BriefsDir lacks brief_state tool")
	}
	// The guidance is JSON-encoded inside the request; compare a short sentence.
	if !strings.Contains(with, "questions_pending") || !strings.Contains(with, "never claim a write happened") {
		t.Errorf("request with BriefsDir lacks the brief guidance")
	}

	without := firstTurnRequest(t, "")
	if strings.Contains(without, "brief_state") || strings.Contains(without, "never claim a write happened") {
		t.Errorf("request without BriefsDir carries brief tools or guidance")
	}
}

func TestBurrowToolWiredFromConfig(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("fake-token-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(fake.Close)

	with := firstTurnRequestCfg(t, serverConfig{BriefsDir: t.TempDir(), BurrowURL: fake.URL, BurrowTokenFile: tokenFile})
	if !strings.Contains(with, "burrow_lookup") {
		t.Errorf("request with Burrow settings lacks the burrow_lookup tool")
	}
	if !strings.Contains(with, "matches exact contracts only") {
		t.Errorf("request with Burrow settings lacks the burrow guidance")
	}
	if strings.Contains(with, "fake-token-value") {
		t.Errorf("request carries the Burrow token")
	}

	without := firstTurnRequest(t, t.TempDir())
	if strings.Contains(without, "burrow_lookup") || strings.Contains(without, "matches exact contracts only") {
		t.Errorf("request without Burrow settings carries the tool or guidance")
	}
}
