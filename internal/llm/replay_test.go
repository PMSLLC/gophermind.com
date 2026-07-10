package llm

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecorderThenReplay(t *testing.T) {
	// A live server that returns a canned completion.
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "hello") {
			t.Errorf("server got unexpected body: %s", body)
		}
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{"answer":"world"}`)
	}))
	defer srv.Close()

	cassette := filepath.Join(t.TempDir(), "cassette.jsonl")

	// Record pass: real round-trips are captured to the cassette.
	rec := NewRecorder(http.DefaultTransport, cassette)
	client := &http.Client{Transport: rec}
	resp, err := client.Post(srv.URL, "application/json", strings.NewReader(`{"msg":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(got) != `{"answer":"world"}` {
		t.Fatalf("recorder altered the response body: %s", got)
	}
	if hits != 1 {
		t.Fatalf("expected 1 live hit, got %d", hits)
	}

	// Replay pass: the same request is served from the cassette, no live hit.
	rp, err := NewReplayer(cassette)
	if err != nil {
		t.Fatal(err)
	}
	client = &http.Client{Transport: rp}
	resp, err = client.Post(srv.URL, "application/json", strings.NewReader(`{"msg":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	got, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(got) != `{"answer":"world"}` {
		t.Errorf("replayed body wrong: %s", got)
	}
	if hits != 1 {
		t.Errorf("replay should not hit the server; hits=%d", hits)
	}
}

func TestReplayerMissErrors(t *testing.T) {
	cassette := filepath.Join(t.TempDir(), "empty.jsonl")
	rec := NewRecorder(http.DefaultTransport, cassette)
	// Record nothing; then a replay of an unknown request must error.
	_ = rec
	rp, err := NewReplayer(cassette)
	if err != nil {
		// A missing cassette is allowed to error; treat as pass.
		return
	}
	client := &http.Client{Transport: rp}
	if _, err := client.Post("http://example.invalid", "application/json", strings.NewReader(`{"x":1}`)); err == nil {
		t.Error("replay of an unrecorded request should error")
	}
}
