package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSessionMessagesHandlerFound(t *testing.T) {
	want := []json.RawMessage{
		json.RawMessage(`{"role":"user","content":"hi"}`),
		json.RawMessage(`{"role":"assistant","content":"hello"}`),
	}
	load := func(id string) ([]json.RawMessage, bool, error) {
		if id != "abc" {
			t.Fatalf("load called with id = %q, want %q", id, "abc")
		}
		return want, true, nil
	}
	h := sessionMessagesHandler(load)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/session/abc/messages", nil)
	req.SetPathValue("id", "abc")
	h(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var got []json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("response not JSON: %v (body=%s)", err, rr.Body.String())
	}
	if len(got) != 2 {
		t.Fatalf("got %d messages, want 2 (body=%s)", len(got), rr.Body.String())
	}
}

func TestSessionMessagesHandlerNotFound(t *testing.T) {
	load := func(id string) ([]json.RawMessage, bool, error) {
		return nil, false, nil
	}
	h := sessionMessagesHandler(load)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/session/missing/messages", nil)
	req.SetPathValue("id", "missing")
	h(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("response not JSON: %v (body=%s)", err, rr.Body.String())
	}
	if got.Error == "" {
		t.Errorf("expected a non-empty error message, body=%s", rr.Body.String())
	}
}

func TestSessionMessagesHandlerBadID(t *testing.T) {
	load := func(id string) ([]json.RawMessage, bool, error) {
		t.Fatal("load should not be called for an invalid id")
		return nil, false, nil
	}
	h := sessionMessagesHandler(load)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/session/../messages", nil)
	req.SetPathValue("id", "..")
	h(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestSessionMessagesHandlerLoadError(t *testing.T) {
	load := func(id string) ([]json.RawMessage, bool, error) {
		return nil, true, errors.New("boom")
	}
	h := sessionMessagesHandler(load)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/session/abc/messages", nil)
	req.SetPathValue("id", "abc")
	h(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body=%s", rr.Code, rr.Body.String())
	}
}
