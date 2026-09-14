package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestAuth_BearerTokenOnEveryMethod covers "Bearer token auth is applied
// to all requests" across a representative request of each HTTP verb this
// client uses, plus the SSE path (a distinct code path from do/attempt).
func TestAuth_BearerTokenOnEveryMethod(t *testing.T) {
	var gotAuth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = append(gotAuth, r.Header.Get("Authorization"))
		switch {
		case r.URL.Path == "/session" && r.Method == http.MethodPost:
			w.Write([]byte(`{"id":"s1"}`))
		case r.URL.Path == "/session" && r.Method == http.MethodGet:
			w.Write([]byte(`[]`))
		case strings.HasPrefix(r.URL.Path, "/session/") && r.Method == http.MethodPatch:
			w.WriteHeader(200)
		case strings.HasPrefix(r.URL.Path, "/session/") && r.Method == http.MethodDelete:
			w.WriteHeader(200)
		case r.URL.Path == "/run/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: done\ndata: \n\n")
		default:
			w.WriteHeader(200)
		}
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Token: "secret-token"})
	ctx := context.Background()

	c.CreateSession(ctx, CreateSessionOptions{})
	c.ListSessions(ctx)
	c.RenameSession(ctx, "s1", "x")
	c.DeleteSession(ctx, "s1")
	stream, err := c.RunStream(ctx, "go")
	if err != nil {
		t.Fatalf("RunStream: %v", err)
	}
	stream.Close()

	if len(gotAuth) < 5 {
		t.Fatalf("expected at least 5 requests, got %d", len(gotAuth))
	}
	for i, h := range gotAuth {
		if h != "Bearer secret-token" {
			t.Errorf("request %d Authorization = %q, want %q", i, h, "Bearer secret-token")
		}
	}
}

// TestTimeout_Enforced covers "Request timeout is configurable and
// enforced": a client whose Timeout is shorter than the server's response
// delay must fail promptly, not hang until the server eventually responds.
func TestTimeout_Enforced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Token: "t", Timeout: 100 * time.Millisecond, Retry: RetryPolicy{MaxAttempts: 1}})
	start := time.Now()
	_, err := c.ListSessions(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	if elapsed > 1*time.Second {
		t.Errorf("call took %v, want well under the server's 2s delay (timeout should have fired)", elapsed)
	}
}

// TestRetry_TransientServerErrorsAreRetried covers "transient errors (5xx,
// network) are retried with backoff": a server failing twice with 503 then
// succeeding must still produce a successful response.
func TestRetry_TransientServerErrorsAreRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Token: "t", Retry: RetryPolicy{MaxAttempts: 4, BaseDelay: 1 * time.Millisecond, MaxDelay: 5 * time.Millisecond}})
	_, err := c.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("server received %d calls, want 3 (2 failures + 1 success)", got)
	}
}

// TestRetry_ExhaustsAttemptsAndReturnsError covers the other side: when
// every attempt fails, the client gives up after MaxAttempts and returns
// an error naming the last failure, rather than retrying forever.
func TestRetry_ExhaustsAttemptsAndReturnsError(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Token: "t", Retry: RetryPolicy{MaxAttempts: 3, BaseDelay: 1 * time.Millisecond, MaxDelay: 2 * time.Millisecond}})
	_, err := c.ListSessions(context.Background())
	if err == nil {
		t.Fatal("expected an error after exhausting retries")
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("server received %d calls, want exactly MaxAttempts=3", got)
	}
}

// TestRetry_ClientErrorsAreNotRetried covers the boundary the acceptance
// criteria implies: a 4xx is the caller's fault, not transient, and
// retrying an unchanged request would fail identically every time.
func TestRetry_ClientErrorsAreNotRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Token: "t", Retry: RetryPolicy{MaxAttempts: 4, BaseDelay: 1 * time.Millisecond, MaxDelay: 2 * time.Millisecond}})
	_, err := c.ListSessions(context.Background())
	if err == nil {
		t.Fatal("expected an error for a 400 response")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server received %d calls, want exactly 1 (no retry on 4xx)", got)
	}
	var statusErr *StatusError
	if se, ok := err.(*StatusError); ok {
		statusErr = se
	}
	if statusErr == nil || statusErr.StatusCode != http.StatusBadRequest {
		t.Errorf("error = %v, want a *StatusError with StatusCode 400", err)
	}
}

// TestRetry_NetworkErrorIsRetried covers the "network" half of "5xx,
// network" by closing the listener out from under the client after
// accepting the connection, forcing a genuine transport-level failure
// rather than an HTTP status.
func TestRetry_NetworkErrorIsRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			// Hijack and close without responding: simulates a connection
			// reset / network-level failure on the first attempt.
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("ResponseWriter does not support hijacking")
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Fatalf("hijack: %v", err)
			}
			conn.Close()
			return
		}
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Token: "t", Retry: RetryPolicy{MaxAttempts: 3, BaseDelay: 1 * time.Millisecond, MaxDelay: 5 * time.Millisecond}})
	_, err := c.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("server received %d calls, want 2 (1 network failure + 1 success)", got)
	}
}

// TestRetryPolicy_BackoffIsBoundedAndNonNegative is a focused unit test on
// the backoff calculation itself, independent of any HTTP round trip.
func TestRetryPolicy_BackoffIsBoundedAndNonNegative(t *testing.T) {
	p := RetryPolicy{MaxAttempts: 10, BaseDelay: 10 * time.Millisecond, MaxDelay: 100 * time.Millisecond}
	for attempt := 1; attempt <= 10; attempt++ {
		d := p.backoff(attempt)
		if d < 0 {
			t.Errorf("attempt %d: backoff = %v, want >= 0", attempt, d)
		}
		if d > p.MaxDelay {
			t.Errorf("attempt %d: backoff = %v, want <= MaxDelay %v", attempt, d, p.MaxDelay)
		}
	}
}

// --- SSE parser tests ---

// buildSSE writes raw SSE-formatted frames for the test server to serve,
// matching gophermind-lib/serve's writeSSEEvent output exactly (an
// "event: <type>\n" line when a type is given, one "data: <line>\n" line
// per line of data, then a blank line).
func buildSSE(frames ...[2]string) string {
	var b strings.Builder
	for _, f := range frames {
		event, data := f[0], f[1]
		if event != "" {
			b.WriteString("event: " + event + "\n")
		}
		for _, line := range strings.Split(data, "\n") {
			b.WriteString("data: " + line + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// TestEventStream_ParsesAllContractEventTypes covers the acceptance
// criterion literally: every named event type parses with the right Type
// and Data.
func TestEventStream_ParsesAllContractEventTypes(t *testing.T) {
	want := []string{
		"token", "assistant", "tool_call", "tool_result",
		"usage", "approval-needed", "model-switched", "error", "done",
	}
	var frames [][2]string
	for _, ev := range want {
		frames = append(frames, [2]string{ev, ev + "-payload"})
	}
	body := buildSSE(frames...)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, body)
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Token: "t"})
	stream, err := c.RunStream(context.Background(), "go")
	if err != nil {
		t.Fatalf("RunStream: %v", err)
	}
	defer stream.Close()

	var got []Event
	for {
		ev, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		got = append(got, ev)
	}

	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d: %+v", len(got), len(want), got)
	}
	for i, ev := range got {
		if ev.Type != want[i] {
			t.Errorf("event %d: Type = %q, want %q", i, ev.Type, want[i])
		}
		if ev.Data != want[i]+"-payload" {
			t.Errorf("event %d: Data = %q, want %q", i, ev.Data, want[i]+"-payload")
		}
	}
}

// TestEventStream_MultiLineDataJoinedWithNewline covers SSE's multi-line
// data: field, which gophermind-server's writeSSEEvent produces whenever
// event data itself contains a newline (e.g. multi-line assistant prose).
func TestEventStream_MultiLineDataJoinedWithNewline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: assistant\ndata: line one\ndata: line two\ndata: line three\n\n")
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Token: "t"})
	stream, err := c.RunStream(context.Background(), "go")
	if err != nil {
		t.Fatalf("RunStream: %v", err)
	}
	defer stream.Close()

	ev, err := stream.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	want := "line one\nline two\nline three"
	if ev.Data != want {
		t.Errorf("Data = %q, want %q", ev.Data, want)
	}
}

// TestEventStream_TypedDecoders covers decoding the JSON-payload event
// types (tool_call, tool_result, usage, approval-needed) into their
// contract structs.
func TestEventStream_TypedDecoders(t *testing.T) {
	body := buildSSE(
		[2]string{"tool_call", `{"name":"run_shell","args":"{\"command\":\"echo hi\"}"}`},
		[2]string{"tool_result", `{"name":"run_shell","text":"hi"}`},
		[2]string{"usage", `{"PromptTokens":10,"CompletionTokens":5,"TotalTokens":15,"CostUSD":0.001}`},
		[2]string{"approval-needed", `{"approval_id":"a1","tool":"run_shell","args":"{}"}`},
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, body)
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Token: "t"})
	stream, err := c.RunStream(context.Background(), "go")
	if err != nil {
		t.Fatalf("RunStream: %v", err)
	}
	defer stream.Close()

	ev, _ := stream.Next()
	tc, err := ev.ToolCall()
	if err != nil || tc.Name != "run_shell" {
		t.Errorf("ToolCall() = %+v, err %v", tc, err)
	}

	ev, _ = stream.Next()
	tr, err := ev.ToolResult()
	if err != nil || tr.Text != "hi" {
		t.Errorf("ToolResult() = %+v, err %v", tr, err)
	}

	ev, _ = stream.Next()
	u, err := ev.Usage()
	if err != nil || u.PromptTokens != 10 || u.TotalTokens != 15 {
		t.Errorf("Usage() = %+v, err %v", u, err)
	}

	ev, _ = stream.Next()
	an, err := ev.ApprovalNeeded()
	if err != nil || an.ApprovalID != "a1" || an.Tool != "run_shell" {
		t.Errorf("ApprovalNeeded() = %+v, err %v", an, err)
	}
}

// TestEventStream_TrailingFrameWithoutBlankLineIsFlushed covers a
// connection that ends immediately after a frame's last data: line, with
// no terminating blank line -- the last event must not be silently
// dropped.
func TestEventStream_TrailingFrameWithoutBlankLineIsFlushed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: done\ndata: finished") // no trailing \n\n
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Token: "t"})
	stream, err := c.RunStream(context.Background(), "go")
	if err != nil {
		t.Fatalf("RunStream: %v", err)
	}
	defer stream.Close()

	ev, err := stream.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if ev.Type != "done" || ev.Data != "finished" {
		t.Errorf("ev = %+v, want {done, finished}", ev)
	}
	if _, err := stream.Next(); err != io.EOF {
		t.Errorf("second Next() err = %v, want io.EOF", err)
	}
}

// TestEventStream_NonSSESuccessResponseIsNotConfusedForFrames is a
// negative case: openSSE must surface a non-2xx status as an error before
// the caller ever starts parsing, rather than trying to interpret an HTML
// error page as SSE frames.
func TestEventStream_NonSSESuccessResponseIsNotConfusedForFrames(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "unauthorized")
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Token: "wrong"})
	_, err := c.RunStream(context.Background(), "go")
	if err == nil {
		t.Fatal("expected an error for a 401 response")
	}
}

func TestNew_DefaultsApplied(t *testing.T) {
	c := New(Config{BaseURL: "http://example.invalid", Token: "t"})
	if c.http.Timeout != DefaultTimeout {
		t.Errorf("Timeout = %v, want default %v", c.http.Timeout, DefaultTimeout)
	}
	if c.retry != DefaultRetryPolicy {
		t.Errorf("retry = %+v, want default %+v", c.retry, DefaultRetryPolicy)
	}
}

func TestNew_TrimsTrailingSlashFromBaseURL(t *testing.T) {
	c := New(Config{BaseURL: "http://example.invalid/", Token: "t"})
	if c.baseURL != "http://example.invalid" {
		t.Errorf("baseURL = %q, want trailing slash trimmed", c.baseURL)
	}
}

// TestHealthy covers the unauthenticated health check used for a
// connection-status indicator.
func TestHealthy(t *testing.T) {
	up := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if up {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Token: "t"})
	if !c.Healthy(context.Background()) {
		t.Error("Healthy() = false, want true")
	}
	up = false
	if c.Healthy(context.Background()) {
		t.Error("Healthy() = true, want false")
	}
}

// TestModelsAndCatalogueAndPipelineAndSkills is a lighter-weight sanity
// pass over the remaining endpoint methods not otherwise exercised above
// (auth/retry/timeout/SSE already cover the cross-cutting behavior; this
// just confirms each method builds the right request and decodes the
// right response shape).
func TestModelsAndCatalogueAndPipelineAndSkills(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/models" && r.Method == http.MethodGet:
			io.WriteString(w, `["m1","m2"]`)
		case r.URL.Path == "/models/catalogue":
			io.WriteString(w, `{"entries":[{"ID":"m1"}]}`)
		case r.URL.Path == "/models/settings" && r.Method == http.MethodGet:
			io.WriteString(w, `{}`)
		case r.URL.Path == "/models/settings" && r.Method == http.MethodPatch:
			w.WriteHeader(200)
		case r.URL.Path == "/skills" && r.Method == http.MethodGet:
			io.WriteString(w, `{"sources":[],"skills":[]}`)
		case r.URL.Path == "/skills" && r.Method == http.MethodPatch:
			w.WriteHeader(200)
		case r.URL.Path == "/skills/sources" && r.Method == http.MethodPost:
			w.WriteHeader(200)
		case strings.HasPrefix(r.URL.Path, "/skills/sources/") && r.Method == http.MethodDelete:
			w.WriteHeader(200)
		case r.URL.Path == "/pipeline/state":
			io.WriteString(w, `{"tasks":[],"generated_at":"2026-01-01T00:00:00Z"}`)
		case r.URL.Path == "/pipeline/report":
			io.WriteString(w, `{}`)
		case r.URL.Path == "/session/s1/messages":
			io.WriteString(w, `[]`)
		case r.URL.Path == "/session/s1/approve":
			w.WriteHeader(200)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Token: "t"})
	ctx := context.Background()

	if models, err := c.ListModels(ctx); err != nil || len(models) != 2 {
		t.Errorf("ListModels: %v, %v", models, err)
	}
	if entries, err := c.Catalogue(ctx); err != nil || len(entries) != 1 {
		t.Errorf("Catalogue: %v, %v", entries, err)
	}
	if _, err := c.ModelSettings(ctx); err != nil {
		t.Errorf("ModelSettings: %v", err)
	}
	if err := c.PatchModelSettings(ctx, map[string]any{"x": 1}); err != nil {
		t.Errorf("PatchModelSettings: %v", err)
	}
	if _, _, err := c.SkillsList(ctx); err != nil {
		t.Errorf("SkillsList: %v", err)
	}
	if err := c.SetSkillEnabled(ctx, "repo:skill", true); err != nil {
		t.Errorf("SetSkillEnabled: %v", err)
	}
	if err := c.AddSkillSource(ctx, "https://example.com/x", ""); err != nil {
		t.Errorf("AddSkillSource: %v", err)
	}
	if err := c.RemoveSkillSource(ctx, "abc"); err != nil {
		t.Errorf("RemoveSkillSource: %v", err)
	}
	if tasks, _, err := c.PipelineState(ctx); err != nil || tasks == nil {
		t.Errorf("PipelineState: %v, %v", tasks, err)
	}
	if _, err := c.PipelineReport(ctx); err != nil {
		t.Errorf("PipelineReport: %v", err)
	}
	if _, err := c.SessionMessages(ctx, "s1"); err != nil {
		t.Errorf("SessionMessages: %v", err)
	}
	if err := c.Approve(ctx, "s1", "a1", true); err != nil {
		t.Errorf("Approve: %v", err)
	}
}

func TestRun_ReturnsResponseBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		io.WriteString(w, "echo:"+string(body))
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Token: "t"})
	got, err := c.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "echo:hello" {
		t.Errorf("got %q", got)
	}
}

func TestPipelineEvents_StreamsTaskStatus(t *testing.T) {
	body := buildSSE([2]string{"task-status", `{"id":"01-01","status":"in_progress","wave":1}`})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, body)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Token: "t"})
	stream, err := c.PipelineEvents(context.Background())
	if err != nil {
		t.Fatalf("PipelineEvents: %v", err)
	}
	defer stream.Close()
	ev, err := stream.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	ts, err := ev.TaskStatus()
	if err != nil || ts.ID != "01-01" || ts.Status != "in_progress" {
		t.Errorf("TaskStatus() = %+v, err %v", ts, err)
	}
}
