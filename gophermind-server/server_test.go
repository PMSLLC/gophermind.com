package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gophermind/gophermind-lib/serve"
)

// fakeLLM serves a queue of canned SSE completion bodies (one per POST to
// its chat endpoint, sticking on the last once exhausted) and a fixed
// GET /v1/models response, standing in for a real OpenAI-compatible
// endpoint so buildDeps' Run/Stream/SessionTurn closures exercise a real
// agent.Agent + llm.Client round trip instead of a hand-rolled stub.
func fakeLLM(t *testing.T, bodies []string) *httptest.Server {
	t.Helper()
	var i atomic.Int64
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"fake-model"}]}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		idx := int(i.Add(1)) - 1
		body := bodies[len(bodies)-1]
		if idx < len(bodies) {
			body = bodies[idx]
		}
		_, _ = w.Write([]byte(body))
	}))
}

// sseChunk formats one SSE "data: <json>\n\n" frame for a fake completion
// stream, matching the shape gophermind-lib/llm.Client.Stream parses.
func sseChunk(deltaJSON string) string {
	return "data: " + deltaJSON + "\n\n"
}

// finalAnswerBody is a one-shot completion: plain text, no tool call.
func finalAnswerBody(text string) string {
	return sseChunk(`{"choices":[{"delta":{"content":"`+text+`"}}]}`) +
		sseChunk(`{"choices":[{"delta":{},"finish_reason":"stop"}]}`) +
		"data: [DONE]\n\n"
}

// toolCallBody requests one run_shell tool call (a gated tool, per
// gophermind-lib/safety.Gated), so SessionTurn's RemoteApprovalGate fires
// and emits "approval-needed".
// toolCallBody's reply carries both narration content and a tool call, so
// agent.Agent.Send emits an "assistant" event for the narration (it does so
// only when a reply has tool calls attached -- see loop.go:191-219 -- never
// for a plain final answer's content) in addition to the "tool_call" event
// this drives once dispatched.
func toolCallBody() string {
	return sseChunk(`{"choices":[{"delta":{"content":"checking...","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"run_shell","arguments":"{\"command\":\"echo hi\"}"}}]}}]}`) +
		sseChunk(`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`) +
		"data: [DONE]\n\n"
}

// testServer builds a real serve.NewMux-backed httptest.Server from
// buildDeps against a fake LLM endpoint and a temp workspace root, for
// exercising the actual HTTP contract end to end.
func testServer(t *testing.T, llmBodies []string, token string) (*httptest.Server, string) {
	t.Helper()
	llm := fakeLLM(t, llmBodies)
	t.Cleanup(llm.Close)

	root := t.TempDir()
	cfg := serverConfig{Token: token, LLMEndpoint: llm.URL, Root: root}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	d, _, err := buildDeps(cfg, root, logger)
	if err != nil {
		t.Fatalf("buildDeps: %v", err)
	}
	mux, err := serve.NewMux(d, serve.Options{Token: token})
	if err != nil {
		t.Fatalf("serve.NewMux: %v", err)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, root
}

func authedReq(t *testing.T, method, url, token, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

// TestBuildDeps_Compiles is a smoke test: buildDeps returns without error and
// produces a Deps whose always-present fields are non-nil, so serve.NewMux
// registers every route the contract in webhook.go's doc comment lists,
// rather than silently omitting some because a Deps field was left nil.
func TestBuildDeps_Compiles(t *testing.T) {
	llm := fakeLLM(t, []string{finalAnswerBody("hi")})
	defer llm.Close()
	root := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	d, hub, err := buildDeps(serverConfig{Token: "t", LLMEndpoint: llm.URL, Root: root}, root, logger)
	if err != nil {
		t.Fatalf("buildDeps: %v", err)
	}
	if d.Run == nil || d.Stream == nil || d.SessionTurn == nil || d.Approvals == nil ||
		d.ListModels == nil || d.EndpointModels == nil || d.Pipeline == nil || d.Skills == nil || d.Metrics == nil {
		t.Errorf("buildDeps left a field nil: %+v", d)
	}
	if hub == nil {
		t.Error("buildDeps returned a nil pipeline hub")
	}
}

// TestRoutes_AuthRequired covers "Auth: valid token accepted, invalid token
// rejected with 401" across a representative route from each auth tier.
func TestRoutes_AuthRequired(t *testing.T) {
	srv, _ := testServer(t, []string{finalAnswerBody("hi")}, "right-token")

	cases := []struct {
		method, path string
	}{
		{http.MethodGet, "/session"},
		{http.MethodGet, "/models/catalogue"},
		{http.MethodGet, "/pipeline/state"},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			// No token at all.
			resp, err := http.DefaultClient.Do(authedReq(t, c.method, srv.URL+c.path, "", ""))
			if err != nil {
				t.Fatalf("no-auth request: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("no token: status = %d, want 401", resp.StatusCode)
			}

			// Wrong token.
			resp, err = http.DefaultClient.Do(authedReq(t, c.method, srv.URL+c.path, "wrong-token", ""))
			if err != nil {
				t.Fatalf("wrong-token request: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("wrong token: status = %d, want 401", resp.StatusCode)
			}

			// Right token.
			resp, err = http.DefaultClient.Do(authedReq(t, c.method, srv.URL+c.path, "right-token", ""))
			if err != nil {
				t.Fatalf("right-token request: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusUnauthorized {
				t.Errorf("right token: status = 401, want anything but 401")
			}
		})
	}
}

// TestRoutes_HealthzReadyzMetricsUnauthenticated confirms the probe routes
// carried over from plan 02-01 still work once the full mux (with real
// auth-gated routes alongside them) is wired in.
func TestRoutes_HealthzReadyzMetricsUnauthenticated(t *testing.T) {
	srv, _ := testServer(t, []string{finalAnswerBody("hi")}, "t")
	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200 (no auth required)", path, resp.StatusCode)
		}
	}
}

// TestModelsCatalogue covers "GET /models/catalogue returns model
// catalogue": the fake LLM's /v1/models response should surface as a
// local-endpoint entry via EndpointModels.
func TestModelsCatalogue(t *testing.T) {
	srv, _ := testServer(t, []string{finalAnswerBody("hi")}, "t")
	req := authedReq(t, http.MethodGet, srv.URL+"/models/catalogue", "t", "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /models/catalogue: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "fake-model") {
		t.Errorf("catalogue missing the fake endpoint's model: %s", body)
	}
}

// TestPipelineStateAndEvents covers "GET /pipeline/state and GET
// /pipeline/events work": state responds even with no .planning directory
// yet, and events is a live SSE stream that at least connects and can be
// read from without erroring.
func TestPipelineStateAndEvents(t *testing.T) {
	srv, _ := testServer(t, []string{finalAnswerBody("hi")}, "t")

	resp, err := http.DefaultClient.Do(authedReq(t, http.MethodGet, srv.URL+"/pipeline/state", "t", ""))
	if err != nil {
		t.Fatalf("GET /pipeline/state: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/pipeline/state status = %d, want 200", resp.StatusCode)
	}

	req := authedReq(t, http.MethodGet, srv.URL+"/pipeline/events", "t", "")
	eventsResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /pipeline/events: %v", err)
	}
	defer eventsResp.Body.Close()
	if eventsResp.StatusCode != http.StatusOK {
		t.Fatalf("/pipeline/events status = %d, want 200", eventsResp.StatusCode)
	}
	if ct := eventsResp.Header.Get("Content-Type"); !strings.Contains(ct, "event-stream") {
		t.Errorf("/pipeline/events Content-Type = %q, want event-stream", ct)
	}
}

// TestSessionStream_EmitsTypedSSEEvents covers "POST /session/stream returns
// SSE events (token, assistant, tool_call, tool_result, usage, done)" for
// the plain (no-tool-call) path: token/assistant/usage/done. The
// tool_call/tool_result/approval-needed path is covered separately by
// TestSessionApprove_ResolvesPendingApproval, since it requires a second
// canned LLM response after the tool result.
func TestSessionStream_EmitsTypedSSEEvents(t *testing.T) {
	srv, _ := testServer(t, []string{finalAnswerBody("hello there")}, "t")

	createResp, err := http.DefaultClient.Do(authedReq(t, http.MethodPost, srv.URL+"/session", "t", `{"id":"sess-1"}`))
	if err != nil {
		t.Fatalf("POST /session: %v", err)
	}
	createResp.Body.Close()
	if createResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /session status = %d, want 200", createResp.StatusCode)
	}

	req := authedReq(t, http.MethodPost, srv.URL+"/session/sess-1/stream", "t", "say hi")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /session/sess-1/stream: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	// A plain final answer (no tool call) streams "token" deltas and
	// returns its text directly -- agent.Agent.Send never emits a
	// redundant "assistant" event for it (that event type is reserved for
	// intermediate narration alongside a tool call); see loop.go:191-219.
	// The tool-call/narration/assistant/tool_result path is covered by
	// TestSessionApprove_ResolvesPendingApproval below.
	for _, want := range []string{"event: token", "event: usage", "event: done"} {
		if !strings.Contains(text, want) {
			t.Errorf("SSE stream missing %q:\n%s", want, text)
		}
	}
}

// TestSessionApprove_ResolvesPendingApproval covers "POST /session/approve
// resolves pending approvals" end to end: the fake LLM first requests a
// gated run_shell tool call, the stream blocks on "approval-needed", the
// test resolves it via POST /session/{id}/approve, and the stream completes.
func TestSessionApprove_ResolvesPendingApproval(t *testing.T) {
	srv, _ := testServer(t, []string{toolCallBody(), finalAnswerBody("done")}, "t")

	createResp, err := http.DefaultClient.Do(authedReq(t, http.MethodPost, srv.URL+"/session", "t", `{"id":"sess-2"}`))
	if err != nil {
		t.Fatalf("POST /session: %v", err)
	}
	createResp.Body.Close()

	req := authedReq(t, http.MethodPost, srv.URL+"/session/sess-2/stream", "t", "run something")
	streamResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /session/sess-2/stream: %v", err)
	}
	defer streamResp.Body.Close()

	// Read the whole SSE stream in the background (it stays open, blocked
	// on the pending approval, until we resolve it below), signaling once
	// on approvalID as soon as it's seen and again (via streamDone) once
	// the connection closes, so the test can inspect everything emitted --
	// not just the approval_id -- after approving.
	approvalID := make(chan string, 1)
	streamDone := make(chan string, 1)
	go func() {
		var all strings.Builder
		sc := bufio.NewScanner(streamResp.Body)
		sentID := false
		for sc.Scan() {
			line := sc.Text()
			all.WriteString(line)
			all.WriteString("\n")
			if sentID || !strings.HasPrefix(line, "data: ") {
				continue
			}
			var payload struct {
				ApprovalID string `json:"approval_id"`
			}
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload) == nil && payload.ApprovalID != "" {
				approvalID <- payload.ApprovalID
				sentID = true
			}
		}
		if !sentID {
			close(approvalID)
		}
		streamDone <- all.String()
	}()

	var id string
	select {
	case id = <-approvalID:
		if id == "" {
			t.Fatal("stream closed before an approval_id was seen")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for approval-needed")
	}

	approveBody := fmt.Sprintf(`{"approval_id":%q,"approved":true}`, id)
	approveResp, err := http.DefaultClient.Do(authedReq(t, http.MethodPost, srv.URL+"/session/sess-2/approve", "t", approveBody))
	if err != nil {
		t.Fatalf("POST /session/sess-2/approve: %v", err)
	}
	defer approveResp.Body.Close()
	if approveResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(approveResp.Body)
		t.Fatalf("approve status = %d, want 200: %s", approveResp.StatusCode, b)
	}

	var full string
	select {
	case full = <-streamDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the stream to complete after approval")
	}

	// The full trip: narration ("assistant", from toolCallBody's content
	// alongside its tool call), the gated call itself ("tool_call"), the
	// approval gate ("approval-needed", already consumed above), its
	// result ("tool_result"), and the second (post-approval) LLM response
	// completing the turn ("done"). Between them this test and
	// TestSessionStream_EmitsTypedSSEEvents cover every SSE event type the
	// acceptance criteria lists: token, assistant, tool_call, tool_result,
	// usage, done.
	for _, want := range []string{"event: assistant", "event: tool_call", "event: tool_result", "event: done"} {
		if !strings.Contains(full, want) {
			t.Errorf("stream missing %q after approval:\n%s", want, full)
		}
	}
}

// TestRateLimiting covers "Rate limiting: 429 returned when limit exceeded".
// GOPHERMIND_SERVE_RATE controls the shared limiter (see
// gophermind-lib/serve's serveRateLimiter); set very low so a handful of
// requests trips it deterministically and quickly.
func TestRateLimiting(t *testing.T) {
	t.Setenv("GOPHERMIND_SERVE_RATE", "2")
	srv, _ := testServer(t, []string{finalAnswerBody("hi")}, "t")

	var got429 bool
	for i := 0; i < 10; i++ {
		resp, err := http.DefaultClient.Do(authedReq(t, http.MethodGet, srv.URL+"/session", "t", ""))
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Error("expected at least one 429 after exceeding GOPHERMIND_SERVE_RATE=2 req/min")
	}
}
