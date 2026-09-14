package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/wireguard"
)

// TestRunEndpoint covers POST /run: the CLI-webhook-parity route buildDeps'
// run closure backs, previously untested (unlike SessionTurn, which every
// other integration test exercises).
func TestRunEndpoint(t *testing.T) {
	srv, _ := testServer(t, []string{finalAnswerBody("42")}, "t")
	resp, err := http.DefaultClient.Do(authedReq(t, http.MethodPost, srv.URL+"/run", "t", "what is the answer"))
	if err != nil {
		t.Fatalf("POST /run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "42") {
		t.Errorf("response missing the model's answer: %s", body)
	}
}

// TestRunStreamEndpoint covers POST /run/stream: buildDeps' stream closure,
// the non-session streaming sibling of SessionTurn.
func TestRunStreamEndpoint(t *testing.T) {
	srv, _ := testServer(t, []string{finalAnswerBody("streamed answer")}, "t")
	resp, err := http.DefaultClient.Do(authedReq(t, http.MethodPost, srv.URL+"/run/stream", "t", "go"))
	if err != nil {
		t.Fatalf("POST /run/stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "event: done") {
		t.Errorf("stream did not complete: %s", body)
	}
}

// TestSessionMessages_ReplaysHistory covers GET /session/{id}/messages:
// buildDeps' loadMessages closure, previously untested.
func TestSessionMessages_ReplaysHistory(t *testing.T) {
	srv, _ := testServer(t, []string{finalAnswerBody("remembered")}, "t")

	create, _ := http.DefaultClient.Do(authedReq(t, http.MethodPost, srv.URL+"/session", "t", `{"id":"sess-msgs"}`))
	create.Body.Close()

	streamResp, err := http.DefaultClient.Do(authedReq(t, http.MethodPost, srv.URL+"/session/sess-msgs/stream", "t", "remember this"))
	if err != nil {
		t.Fatalf("POST stream: %v", err)
	}
	io.Copy(io.Discard, streamResp.Body)
	streamResp.Body.Close()

	// A session that never existed returns 404, not an empty 200 -- worth
	// covering the negative path explicitly since loadMessages branches on
	// session.Exists.
	missing, err := http.DefaultClient.Do(authedReq(t, http.MethodGet, srv.URL+"/session/nonexistent/messages", "t", ""))
	if err != nil {
		t.Fatalf("GET messages (missing): %v", err)
	}
	missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Errorf("missing session status = %d, want 404", missing.StatusCode)
	}

	resp, err := http.DefaultClient.Do(authedReq(t, http.MethodGet, srv.URL+"/session/sess-msgs/messages", "t", ""))
	if err != nil {
		t.Fatalf("GET messages: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	var msgs []json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&msgs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(msgs) == 0 {
		t.Error("expected the replayed session to have messages")
	}
}

// TestSessionCRUD covers PATCH /session/{id} (rename) and DELETE
// /session/{id}, the two session-lifecycle routes no other test exercises.
// Both return 204 No Content on success, per sessionRenameHandler/
// sessionDeleteHandler. Rename only touches a small name sidecar and
// succeeds even for a session with no turns yet; delete removes the actual
// session file, which session.Save only creates after a real turn -- so
// this runs one turn first, or DELETE correctly 404s on a file that was
// never written (confirmed by hand before fixing this test: POST /session
// alone does not create that file).
func TestSessionCRUD(t *testing.T) {
	srv, _ := testServer(t, []string{finalAnswerBody("hi")}, "t")

	create, err := http.DefaultClient.Do(authedReq(t, http.MethodPost, srv.URL+"/session", "t", `{"id":"sess-crud"}`))
	if err != nil {
		t.Fatalf("POST /session: %v", err)
	}
	create.Body.Close()

	rename, err := http.DefaultClient.Do(authedReq(t, http.MethodPatch, srv.URL+"/session/sess-crud", "t", `{"name":"renamed"}`))
	if err != nil {
		t.Fatalf("PATCH /session/sess-crud: %v", err)
	}
	rename.Body.Close()
	if rename.StatusCode != http.StatusNoContent {
		t.Errorf("rename status = %d, want 204", rename.StatusCode)
	}

	turn, err := http.DefaultClient.Do(authedReq(t, http.MethodPost, srv.URL+"/session/sess-crud/stream", "t", "hi"))
	if err != nil {
		t.Fatalf("POST stream: %v", err)
	}
	io.Copy(io.Discard, turn.Body)
	turn.Body.Close()

	del, err := http.DefaultClient.Do(authedReq(t, http.MethodDelete, srv.URL+"/session/sess-crud", "t", ""))
	if err != nil {
		t.Fatalf("DELETE /session/sess-crud: %v", err)
	}
	del.Body.Close()
	if del.StatusCode != http.StatusNoContent {
		t.Errorf("delete status = %d, want 204", del.StatusCode)
	}
}

// TestModelsAndSkillsRoutes covers GET /models and GET /skills, the two
// remaining always-registered routes (given SessionTurn is set) no other
// test hits directly (TestModelsCatalogue covers /models/catalogue, a
// different route backed by EndpointModels rather than ListModels).
func TestModelsAndSkillsRoutes(t *testing.T) {
	srv, _ := testServer(t, []string{finalAnswerBody("hi")}, "t")

	resp, err := http.DefaultClient.Do(authedReq(t, http.MethodGet, srv.URL+"/models", "t", ""))
	if err != nil {
		t.Fatalf("GET /models: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}

	skillsResp, err := http.DefaultClient.Do(authedReq(t, http.MethodGet, srv.URL+"/skills", "t", ""))
	if err != nil {
		t.Fatalf("GET /skills: %v", err)
	}
	defer skillsResp.Body.Close()
	if skillsResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(skillsResp.Body)
		t.Fatalf("status = %d, want 200: %s", skillsResp.StatusCode, body)
	}
}

// TestHMACVerification covers the acceptance criterion explicitly: when
// GOPHERMIND_SERVE_HMAC_SECRET is configured, a request also needs a valid
// X-Hub-Signature-256 over its body, on top of the bearer token.
func TestHMACVerification(t *testing.T) {
	secret := "shh"
	t.Setenv("GOPHERMIND_SERVE_HMAC_SECRET", secret)
	srv, _ := testServer(t, []string{finalAnswerBody("hi")}, "t")

	body := `{"id":"sess-hmac"}`
	sign := func(b string) string {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(b))
		return "sha256=" + hex.EncodeToString(mac.Sum(nil))
	}

	// Correct signature: accepted.
	req := authedReq(t, http.MethodPost, srv.URL+"/session", "t", body)
	req.Header.Set("X-Hub-Signature-256", sign(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("valid signature: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("valid signature status = %d, want 200", resp.StatusCode)
	}

	// Wrong signature: rejected even with a valid bearer token.
	req = authedReq(t, http.MethodPost, srv.URL+"/session", "t", body)
	req.Header.Set("X-Hub-Signature-256", sign("tampered"))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("invalid signature: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("wrong HMAC signature was accepted")
	}

	// Missing signature entirely: also rejected.
	req = authedReq(t, http.MethodPost, srv.URL+"/session", "t", body)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("missing signature: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("missing HMAC signature was accepted")
	}
}

// TestWgRegisterHandler_BadRequests covers wgRegisterHandler's remaining
// error branches: malformed JSON and a missing public_key.
func TestWgRegisterHandler_BadRequests(t *testing.T) {
	wg, tracker, err := startWireGuard(serverConfig{WGInterface: "wg0", WGListenPort: freeUDPPort(t)}, discardLogger())
	if err != nil {
		t.Fatalf("startWireGuard: %v", err)
	}
	defer wg.Close()
	h := wgRegisterHandler(tracker, fakeValidator("t", "alice"), discardLogger())

	cases := []struct {
		name string
		body string
	}{
		{"malformed JSON", `{not json`},
		{"missing public_key", `{"token":"t"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/wg/register", strings.NewReader(c.body))
			rr := httptest.NewRecorder()
			h(rr, req)
			if rr.Code == http.StatusOK {
				t.Errorf("%s: status = 200, want an error", c.name)
			}
		})
	}
}

// TestRunWithConfig_EndToEnd exercises run's actual orchestration (the one
// piece no other test does: buildDeps + serve.NewMux + startWireGuard +
// the pipeline watcher + runServer, wired together exactly as run() wires
// them) against a real listener, a fake LLM backend, and a cancellable
// context standing in for a delivered SIGINT/SIGTERM.
func TestRunWithConfig_EndToEnd(t *testing.T) {
	llm := fakeLLM(t, []string{finalAnswerBody("hi")})
	defer llm.Close()
	root := t.TempDir()
	logger := discardLogger()

	ln := mustListen(t)
	addr := ln.Addr().String()

	cfg := serverConfig{Token: "t", LLMEndpoint: llm.URL, Root: root, WGInterface: "wg0", WGListenPort: freeUDPPort(t)}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- runWithConfig(cfg, ln, logger, ctx) }()

	// Poll /healthz rather than a fixed sleep: runWithConfig does real work
	// (buildDeps, NewMux, WireGuard init) before runServer starts Serve.
	deadline := time.Now().Add(5 * time.Second)
	var up bool
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/healthz")
		if err == nil {
			resp.Body.Close()
			up = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !up {
		t.Fatal("server never became reachable")
	}

	// A registered route (proves the full mux, not just healthz, is live)
	// and the WireGuard route (proves startWireGuard actually ran).
	resp, err := http.DefaultClient.Do(authedReq(t, http.MethodGet, "http://"+addr+"/session", "t", ""))
	if err != nil {
		t.Fatalf("GET /session: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/session status = %d, want 200", resp.StatusCode)
	}
	wgResp, err := http.DefaultClient.Do(authedReq(t, http.MethodPost, "http://"+addr+"/wg/register", "t", `{"public_key":"x"}`))
	if err != nil {
		t.Fatalf("POST /wg/register: %v", err)
	}
	wgResp.Body.Close()
	if wgResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("/wg/register status = %d, want 401 (stub validator, no token configured)", wgResp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("runWithConfig: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runWithConfig did not shut down after ctx was cancelled")
	}
}

// TestRunSweeper_RemovesTimedOutPeersOnATicker covers runSweeper itself
// (Sweep is already covered directly by TestPeerTracker_Timeout): the
// background ticker loop actually calls Sweep periodically and stops when
// its context is cancelled.
func TestRunSweeper_RemovesTimedOutPeersOnATicker(t *testing.T) {
	wg, err := wireguard.NewServer(context.Background(), wireguard.ServerConfig{ListenPort: freeUDPPort(t)})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer wg.Close()

	_, pub := genKeypair(t)
	tracker := newPeerTracker(wg, 10*time.Millisecond, discardLogger())
	if _, err := tracker.Register(pub); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		tracker.runSweeper(ctx, 20*time.Millisecond)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runSweeper did not stop when its context was cancelled")
	}
	if wg.PeerCount() != 0 {
		t.Errorf("peer count = %d, want 0 (runSweeper should have timed the peer out)", wg.PeerCount())
	}
}
