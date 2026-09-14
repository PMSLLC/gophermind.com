package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// modelsRequestPathFor spins up a server that records the path it was asked
// for and returns it after a ListModels call completes.
func modelsRequestPathFor(t *testing.T, modelsPath string) string {
	t.Helper()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"m"}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "", "m", 5*time.Second, false)
	c.ModelsPath = modelsPath
	if _, err := c.ListModels(context.Background()); err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	return got
}

// An empty ModelsPath must preserve today's ListModels behavior exactly.
func TestModelsPathDefaultsToV1(t *testing.T) {
	if got := modelsRequestPathFor(t, ""); got != "/v1/models" {
		t.Errorf("default path = %q, want /v1/models", got)
	}
}

// A base URL that already carries /v1 sets ModelsPath so the path is not
// doubled.
func TestModelsPathOverrideAvoidsDoubledV1(t *testing.T) {
	if got := modelsRequestPathFor(t, "/models"); got != "/models" {
		t.Errorf("override path = %q, want /models", got)
	}
}

// capabilitiesRequestPathFor spins up a server that records the path hit by
// the capability probe (fetchModelEntry, via the exported ProbeCapabilities)
// and returns it.
func capabilitiesRequestPathFor(t *testing.T, modelsPath string) string {
	t.Helper()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"m","context_length":1234}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "", "m", 5*time.Second, false)
	c.ModelsPath = modelsPath
	c.ProbeCapabilities(context.Background())
	return got
}

// An empty ModelsPath must preserve today's capability-probe behavior exactly.
func TestModelsPathCapabilitiesDefaultsToV1(t *testing.T) {
	if got := capabilitiesRequestPathFor(t, ""); got != "/v1/models" {
		t.Errorf("default capabilities path = %q, want /v1/models", got)
	}
}

// A base URL that already carries /v1 sets ModelsPath so the capability
// probe's path is not doubled either.
func TestModelsPathCapabilitiesOverrideAvoidsDoubledV1(t *testing.T) {
	if got := capabilitiesRequestPathFor(t, "/models"); got != "/models" {
		t.Errorf("override capabilities path = %q, want /models", got)
	}
}
