package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
	llm, bodies := recordingLLM(t, "ok")
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	cfg := serverConfig{Token: "t", LLMEndpoint: llm.URL, Root: root, BriefsDir: briefsDir}
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
