package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestSessionModelRoundTrip(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())

	if got := readSessionModel("s1"); got != "" {
		t.Fatalf("readSessionModel before write = %q, want empty", got)
	}
	if err := writeSessionModel("s1", "gpt-4o"); err != nil {
		t.Fatalf("writeSessionModel: %v", err)
	}
	if got := readSessionModel("s1"); got != "gpt-4o" {
		t.Fatalf("readSessionModel = %q, want %q", got, "gpt-4o")
	}

	p, err := sessionModelPath("s1")
	if err != nil {
		t.Fatalf("sessionModelPath: %v", err)
	}
	if !strings.HasSuffix(p, "s1.model") {
		t.Fatalf("sessionModelPath = %q, want suffix s1.model", p)
	}
}

func TestWriteSessionModelEmptyRemovesSidecar(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())

	if err := writeSessionModel("s2", "claude-x"); err != nil {
		t.Fatalf("writeSessionModel: %v", err)
	}
	p, err := sessionModelPath("s2")
	if err != nil {
		t.Fatalf("sessionModelPath: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("sidecar not written: %v", err)
	}

	if err := writeSessionModel("s2", ""); err != nil {
		t.Fatalf("writeSessionModel(empty): %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("sidecar still exists after empty write: err=%v", err)
	}
	if got := readSessionModel("s2"); got != "" {
		t.Fatalf("readSessionModel after removal = %q, want empty", got)
	}
}

func TestModelsHandlerSuccess(t *testing.T) {
	h := modelsHandler(func() ([]string, error) {
		return []string{"model-a", "model-b"}, nil
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/models", nil)
	h(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"model-a"`) || !strings.Contains(rr.Body.String(), `"model-b"`) {
		t.Fatalf("body = %s, want models listed", rr.Body.String())
	}
}

func TestModelsHandlerError(t *testing.T) {
	h := modelsHandler(func() ([]string, error) {
		return nil, errors.New("boom")
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/models", nil)
	h(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rr.Code, rr.Body.String())
	}
}

func TestSessionCreateHandlerWritesModelSidecar(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())

	h := sessionCreateHandler()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/session", strings.NewReader(`{"id":"x","model":"m"}`))
	h(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if got := readSessionModel("x"); got != "m" {
		t.Fatalf("readSessionModel(x) = %q, want %q", got, "m")
	}
}
