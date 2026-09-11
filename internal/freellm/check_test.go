package freellm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckReturnsModelIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("requested %q, want /models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer k123" {
			t.Errorf("Authorization = %q, want bearer token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"m-a"},{"id":"m-b"}]}`))
	}))
	defer srv.Close()

	ids, err := Check(context.Background(), srv.URL, "k123", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "m-a" || ids[1] != "m-b" {
		t.Fatalf("ids = %v, want [m-a m-b]", ids)
	}
}

func TestCheckOmitsAuthWhenNoKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("sent an Authorization header with no key: %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	if _, err := Check(context.Background(), srv.URL, "", srv.Client()); err != nil {
		t.Fatal(err)
	}
}

func TestCheckSurfacesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	_, err := Check(context.Background(), srv.URL, "secret-key-value", srv.Client())
	if err == nil {
		t.Fatal("expected an error for 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error %q does not mention the status", err)
	}
	if strings.Contains(err.Error(), "secret-key-value") {
		t.Errorf("error leaked the API key: %q", err)
	}
}

func TestCheckSurfacesMalformedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	if _, err := Check(context.Background(), srv.URL, "", srv.Client()); err == nil {
		t.Fatal("expected an error for a malformed body")
	}
}

func TestCheckRedactsKeyEchoedByProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		// A hostile or debug endpoint echoing what it received.
		_, _ = w.Write([]byte(`{"error":"rejected ` + r.Header.Get("Authorization") + `"}`))
	}))
	defer srv.Close()

	_, err := Check(context.Background(), srv.URL, "super-secret-key", srv.Client())
	if err == nil {
		t.Fatal("expected an error for 401")
	}
	if strings.Contains(err.Error(), "super-secret-key") {
		t.Errorf("error leaked the API key echoed by the provider: %q", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Errorf("error does not show the redaction happened: %q", err)
	}
}
