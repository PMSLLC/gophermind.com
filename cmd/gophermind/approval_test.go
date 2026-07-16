package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- registry ---

func TestApprovalRegistryRegisterResolveTrue(t *testing.T) {
	reg := newApprovalRegistry()
	ch := reg.register("a1")
	if !reg.resolve("a1", true) {
		t.Fatal("resolve on registered id should report found")
	}
	select {
	case got := <-ch:
		if !got {
			t.Errorf("channel delivered %v, want true", got)
		}
	default:
		t.Fatal("expected a value on the channel")
	}
}

func TestApprovalRegistryResolveFalse(t *testing.T) {
	reg := newApprovalRegistry()
	ch := reg.register("a1")
	if !reg.resolve("a1", false) {
		t.Fatal("resolve on registered id should report found")
	}
	select {
	case got := <-ch:
		if got {
			t.Errorf("channel delivered %v, want false", got)
		}
	default:
		t.Fatal("expected a value on the channel")
	}
}

func TestApprovalRegistryResolveUnknownID(t *testing.T) {
	reg := newApprovalRegistry()
	if reg.resolve("nope", true) {
		t.Error("resolve on unknown id should report not found")
	}
}

func TestApprovalRegistryCancelRemoves(t *testing.T) {
	reg := newApprovalRegistry()
	reg.register("a1")
	reg.cancel("a1")
	if reg.resolve("a1", true) {
		t.Error("resolve after cancel should report not found")
	}
}

func TestApprovalRegistryDoubleResolveSecondFails(t *testing.T) {
	reg := newApprovalRegistry()
	reg.register("a1")
	if !reg.resolve("a1", true) {
		t.Fatal("first resolve should succeed")
	}
	if reg.resolve("a1", true) {
		t.Error("second resolve on the same id should report not found")
	}
}

func TestApprovalRegistryConcurrentRegisterResolve(t *testing.T) {
	reg := newApprovalRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		id := string(rune('a' + i%26))
		wg.Add(2)
		go func(id string) {
			defer wg.Done()
			reg.register(id)
		}(id)
		go func(id string) {
			defer wg.Done()
			reg.resolve(id, true)
		}(id)
	}
	wg.Wait()
}

// --- gate ---

// captureEmit records every emitted (event, data) frame.
type captureEmit struct {
	mu     sync.Mutex
	events []string
	datas  []string
}

func (c *captureEmit) emit(event, data string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
	c.datas = append(c.datas, data)
	return nil
}

func (c *captureEmit) only() (event, data string, n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n = len(c.events)
	if n == 0 {
		return "", "", n
	}
	return c.events[0], c.datas[0], n
}

func idSeq(ids ...string) func() string {
	i := 0
	return func() string {
		id := ids[i]
		i++
		return id
	}
}

func TestRemoteApprovalGateEmitsOneFrameAndApproves(t *testing.T) {
	reg := newApprovalRegistry()
	rec := &captureEmit{}
	var chOut chan bool
	gate := remoteApprovalGate(reg, context.Background(), time.Minute, rec.emit, func() string { return "id-1" })

	go func() {
		// Wait until the id is registered, then resolve it true.
		for {
			reg.mu.Lock()
			ch, ok := reg.pending["id-1"]
			reg.mu.Unlock()
			if ok {
				chOut = ch
				reg.resolve("id-1", true)
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()

	got := gate("run_shell", `{"command":"ls"}`)
	if !got {
		t.Errorf("gate returned %v, want true", got)
	}
	_ = chOut

	event, data, n := rec.only()
	if n != 1 {
		t.Fatalf("emitted %d frames, want exactly 1", n)
	}
	if event != "approval-needed" {
		t.Errorf("event = %q, want approval-needed", event)
	}
	var payload struct {
		ApprovalID string `json:"approval_id"`
		Tool       string `json:"tool"`
		Args       string `json:"args"`
	}
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		t.Fatalf("frame data not JSON: %v (data=%s)", err, data)
	}
	if payload.ApprovalID != "id-1" {
		t.Errorf("approval_id = %q, want id-1", payload.ApprovalID)
	}
	if payload.Tool != "run_shell" {
		t.Errorf("tool = %q, want run_shell", payload.Tool)
	}
	if payload.Args != `{"command":"ls"}` {
		t.Errorf("args = %q, want %q", payload.Args, `{"command":"ls"}`)
	}
}

func TestRemoteApprovalGateDenies(t *testing.T) {
	reg := newApprovalRegistry()
	rec := &captureEmit{}
	gate := remoteApprovalGate(reg, context.Background(), time.Minute, rec.emit, idSeq("id-2"))

	go func() {
		for {
			if reg.resolve("id-2", false) {
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()

	if got := gate("write_file", `{}`); got {
		t.Errorf("gate returned %v, want false", got)
	}
}

func TestRemoteApprovalGateTimeoutDenies(t *testing.T) {
	reg := newApprovalRegistry()
	rec := &captureEmit{}
	gate := remoteApprovalGate(reg, context.Background(), 20*time.Millisecond, rec.emit, idSeq("id-3"))

	start := time.Now()
	got := gate("write_file", `{}`)
	elapsed := time.Since(start)

	if got {
		t.Error("gate should auto-deny on timeout")
	}
	if elapsed > 2*time.Second {
		t.Errorf("gate took %v, expected to return promptly after the tiny timeout", elapsed)
	}
	// The pending entry must be cleaned up so a late decision cannot leak
	// through / block forever.
	if reg.resolve("id-3", true) {
		t.Error("expected the pending approval to be cancelled after timeout")
	}
	_, _, n := rec.only()
	if n != 1 {
		t.Fatalf("emitted %d frames, want exactly 1", n)
	}
}

func TestRemoteApprovalGateCtxCancelDenies(t *testing.T) {
	reg := newApprovalRegistry()
	rec := &captureEmit{}
	ctx, cancel := context.WithCancel(context.Background())
	gate := remoteApprovalGate(reg, ctx, time.Minute, rec.emit, idSeq("id-4"))

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	got := gate("write_file", `{}`)
	if got {
		t.Error("gate should deny on ctx cancellation")
	}

	event, data, n := rec.only()
	if n != 1 {
		t.Fatalf("emitted %d frames, want exactly 1", n)
	}
	if event != "approval-needed" {
		t.Errorf("event = %q, want approval-needed", event)
	}
	var payload struct {
		ApprovalID string `json:"approval_id"`
	}
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		t.Fatalf("frame data not JSON: %v", err)
	}
	if payload.ApprovalID != "id-4" {
		t.Errorf("approval_id = %q, want id-4", payload.ApprovalID)
	}
}

// --- approve handler ---

func TestSessionApproveHandlerResolvesPending(t *testing.T) {
	reg := newApprovalRegistry()
	ch := reg.register("a1")
	h := sessionApproveHandler(reg)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/session/s1/approve", strings.NewReader(`{"approval_id":"a1","approved":true}`))
	req.SetPathValue("id", "s1")
	h(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if !got.OK {
		t.Errorf("ok = %v, want true", got.OK)
	}
	select {
	case v := <-ch:
		if !v {
			t.Errorf("channel delivered %v, want true", v)
		}
	default:
		t.Fatal("expected the gate's channel to receive the decision")
	}
}

func TestSessionApproveHandlerUnknownID(t *testing.T) {
	reg := newApprovalRegistry()
	h := sessionApproveHandler(reg)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/session/s1/approve", strings.NewReader(`{"approval_id":"nope","approved":true}`))
	req.SetPathValue("id", "s1")
	h(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body=%s", rr.Code, rr.Body.String())
	}
}

func TestSessionApproveHandlerBadJSON(t *testing.T) {
	reg := newApprovalRegistry()
	h := sessionApproveHandler(reg)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/session/s1/approve", strings.NewReader(`not json`))
	req.SetPathValue("id", "s1")
	h(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestSessionApproveHandlerRejectsNonPost(t *testing.T) {
	reg := newApprovalRegistry()
	h := sessionApproveHandler(reg)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/session/s1/approve", nil)
	req.SetPathValue("id", "s1")
	h(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405; body=%s", rr.Code, rr.Body.String())
	}
}
