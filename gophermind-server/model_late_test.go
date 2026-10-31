package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"gophermind/gophermind-lib/serve"
)

// TestChatWorksWhenTheModelServerStartsLate covers the launchd boot order: the
// server comes up while the model server is still loading, so the one lookup at
// startup is refused. The first chat turn after the model server is up must
// still find the model instead of failing with "no model configured".
func TestChatWorksWhenTheModelServerStartsLate(t *testing.T) {
	var up atomic.Bool
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up.Load() {
			http.Error(w, "loading", http.StatusServiceUnavailable)
			return
		}
		if strings.Contains(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"fake-model"}]}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(finalAnswerBody("hello")))
	}))
	t.Cleanup(llm.Close)

	root := t.TempDir()
	cfg := serverConfig{Token: "t", LLMEndpoint: llm.URL, Root: root}
	d, err := buildDeps(cfg, root, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("buildDeps: %v", err)
	}
	mux, err := serve.NewMux(d, serve.Options{Token: "t"})
	if err != nil {
		t.Fatalf("serve.NewMux: %v", err)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	up.Store(true)

	resp, err := http.DefaultClient.Do(authedReq(t, http.MethodPost, srv.URL+"/run", "t", "say hello"))
	if err != nil {
		t.Fatalf("POST /run: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || strings.Contains(string(body), "no model configured") {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
}
