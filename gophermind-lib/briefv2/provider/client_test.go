package provider_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/provider"
)

func TestNewHTTPClientIsHardened(t *testing.T) {
	c := provider.NewHTTPClient(90 * time.Second)
	if c.Timeout != 90*time.Second {
		t.Errorf("timeout = %v", c.Timeout)
	}
	if c.CheckRedirect == nil {
		t.Fatal("redirects are followed")
	}
	if err := c.CheckRedirect(&http.Request{}, nil); err == nil {
		t.Error("CheckRedirect allows the redirect")
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok || tr.Proxy != nil {
		t.Fatalf("the transport consults the proxy environment: %#v", c.Transport)
	}
	if provider.NewHTTPClient(0).Timeout != 0 {
		t.Error("zero timeout must stay zero (the caller's context bounds the call)")
	}
}

// A redirect, whatever its status, must never carry the prompt to another host.
func TestRedirectsNeverResendThePrompt(t *testing.T) {
	var hits atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer other.Close()
	for _, code := range []int{301, 302, 303, 307, 308} {
		first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, other.URL+"/v1/chat/completions", code)
		}))
		clients := map[string]*http.Client{
			"nil client":     nil,
			"hardened":       provider.NewHTTPClient(0),
			"plain client":   {},
			"server client":  first.Client(),
			"custom timeout": {Timeout: time.Minute},
		}
		for name, hc := range clients {
			p := provider.NewOpenAI(provider.Config{Name: "mini", BaseURL: first.URL + "/v1", HTTPClient: hc,
				Models: []provider.ModelInfo{{ID: "qwen", ContextTokens: 32768}}})
			if _, err := p.Complete(context.Background(), request()); err == nil {
				t.Errorf("%d %s: a redirect was treated as an answer", code, name)
			}
		}
		first.Close()
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("the redirect target received %d requests", n)
	}
}
