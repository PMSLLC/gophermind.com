package serve

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewMuxRejectsEmptyToken(t *testing.T) {
	t.Setenv("GOPHERMIND_SERVE_TOKEN", "")
	_, err := NewMux(Deps{}, Options{})
	if err == nil {
		t.Fatal("NewMux accepted an empty token; this endpoint runs shell commands")
	}
}

func TestNewMuxAcceptsExplicitToken(t *testing.T) {
	t.Setenv("GOPHERMIND_SERVE_TOKEN", "")
	mux, err := NewMux(Deps{}, Options{Token: "explicit"})
	if err != nil {
		t.Fatalf("NewMux with an explicit token: %v", err)
	}
	if mux == nil {
		t.Fatal("NewMux returned a nil mux and no error")
	}
}

func TestNewMuxFallsBackToEnvToken(t *testing.T) {
	t.Setenv("GOPHERMIND_SERVE_TOKEN", "from-env")
	if _, err := NewMux(Deps{}, Options{}); err != nil {
		t.Fatalf("NewMux should read the env token: %v", err)
	}
}

// Two calls must yield independent muxes, so one process can serve more
// than one listener.
func TestNewMuxReturnsIndependentMuxes(t *testing.T) {
	t.Setenv("GOPHERMIND_SERVE_TOKEN", "t")
	a, err := NewMux(Deps{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewMux(Deps{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("NewMux returned the same mux twice")
	}
}

// Serve must return when its context is cancelled, which is the graceful
// shutdown the old runServe had no way to do.
func TestServeReturnsOnContextCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, ln, http.NewServeMux()) }()

	// Let the server come up, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Returned, which is the point. Either nil or a shutdown error is fine.
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return within 5s of context cancel")
	}
}

// The health endpoint needs no token, so it is the cheapest proof that a
// mux built by NewMux actually serves.
func TestNewMuxServesHealthz(t *testing.T) {
	t.Setenv("GOPHERMIND_SERVE_TOKEN", "t")
	mux, err := NewMux(Deps{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("GET /healthz = %d, want 200", rec.Code)
	}
}
