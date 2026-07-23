package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"gophermind/internal/session"
)

func TestSessionCreateHandlerGeneratesID(t *testing.T) {
	h := sessionCreateHandler()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/session", nil)
	h(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("response not JSON: %v (body=%s)", err, rr.Body.String())
	}
	if got.ID == "" {
		t.Errorf("expected a generated id, got empty")
	}
}

func TestSessionCreateHandlerEchoesValidID(t *testing.T) {
	h := sessionCreateHandler()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/session", strings.NewReader(`{"id":"my-session_1"}`))
	h(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if got.ID != "my-session_1" {
		t.Errorf("id = %q, want %q", got.ID, "my-session_1")
	}
}

func TestSessionCreateHandlerRejectsInvalidID(t *testing.T) {
	h := sessionCreateHandler()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/session", strings.NewReader(`{"id":"../escape"}`))
	h(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestSessionStreamHandlerForwardsTaskAndEmitsFrames(t *testing.T) {
	var gotID, gotTask string
	turn := func(_ context.Context, id, task string, emit func(event, data string) error) error {
		gotID, gotTask = id, task
		if err := emit("token", "hello "); err != nil {
			return err
		}
		if err := emit("assistant", "hello world"); err != nil {
			return err
		}
		return nil
	}
	locks := newSessionLocks()
	h := sessionStreamHandler(turn, locks)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/session/abc/stream", strings.NewReader("say hi"))
	req.SetPathValue("id", "abc")
	h(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if gotID != "abc" {
		t.Errorf("id forwarded = %q, want %q", gotID, "abc")
	}
	if gotTask != "say hi" {
		t.Errorf("task forwarded = %q, want %q", gotTask, "say hi")
	}
	body := rr.Body.String()
	if !strings.Contains(body, "event: token\ndata: hello \n\n") {
		t.Errorf("missing token frame:\n%s", body)
	}
	if !strings.Contains(body, "event: assistant\ndata: hello world\n\n") {
		t.Errorf("missing assistant frame:\n%s", body)
	}
	if !strings.HasSuffix(body, "event: done\ndata: \n\n") {
		t.Errorf("stream must end with terminal done event:\n%s", body)
	}
}

func TestSessionStreamHandlerRejectsInvalidID(t *testing.T) {
	turn := func(context.Context, string, string, func(string, string) error) error {
		t.Fatal("turn should not be called for an invalid id")
		return nil
	}
	locks := newSessionLocks()
	h := sessionStreamHandler(turn, locks)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/session/../stream", strings.NewReader("x"))
	req.SetPathValue("id", "..")
	h(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestSessionStreamHandlerEmitsErrorEventOnTurnFailure(t *testing.T) {
	turn := func(context.Context, string, string, func(string, string) error) error {
		return errors.New("boom")
	}
	locks := newSessionLocks()
	h := sessionStreamHandler(turn, locks)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/session/abc/stream", strings.NewReader("x"))
	req.SetPathValue("id", "abc")
	h(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Errorf("expected an error event on turn failure:\n%s", body)
	}
	if !strings.HasSuffix(body, "event: done\ndata: \n\n") {
		t.Errorf("stream must still end with terminal done event:\n%s", body)
	}
}

func TestSessionStreamHandlerConcurrency(t *testing.T) {
	block := make(chan struct{})
	started := make(chan struct{}, 1)
	turn := func(ctx context.Context, id, task string, emit func(event, data string) error) error {
		started <- struct{}{}
		<-block
		return nil
	}
	locks := newSessionLocks()
	h := sessionStreamHandler(turn, locks)

	// First request to id "busy" holds the lock until we release `block`.
	done := make(chan struct{})
	go func() {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/session/busy/stream", strings.NewReader("first"))
		req.SetPathValue("id", "busy")
		h(rr, req)
		close(done)
	}()
	<-started

	// Second request to the SAME id must be rejected with 409 while the first
	// is still in flight.
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/session/busy/stream", strings.NewReader("second"))
	req2.SetPathValue("id", "busy")
	h(rr2, req2)
	if rr2.Code != http.StatusConflict {
		t.Errorf("concurrent same-id status = %d, want 409; body=%s", rr2.Code, rr2.Body.String())
	}

	// A DISTINCT id proceeds in parallel (its own turn blocks separately, so use
	// a turn that returns immediately here to keep the test simple).
	var gotOtherID string
	otherTurn := func(_ context.Context, id, task string, emit func(event, data string) error) error {
		gotOtherID = id
		return nil
	}
	h2 := sessionStreamHandler(otherTurn, locks)
	rr3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, "/session/other/stream", strings.NewReader("third"))
	req3.SetPathValue("id", "other")
	h2(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Errorf("distinct-id status = %d, want 200; body=%s", rr3.Code, rr3.Body.String())
	}
	if gotOtherID != "other" {
		t.Errorf("distinct id forwarded = %q, want %q", gotOtherID, "other")
	}

	close(block)
	<-done
}

func TestSessionLocksTryAcquireRelease(t *testing.T) {
	locks := newSessionLocks()
	if !locks.TryAcquire("a") {
		t.Fatal("first TryAcquire should succeed")
	}
	if locks.TryAcquire("a") {
		t.Error("second TryAcquire on held id should fail")
	}
	locks.Release("a")
	if !locks.TryAcquire("a") {
		t.Error("TryAcquire after Release should succeed")
	}
}

func TestSessionLocksConcurrentDistinctIDs(t *testing.T) {
	locks := newSessionLocks()
	var wg sync.WaitGroup
	results := make([]bool, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := string(rune('a' + i))
			results[i] = locks.TryAcquire(id)
		}(i)
	}
	wg.Wait()
	for i, ok := range results {
		if !ok {
			t.Errorf("distinct id %d should acquire", i)
		}
	}
}

func TestSessionListHandlerReturnsInjectedList(t *testing.T) {
	list := func() ([]session.Info, error) {
		return []session.Info{{ID: "one"}, {ID: "two"}}, nil
	}
	h := sessionListHandler(list)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/session", nil)
	h(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var got []session.Info
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("response not JSON: %v (body=%s)", err, rr.Body.String())
	}
	if len(got) != 2 || got[0].ID != "one" || got[1].ID != "two" {
		t.Errorf("got = %+v, want ids [one two]", got)
	}
}

func TestSessionListHandlerRejectsNonGet(t *testing.T) {
	h := sessionListHandler(func() ([]session.Info, error) { return nil, nil })
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/session", nil)
	h(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
}

func TestSessionDeleteHandlerCallsRemove(t *testing.T) {
	var gotID string
	remove := func(id string) error {
		gotID = id
		return nil
	}
	h := sessionDeleteHandler(remove)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/session/abc", nil)
	req.SetPathValue("id", "abc")
	h(rr, req)

	if rr.Code != http.StatusNoContent && rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 or 204; body=%s", rr.Code, rr.Body.String())
	}
	if gotID != "abc" {
		t.Errorf("remove called with id = %q, want %q", gotID, "abc")
	}
}

func TestSessionDeleteHandlerPropagatesRemoveError(t *testing.T) {
	remove := func(string) error { return errors.New("not found") }
	h := sessionDeleteHandler(remove)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/session/abc", nil)
	req.SetPathValue("id", "abc")
	h(rr, req)

	if rr.Code == http.StatusOK || rr.Code == http.StatusNoContent {
		t.Errorf("remove error should not report success, got %d", rr.Code)
	}
}

func TestSessionRenameHandlerCallsSetName(t *testing.T) {
	var gotID, gotName string
	setName := func(id, name string) error {
		gotID, gotName = id, name
		return nil
	}
	h := sessionRenameHandler(setName)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/session/abc", strings.NewReader(`{"name":"My Project"}`))
	req.SetPathValue("id", "abc")
	h(rr, req)

	if rr.Code != http.StatusNoContent && rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200/204; body=%s", rr.Code, rr.Body.String())
	}
	if gotID != "abc" || gotName != "My Project" {
		t.Errorf("SetName(%q,%q), want (abc, My Project)", gotID, gotName)
	}
}

func TestSessionRenameHandlerRejectsWrongMethod(t *testing.T) {
	h := sessionRenameHandler(func(string, string) error { return nil })
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/session/abc", nil)
	req.SetPathValue("id", "abc")
	h(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
}

func TestSessionRenameHandlerRejectsBadID(t *testing.T) {
	h := sessionRenameHandler(func(string, string) error { return nil })
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/session/../escape", strings.NewReader(`{"name":"x"}`))
	req.SetPathValue("id", "../escape")
	h(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestSessionRenameHandlerRejectsBadJSON(t *testing.T) {
	h := sessionRenameHandler(func(string, string) error { return nil })
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/session/abc", strings.NewReader(`not json`))
	req.SetPathValue("id", "abc")
	h(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestSessionRenameHandlerAllowsEmptyNameToClear(t *testing.T) {
	var gotName = "unset"
	h := sessionRenameHandler(func(_, name string) error { gotName = name; return nil })
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/session/abc", strings.NewReader(`{"name":""}`))
	req.SetPathValue("id", "abc")
	h(rr, req)
	if rr.Code != http.StatusNoContent && rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200/204", rr.Code)
	}
	if gotName != "" {
		t.Errorf("SetName name = %q, want empty (clear)", gotName)
	}
}
