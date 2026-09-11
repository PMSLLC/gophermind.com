package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// okHandler stands in for the real mux: it answers 200 so a test can tell a
// blocked preflight from a request that actually reached a route.
func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// The regression this file exists for: without an allow header the WebView
// blocks every fetch and the window shows the opaque "Load failed".
func TestCORSAllowsTheWebviewOrigin(t *testing.T) {
	const origin = "wails://wails.localhost"
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", origin)

	withCORS(okHandler()).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, origin)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// Authorization is a non-simple header, so every authenticated call is
// preceded by a preflight. If the preflight is not answered, nothing works.
func TestCORSAnswersPreflightWithAuthHeader(t *testing.T) {
	const origin = "wails://wails.localhost"
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/session", nil)
	req.Header.Set("Origin", origin)
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "authorization")

	withCORS(okHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("preflight status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Error("preflight did not allow any headers, so Authorization is blocked")
	}
}

// The exact origin Wails v2.13 sends on macOS. The first shipped allowlist
// named only "wails://wails.localhost", so every preflight was refused and the
// window showed only "Load failed". This pins the real value.
func TestCORSAllowsTheActualMacOSWailsOrigin(t *testing.T) {
	const origin = "wails://wails"
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/session", nil)
	req.Header.Set("Origin", origin)
	req.Header.Set("Access-Control-Request-Method", "POST")

	withCORS(okHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("preflight from %q = %d, want 204", origin, rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, origin)
	}
}

// A page in a real browser must not be able to drive the embedded server even
// if it discovers the port.
func TestCORSRefusesAnUnknownOrigin(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "http://evil.example")

	withCORS(okHandler()).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("allowed a foreign origin: %q", got)
	}
}

func TestCORSRefusesAnUnknownOriginPreflight(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/session", nil)
	req.Header.Set("Origin", "http://evil.example")

	withCORS(okHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("foreign preflight status = %d, want 403", rec.Code)
	}
}

// A request with no Origin is not a CORS request. curl, the health probe and
// server_test.go all look like this and must pass through untouched.
func TestCORSLeavesNonBrowserRequestsAlone(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	withCORS(okHandler()).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("added a CORS header to a non-CORS request: %q", got)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}
