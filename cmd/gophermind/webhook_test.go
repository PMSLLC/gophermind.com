package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebhookRunsTask(t *testing.T) {
	var gotTask string
	h := webhookHandler(func(_ context.Context, task string) (string, error) {
		gotTask = task
		return "result:" + task, nil
	}, "")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/run", strings.NewReader("do the thing"))
	h(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if gotTask != "do the thing" {
		t.Errorf("task forwarded = %q", gotTask)
	}
	if !strings.Contains(rr.Body.String(), "result:do the thing") {
		t.Errorf("body = %q", rr.Body.String())
	}
}

func TestWebhookParsesJSONBody(t *testing.T) {
	h := webhookHandler(func(_ context.Context, task string) (string, error) { return task, nil }, "")
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/run", strings.NewReader(`{"task":"json task"}`))
	req.Header.Set("Content-Type", "application/json")
	h(rr, req)
	if !strings.Contains(rr.Body.String(), "json task") {
		t.Errorf("json task not parsed: %q", rr.Body.String())
	}
}

func TestWebhookRejectsNonPost(t *testing.T) {
	h := webhookHandler(func(context.Context, string) (string, error) { return "", nil }, "")
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodGet, "/run", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d, want 405", rr.Code)
	}
}

func TestWebhookEmptyBody(t *testing.T) {
	h := webhookHandler(func(context.Context, string) (string, error) { return "", nil }, "")
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/run", strings.NewReader("")))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("empty body status = %d, want 400", rr.Code)
	}
}

func TestWebhookTokenAuth(t *testing.T) {
	h := webhookHandler(func(context.Context, string) (string, error) { return "ok", nil }, "secret")

	// no token -> 401
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/run", strings.NewReader("t")))
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("missing token status = %d, want 401", rr.Code)
	}
	// correct token -> 200
	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/run", strings.NewReader("t"))
	req.Header.Set("Authorization", "Bearer secret")
	h(rr, req)
	if rr.Code != 200 {
		t.Errorf("authorized status = %d, want 200", rr.Code)
	}
}

func TestWebhookRunError(t *testing.T) {
	h := webhookHandler(func(context.Context, string) (string, error) { return "", errors.New("boom") }, "")
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/run", strings.NewReader("t")))
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("error status = %d, want 500", rr.Code)
	}
}
