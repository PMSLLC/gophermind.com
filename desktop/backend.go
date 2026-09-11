package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"gophermind/internal/config"
	"gophermind/internal/llm"
)

// fallbackProfile is the free provider the desktop app falls back to when
// the configured endpoint fails its liveness probe. free-ovhcloud needs no
// API key and is confirmed working (see internal/freellm/compat.go).
const fallbackProfile = "free-ovhcloud"

// clientHolder holds the *llm.Client the running embedded server currently
// uses, if any. It exists so serve.Deps can be built, and the server
// started, before the LLM backend has finished resolving: Get returns a
// clear error while resolution is still in flight or has failed outright,
// instead of the caller blocking or touching a nil client.
type clientHolder struct {
	mu     sync.RWMutex
	client *llm.Client
}

// Get returns the currently active client, or an error naming that no model
// backend is available yet.
func (h *clientHolder) Get() (*llm.Client, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.client == nil {
		return nil, fmt.Errorf("no model backend available")
	}
	return h.client, nil
}

// Set installs client as the active client, replacing any previous one.
func (h *clientHolder) Set(client *llm.Client) {
	h.mu.Lock()
	h.client = client
	h.mu.Unlock()
}

// backendStatusSnapshot is the JSON shape GET /backend-status returns to the
// frontend: which endpoint and model the app ended up using, whether that
// took a fallback, and the failure text when even the fallback did not work.
type backendStatusSnapshot struct {
	// Ready is true once a usable client (configured or fallback) is
	// installed. False with an empty Error means resolution is still in
	// flight; false with a non-empty Error means it failed outright.
	Ready           bool   `json:"ready"`
	BaseURL         string `json:"baseURL,omitempty"`
	Model           string `json:"model,omitempty"`
	FellBack        bool   `json:"fellBack"`
	FailedBaseURL   string `json:"failedBaseURL,omitempty"`
	FallbackProfile string `json:"fallbackProfile,omitempty"`
	Error           string `json:"error,omitempty"`
}

// backendStatus is the mutable holder for the current backendStatusSnapshot.
// resolveLLMBackend writes it exactly once (to either a ready or an error
// state); the /backend-status handler reads it on every poll from the
// frontend.
type backendStatus struct {
	mu   sync.RWMutex
	snap backendStatusSnapshot
}

// setReady records a usable backend: baseURL and model are what is actually
// in use, and fellBack/failedBaseURL/fallbackProfile record whether that
// took a fallback away from the user's configured endpoint.
func (s *backendStatus) setReady(baseURL, model string, fellBack bool, failedBaseURL, fallbackProfile string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snap = backendStatusSnapshot{
		Ready:           true,
		BaseURL:         baseURL,
		Model:           model,
		FellBack:        fellBack,
		FailedBaseURL:   failedBaseURL,
		FallbackProfile: fallbackProfile,
	}
}

// setError records that no backend could be resolved, configured or
// fallback, with msg as the reportable reason.
func (s *backendStatus) setError(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snap = backendStatusSnapshot{Error: msg}
}

// Snapshot returns the current status for the /backend-status handler to
// encode.
func (s *backendStatus) Snapshot() backendStatusSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snap
}

// resolveLLMBackend builds the LLM client in the background, after the
// embedded server is already listening, so a slow or unreachable endpoint
// never blocks the window from becoming usable. It tries cfg as configured
// first; if newLLMClient's liveness probe fails, it falls back to
// fallbackProfile (free-ovhcloud) via config.ApplyProfile, which supplies
// that provider's BaseURL, DefaultModel, ChatPath, and ModelsPath, rather
// than a hardcoded URL. The outcome is recorded in status for the frontend
// and, on success, the resolved client is installed into holder so requests
// start working.
func resolveLLMBackend(ctx context.Context, cfg config.Config, holder *clientHolder, status *backendStatus) {
	client, err := newLLMClient(ctx, cfg)
	if err == nil {
		holder.Set(client)
		status.setReady(cfg.BaseURL, client.Model, false, "", "")
		return
	}
	failedBaseURL, firstErr := cfg.BaseURL, err

	fallbackCfg := cfg
	fallbackCfg.Profile = fallbackProfile
	fallbackCfg, applyErr := fallbackCfg.ApplyProfile()
	if applyErr != nil {
		status.setError(fmt.Sprintf(
			"endpoint %s unreachable (%v); fallback profile %s could not be applied: %v",
			failedBaseURL, firstErr, fallbackProfile, applyErr))
		return
	}

	fbClient, fbErr := newLLMClient(ctx, fallbackCfg)
	if fbErr != nil {
		status.setError(fmt.Sprintf(
			"endpoint %s unreachable (%v); fallback to %s also failed: %v",
			failedBaseURL, firstErr, fallbackProfile, fbErr))
		return
	}

	holder.Set(fbClient)
	status.setReady(fallbackCfg.BaseURL, fbClient.Model, true, failedBaseURL, fallbackProfile)
}

// backendStatusHandler serves GET /backend-status: the same bearer-token
// check as every other route on this listener, then the current
// backendStatusSnapshot as JSON. The frontend polls this after creating a
// session, since resolveLLMBackend runs concurrently with the window
// becoming usable.
func backendStatusHandler(token string, status *backendStatus) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "use GET", http.StatusMethodNotAllowed)
			return
		}
		want := "Bearer " + token
		got := r.Header.Get("Authorization")
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(status.Snapshot())
	}
}
