package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// captureServer returns an httptest server that records the raw request body of
// every chat-completions call, plus a function to read the most recent one.
func captureServer(t *testing.T) (*httptest.Server, func() string) {
	t.Helper()
	var mu sync.Mutex
	var last string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		last = string(body)
		mu.Unlock()
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() string {
		mu.Lock()
		defer mu.Unlock()
		return last
	}
}

func TestRequestSendsConfiguredTemperature(t *testing.T) {
	srv, lastBody := captureServer(t)
	c := New(srv.URL, "", "m", 5*time.Second, false)
	c.SetTemperature(0.7)

	if _, _, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	var got ChatRequest
	if err := json.Unmarshal([]byte(lastBody()), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Temperature != 0.7 {
		t.Errorf("temperature = %v, want 0.7", got.Temperature)
	}
}

func TestRequestAlwaysSendsTemperatureZero(t *testing.T) {
	srv, lastBody := captureServer(t)
	c := New(srv.URL, "", "m", 5*time.Second, false)
	// Default temperature 0 must still appear on the wire (key present), so the
	// server gets explicit determinism rather than its own default.
	if _, _, err := c.Complete(context.Background(), nil, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !strings.Contains(lastBody(), `"temperature":0`) {
		t.Errorf("body missing explicit temperature:0: %s", lastBody())
	}
}

func TestRequestOmitsTopPWhenUnset(t *testing.T) {
	srv, lastBody := captureServer(t)
	c := New(srv.URL, "", "m", 5*time.Second, false)
	if _, _, err := c.Complete(context.Background(), nil, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if strings.Contains(lastBody(), "top_p") {
		t.Errorf("top_p should be omitted when unset, got: %s", lastBody())
	}
}

func TestRequestSendsTopPWhenSet(t *testing.T) {
	srv, lastBody := captureServer(t)
	c := New(srv.URL, "", "m", 5*time.Second, false)
	p := 0.9
	c.SetTopP(&p)
	if _, _, err := c.Complete(context.Background(), nil, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	var got ChatRequest
	if err := json.Unmarshal([]byte(lastBody()), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.TopP == nil || *got.TopP != 0.9 {
		t.Errorf("top_p = %v, want 0.9", got.TopP)
	}
}

func TestSetTopPNilUnsets(t *testing.T) {
	srv, lastBody := captureServer(t)
	c := New(srv.URL, "", "m", 5*time.Second, false)
	p := 0.5
	c.SetTopP(&p)
	c.SetTopP(nil)
	if _, _, err := c.Complete(context.Background(), nil, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if strings.Contains(lastBody(), "top_p") {
		t.Errorf("top_p should be omitted after unset, got: %s", lastBody())
	}
}

func TestChangingTemperatureAffectsNextRequest(t *testing.T) {
	srv, lastBody := captureServer(t)
	c := New(srv.URL, "", "m", 5*time.Second, false)

	c.SetTemperature(0.2)
	if _, _, err := c.Complete(context.Background(), nil, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	var first ChatRequest
	json.Unmarshal([]byte(lastBody()), &first)
	if first.Temperature != 0.2 {
		t.Fatalf("first temperature = %v, want 0.2", first.Temperature)
	}

	c.SetTemperature(1.1)
	if _, _, err := c.Complete(context.Background(), nil, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	var second ChatRequest
	json.Unmarshal([]byte(lastBody()), &second)
	if second.Temperature != 1.1 {
		t.Errorf("second temperature = %v, want 1.1 (change should take effect)", second.Temperature)
	}
}

func TestSetTopPCopiesValue(t *testing.T) {
	c := New("http://x", "", "m", time.Second, false)
	p := 0.4
	c.SetTopP(&p)
	p = 0.99 // mutating the caller's variable must not change the stored value
	if got := c.TopP(); got == nil || *got != 0.4 {
		t.Errorf("TopP = %v, want 0.4 (stored value must be a copy)", got)
	}
}
