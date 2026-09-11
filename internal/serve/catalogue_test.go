package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"gophermind/internal/modelcat"
)

// fakeSessionTurn is a no-op SessionTurn used only to make NewMux register
// the session-backed routes (including the new model catalogue/settings
// ones), which are gated on Deps.SessionTurn being non-nil.
func fakeSessionTurn(ctx context.Context, id, task string, emit func(event, data string) error) error {
	return nil
}

// newCatalogueTestMux isolates the odometer and model settings paths under
// t.TempDir() so this test never touches the real user state, and builds a
// mux with the session routes (and so the catalogue/settings routes) active.
func newCatalogueTestMux(t *testing.T) (*http.ServeMux, string) {
	t.Helper()
	t.Setenv("GOPHERMIND_SERVE_TOKEN", "t")
	t.Setenv("GOPHERMIND_ODOMETER", filepath.Join(t.TempDir(), "odo.json"))
	t.Setenv("GOPHERMIND_MODEL_SETTINGS", filepath.Join(t.TempDir(), "settings.json"))
	mux, err := NewMux(Deps{SessionTurn: fakeSessionTurn}, Options{})
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	return mux, "t"
}

func TestCatalogueRequiresBearerToken(t *testing.T) {
	mux, _ := newCatalogueTestMux(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/models/catalogue", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rr.Code, rr.Body.String())
	}
}

func TestCatalogueReturnsEntries(t *testing.T) {
	mux, token := newCatalogueTestMux(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/models/catalogue", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Entries []modelcat.Entry `json:"entries"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rr.Body.String())
	}
	if len(body.Entries) < 100 {
		t.Errorf("got %d entries, want the full registry (100+)", len(body.Entries))
	}
}

func TestSettingsGetReturnsDefaultsOnFreshStore(t *testing.T) {
	mux, token := newCatalogueTestMux(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/models/settings", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var s modelcat.Settings
	if err := json.Unmarshal(rr.Body.Bytes(), &s); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if s.CapacityPercent != 90 {
		t.Errorf("CapacityPercent = %d, want 90", s.CapacityPercent)
	}
	if s.WhenAllFull != "stay" {
		t.Errorf("WhenAllFull = %q, want stay", s.WhenAllFull)
	}
}

func TestSettingsPatchMergesAndPersists(t *testing.T) {
	mux, token := newCatalogueTestMux(t)

	patch := `{"capacity_percent":75,"custom_links":{"free-groq":"https://groq.com/docs"}}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/models/settings", bytes.NewBufferString(patch))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/models/settings", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	mux.ServeHTTP(rr2, req2)
	var s modelcat.Settings
	if err := json.Unmarshal(rr2.Body.Bytes(), &s); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if s.CapacityPercent != 75 {
		t.Errorf("CapacityPercent = %d, want 75 after patch", s.CapacityPercent)
	}
	if s.CustomLinks["free-groq"] != "https://groq.com/docs" {
		t.Errorf("custom link did not persist: %v", s.CustomLinks)
	}
	// Fields not present in the patch keep their default: this is a merge,
	// not a replace.
	if s.WhenAllFull != "stay" {
		t.Errorf("WhenAllFull = %q, want stay to survive an unrelated patch", s.WhenAllFull)
	}
}

func TestSettingsPatchRejectsDangerousLinkAndStoresNothing(t *testing.T) {
	mux, token := newCatalogueTestMux(t)

	patch := `{"custom_links":{"free-groq":"javascript:alert(1)"}}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/models/settings", bytes.NewBufferString(patch))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}

	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/models/settings", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	mux.ServeHTTP(rr2, req2)
	var s modelcat.Settings
	if err := json.Unmarshal(rr2.Body.Bytes(), &s); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(s.CustomLinks) != 0 {
		t.Errorf("a rejected patch stored a custom link: %v", s.CustomLinks)
	}
}

func TestSettingsOtherMethodNotAllowed(t *testing.T) {
	mux, token := newCatalogueTestMux(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/models/settings", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405; body=%s", rr.Code, rr.Body.String())
	}
}
