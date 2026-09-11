package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// TestEmbeddedServerFallsBackToFreeProvider is the proof for this task: a
// config pointing at a deliberately dead address must not hang startup, and
// the embedded server must resolve to the free-ovhcloud fallback instead of
// staying stuck on an unreachable endpoint. This is the scenario that used
// to hang the desktop app for five minutes with no way to type in the
// window (a slow/unreachable LAN endpoint with a model configured).
func TestEmbeddedServerFallsBackToFreeProvider(t *testing.T) {
	// 127.0.0.1:1 is a privileged, essentially always-closed port: a connect
	// attempt against it fails fast (connection refused), which is exactly
	// the "unreachable endpoint" shape this test needs, without depending on
	// any real dead host being reachable-but-silent.
	t.Setenv("GOPHERMIND_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("GOPHERMIND_MODEL", "some-model-that-does-not-matter")
	t.Setenv("GOPHERMIND_ROOT", t.TempDir())
	t.Setenv("GOPHERMIND_APPROVAL", "auto")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := time.Now()
	srv, err := startEmbeddedServer(ctx)
	if err != nil {
		t.Fatalf("startEmbeddedServer: %v", err)
	}
	defer func() {
		if err := srv.Shutdown(); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	}()

	// startEmbeddedServer itself must return almost immediately: it must not
	// block on the LLM client at all, dead endpoint or not.
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("startEmbeddedServer took %v; it must return before the LLM backend resolves", elapsed)
	}

	deadline := time.Now().Add(15 * time.Second)
	var snap struct {
		Ready           bool   `json:"ready"`
		BaseURL         string `json:"baseURL"`
		Model           string `json:"model"`
		FellBack        bool   `json:"fellBack"`
		FailedBaseURL   string `json:"failedBaseURL"`
		FallbackProfile string `json:"fallbackProfile"`
		Error           string `json:"error"`
	}
	for {
		if time.Now().After(deadline) {
			t.Fatalf("backend status never became ready within 15s; last snapshot: %+v", snap)
		}

		req, _ := http.NewRequest(http.MethodGet, srv.BaseURL+"/backend-status", nil)
		req.Header.Set("Authorization", "Bearer "+srv.Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET /backend-status: %v", err)
		}
		decErr := json.NewDecoder(resp.Body).Decode(&snap)
		resp.Body.Close()
		if decErr != nil {
			t.Fatalf("decode /backend-status response: %v", decErr)
		}

		if snap.Ready || snap.Error != "" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("fallback resolution took %v; want well under 15s", elapsed)
	}

	if snap.Error != "" {
		t.Fatalf("backend status reported an error instead of falling back: %s", snap.Error)
	}
	if !snap.Ready {
		t.Fatal("backend status never reported ready")
	}
	if !snap.FellBack {
		t.Fatalf("expected a fallback, got snapshot: %+v", snap)
	}
	if snap.FallbackProfile != "free-ovhcloud" {
		t.Fatalf("expected fallback profile free-ovhcloud, got %q", snap.FallbackProfile)
	}
	if snap.FailedBaseURL != "http://127.0.0.1:1" {
		t.Fatalf("expected failedBaseURL http://127.0.0.1:1, got %q", snap.FailedBaseURL)
	}
	if snap.BaseURL == "http://127.0.0.1:1" {
		t.Fatal("backend status still reports the dead endpoint as the active one")
	}
	if snap.Model == "" {
		t.Fatal("expected a non-empty fallback model")
	}
}
