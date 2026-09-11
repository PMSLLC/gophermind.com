package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// scriptedLLM is a stand-in OpenAI-compatible endpoint for the approval
// round-trip test below. It never reaches a real LLM: /v1/models answers
// with an empty model list (which newLLMClient's liveness check treats as
// "nothing to validate against"), and /v1/chat/completions replays a fixed
// script of canned SSE bodies in order, one per call.
type scriptedLLM struct {
	mu    sync.Mutex
	turns [][]byte
	i     int
}

func (s *scriptedLLM) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		s.mu.Lock()
		body := s.turns[len(s.turns)-1]
		if s.i < len(s.turns) {
			body = s.turns[s.i]
			s.i++
		}
		s.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(body)
	}
}

// toolCallResp builds a streamed chat-completion body whose single delta
// carries one tool call, mirroring internal/agent's own test helper of the
// same shape (unexported there, so reproduced here).
func toolCallResp(id, name, args string) []byte {
	b, _ := json.Marshal(args)
	frame := `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"` + id +
		`","type":"function","function":{"name":"` + name + `","arguments":` + string(b) + `}}]}}]}`
	return []byte("data: " + frame + "\n\ndata: [DONE]\n\n")
}

// finalResp builds a streamed chat-completion body whose single delta
// carries final prose with finish_reason stop.
func finalResp(text string) []byte {
	b, _ := json.Marshal(text)
	frame := `{"choices":[{"delta":{"content":` + string(b) + `},"finish_reason":"stop"}]}`
	return []byte("data: " + frame + "\n\ndata: [DONE]\n\n")
}

// sseFrame is one parsed "event: ...\ndata: ...\n\n" block read from a live
// streaming HTTP response.
type sseFrame struct {
	event string
	data  string
}

// readSSE reads r line by line, decoding SSE frames and sending each to out
// as it completes (on the blank-line terminator), until r is exhausted, then
// closes out. It reads incrementally rather than buffering the whole body,
// so a caller can observe (or fail to observe) a frame while the server is
// still mid-response - the property this test depends on to prove blocking.
func readSSE(r *http.Response, out chan<- sseFrame) {
	defer close(out)
	defer r.Body.Close()
	scanner := bufio.NewScanner(r.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var event string
	var dataLines []string
	flush := func() {
		if event == "" && len(dataLines) == 0 {
			return
		}
		out <- sseFrame{event: event, data: strings.Join(dataLines, "\n")}
		event = ""
		dataLines = nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
		}
	}
	flush()
}

// waitReady polls GET /backend-status until the embedded server's LLM
// backend is installed, so the stream request below does not race
// resolveLLMBackend's background goroutine.
func waitReady(t *testing.T, srv *embeddedServer) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		req, _ := http.NewRequest(http.MethodGet, srv.BaseURL+"/backend-status", nil)
		req.Header.Set("Authorization", "Bearer "+srv.Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET /backend-status: %v", err)
		}
		var snap struct {
			Ready bool   `json:"ready"`
			Error string `json:"error"`
		}
		decErr := json.NewDecoder(resp.Body).Decode(&snap)
		resp.Body.Close()
		if decErr != nil {
			t.Fatalf("decode /backend-status: %v", decErr)
		}
		if snap.Ready {
			return
		}
		if snap.Error != "" {
			t.Fatalf("backend-status reported an error: %s", snap.Error)
		}
		if time.Now().After(deadline) {
			t.Fatal("backend never became ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestSessionApprovalGateBlocksAndResolves is the end-to-end proof for the
// desktop approvals feature: a session turn that calls a gated tool
// (run_shell) must stop on the server and wait for a human decision posted
// to POST /session/{id}/approve, not auto-approve. It runs the exact
// production wiring (startEmbeddedServer -> newServeDeps -> serve.NewMux),
// with the LLM stubbed by scriptedLLM so the test never touches a real
// model endpoint. Both outcomes are covered: approve lets run_shell
// actually execute, deny stops it before it runs.
func TestSessionApprovalGateBlocksAndResolves(t *testing.T) {
	for _, tc := range []struct {
		name    string
		approve bool
	}{
		{"approve", true},
		{"deny", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := &scriptedLLM{turns: [][]byte{
				toolCallResp("call_1", "run_shell", `{"command":"echo hello-approval-test"}`),
				finalResp("turn complete"),
			}}
			llmSrv := httptest.NewServer(script.handler())
			defer llmSrv.Close()

			t.Setenv("GOPHERMIND_BASE_URL", llmSrv.URL)
			t.Setenv("GOPHERMIND_MODEL", "test-model")
			t.Setenv("GOPHERMIND_ROOT", t.TempDir())
			t.Setenv("GOPHERMIND_APPROVAL", "ask")

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			srv, err := startEmbeddedServer(ctx)
			if err != nil {
				t.Fatalf("startEmbeddedServer: %v", err)
			}
			defer func() {
				if err := srv.Shutdown(); err != nil {
					t.Errorf("Shutdown: %v", err)
				}
			}()

			waitReady(t, srv)

			// Create a session.
			createReq, _ := http.NewRequest(http.MethodPost, srv.BaseURL+"/session", nil)
			createReq.Header.Set("Authorization", "Bearer "+srv.Token)
			createResp, err := http.DefaultClient.Do(createReq)
			if err != nil {
				t.Fatalf("POST /session: %v", err)
			}
			var created struct {
				ID string `json:"id"`
			}
			decErr := json.NewDecoder(createResp.Body).Decode(&created)
			createResp.Body.Close()
			if decErr != nil {
				t.Fatalf("decode /session response: %v", decErr)
			}
			if created.ID == "" {
				t.Fatal("POST /session returned an empty id")
			}

			// Start the turn. The gated run_shell call inside it must block
			// on the server until this test resolves it below.
			streamReq, _ := http.NewRequest(http.MethodPost,
				srv.BaseURL+"/session/"+created.ID+"/stream", strings.NewReader("run the echo command"))
			streamReq.Header.Set("Authorization", "Bearer "+srv.Token)
			streamResp, err := http.DefaultClient.Do(streamReq)
			if err != nil {
				t.Fatalf("POST /session/{id}/stream: %v", err)
			}
			if streamResp.StatusCode != http.StatusOK {
				streamResp.Body.Close()
				t.Fatalf("POST /session/{id}/stream: want 200, got %d", streamResp.StatusCode)
			}

			frames := make(chan sseFrame, 32)
			go readSSE(streamResp, frames)

			// Read frames until approval-needed, capturing the approval id.
			var approvalID string
			for approvalID == "" {
				select {
				case f, ok := <-frames:
					if !ok {
						t.Fatal("stream ended before an approval-needed frame arrived")
					}
					if f.event != "approval-needed" {
						continue
					}
					var payload struct {
						ApprovalID string `json:"approval_id"`
						Tool       string `json:"tool"`
						Args       string `json:"args"`
					}
					if err := json.Unmarshal([]byte(f.data), &payload); err != nil {
						t.Fatalf("approval-needed frame not JSON: %v (data=%s)", err, f.data)
					}
					if payload.Tool != "run_shell" {
						t.Fatalf("approval-needed tool = %q, want run_shell", payload.Tool)
					}
					if payload.ApprovalID == "" {
						t.Fatal("approval-needed frame carried an empty approval_id")
					}
					approvalID = payload.ApprovalID
				case <-time.After(15 * time.Second):
					t.Fatal("timed out waiting for an approval-needed frame")
				}
			}

			// Prove the call is actually blocked, not just eventually
			// consistent: with no decision posted yet, no further frame
			// should show up within a short window.
			select {
			case f, ok := <-frames:
				if ok {
					t.Fatalf("received a frame (%s) before the approval was resolved; the gate did not block", f.event)
				}
			case <-time.After(300 * time.Millisecond):
				// Expected: still blocked.
			}

			// Resolve the approval.
			decisionBody, _ := json.Marshal(map[string]any{
				"approval_id": approvalID,
				"approved":    tc.approve,
			})
			approveReq, _ := http.NewRequest(http.MethodPost,
				fmt.Sprintf("%s/session/%s/approve", srv.BaseURL, created.ID), strings.NewReader(string(decisionBody)))
			approveReq.Header.Set("Authorization", "Bearer "+srv.Token)
			approveReq.Header.Set("Content-Type", "application/json")
			approveResp, err := http.DefaultClient.Do(approveReq)
			if err != nil {
				t.Fatalf("POST /session/{id}/approve: %v", err)
			}
			approveResp.Body.Close()
			if approveResp.StatusCode != http.StatusOK {
				t.Fatalf("POST /session/{id}/approve: want 200, got %d", approveResp.StatusCode)
			}

			// Drain the rest of the stream and check what actually happened.
			var sawToolResult bool
			var toolResultText string
			var sawDone bool
		drain:
			for {
				select {
				case f, ok := <-frames:
					if !ok {
						break drain
					}
					switch f.event {
					case "tool_result":
						var payload struct {
							Name string `json:"name"`
							Text string `json:"text"`
						}
						if err := json.Unmarshal([]byte(f.data), &payload); err != nil {
							t.Fatalf("tool_result frame not JSON: %v (data=%s)", err, f.data)
						}
						sawToolResult = true
						toolResultText = payload.Text
					case "done":
						sawDone = true
					}
				case <-time.After(15 * time.Second):
					t.Fatal("timed out waiting for the turn to finish after resolving the approval")
				}
			}

			if !sawDone {
				t.Fatal("stream never reached done after the approval was resolved")
			}

			if tc.approve {
				if !sawToolResult {
					t.Fatal("approve: expected a tool_result frame (run_shell should have executed), got none")
				}
				if !strings.Contains(toolResultText, "hello-approval-test") {
					t.Fatalf("approve: tool_result text = %q, want it to contain the echoed output", toolResultText)
				}
			} else {
				if sawToolResult {
					t.Fatalf("deny: expected run_shell never to execute (no tool_result frame), got text %q", toolResultText)
				}
			}
		})
	}
}

// TestOneShotRoutesRefuseGatedTools is the proof that /run and /run/stream
// cannot be used to walk around the approvals screen. Both routes are mounted
// on the same mux, behind the same bearer token, as the session routes, and
// neither carries a session id or an SSE channel a pending approval could be
// raised on and resolved against. So a gated tool call made through them must
// be refused outright, and the refusal must reach the caller rather than
// disappearing into the model's own transcript.
func TestOneShotRoutesRefuseGatedTools(t *testing.T) {
	for _, tc := range []struct {
		name  string
		route string
	}{
		{"run", "/run"},
		{"run stream", "/run/stream"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := &scriptedLLM{turns: [][]byte{
				toolCallResp("call_1", "run_shell", `{"command":"touch pwned-one-shot"}`),
				finalResp("turn complete"),
			}}
			llmSrv := httptest.NewServer(script.handler())
			defer llmSrv.Close()

			root := t.TempDir()
			t.Setenv("GOPHERMIND_BASE_URL", llmSrv.URL)
			t.Setenv("GOPHERMIND_MODEL", "test-model")
			t.Setenv("GOPHERMIND_ROOT", root)
			t.Setenv("GOPHERMIND_APPROVAL", "ask")

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			srv, err := startEmbeddedServer(ctx)
			if err != nil {
				t.Fatalf("startEmbeddedServer: %v", err)
			}
			defer func() {
				if err := srv.Shutdown(); err != nil {
					t.Errorf("Shutdown: %v", err)
				}
			}()

			waitReady(t, srv)

			req, _ := http.NewRequest(http.MethodPost, srv.BaseURL+tc.route, strings.NewReader("run the command"))
			req.Header.Set("Authorization", "Bearer "+srv.Token)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("POST %s: %v", tc.route, err)
			}
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				t.Fatalf("read %s response: %v", tc.route, readErr)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("POST %s: want 200, got %d (%s)", tc.route, resp.StatusCode, body)
			}

			if _, err := os.Stat(filepath.Join(root, "pwned-one-shot")); err == nil {
				t.Fatalf("POST %s executed the gated run_shell call: the marker file exists", tc.route)
			} else if !os.IsNotExist(err) {
				t.Fatalf("stat marker file: %v", err)
			}

			text := string(body)
			if tc.route == "/run" {
				var decoded struct {
					Result string `json:"result"`
				}
				if err := json.Unmarshal(body, &decoded); err != nil {
					t.Fatalf("decode /run response: %v (body=%s)", err, body)
				}
				text = decoded.Result
			}
			if !strings.Contains(text, "run_shell") || !strings.Contains(text, "refused") {
				t.Fatalf("POST %s did not report the refusal to the caller: %q", tc.route, text)
			}
		})
	}
}
