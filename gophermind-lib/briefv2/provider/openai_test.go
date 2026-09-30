package provider_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/provider"
)

const (
	secretKey = "sk-SECRET-key-91b7"
	canary    = "CANARY-prompt-text-5e21"
)

func newProvider(t *testing.T, h http.Handler, key string) provider.Provider {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return provider.NewOpenAI(provider.Config{
		Name: "mini", BaseURL: srv.URL + "/v1", APIKey: key, MaxConcurrent: 1, HTTPClient: srv.Client(),
		Models: []provider.ModelInfo{{ID: "qwen", ContextTokens: 32768}},
	})
}

func request() provider.Request {
	return provider.Request{
		Model:       "qwen",
		Messages:    []provider.Message{{Role: provider.RoleSystem, Content: "be brief"}, {Role: provider.RoleUser, Content: canary}},
		MaxTokens:   256,
		Temperature: 0.2,
	}
}

func TestCompleteSuccessSendsAnOpenAIRequestAndReadsTheReply(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	p := newProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotBody)
		io.WriteString(w, `{"model":"stealth/served","choices":[{"message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":11,"completion_tokens":3}}`)
	}), secretKey)

	got, err := p.Complete(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/chat/completions" || gotAuth != "Bearer "+secretKey {
		t.Errorf("path %q auth %q", gotPath, gotAuth)
	}
	if gotBody["model"] != "qwen" || gotBody["max_tokens"] != float64(256) || gotBody["temperature"] != 0.2 {
		t.Errorf("body = %v", gotBody)
	}
	if msgs, _ := gotBody["messages"].([]any); len(msgs) != 2 {
		t.Errorf("messages = %v", gotBody["messages"])
	}
	if got.Text != "hello" || got.Model != "stealth/served" || got.Usage.PromptTokens != 11 || got.Usage.CompletionTokens != 3 || got.Duration <= 0 {
		t.Errorf("response = %+v", got)
	}
}

func TestNoAuthorizationHeaderWithoutAKey(t *testing.T) {
	var sawAuth bool
	p := newProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sawAuth = r.Header["Authorization"]
		io.WriteString(w, `{"choices":[{"message":{"content":"x"}}]}`)
	}), "")
	got, err := p.Complete(context.Background(), request())
	if err != nil || sawAuth {
		t.Fatalf("err=%v sawAuth=%v", err, sawAuth)
	}
	if got.Model != "qwen" {
		t.Errorf("a reply with no model field should report the requested model, got %q", got.Model)
	}
}

func TestStatusCodesMapToTypedErrors(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		header  map[string]string
		body    string
		check   func(error) bool
		message string
	}{
		{"429 with Retry-After", 429, map[string]string{"Retry-After": "7"}, `{"error":"slow down"}`,
			func(e error) bool {
				var x provider.ErrRateLimited
				return errors.As(e, &x) && x.RetryAfter == 7*time.Second
			}, "rate limited with 7s"},
		{"429 without Retry-After", 429, nil, ``,
			func(e error) bool { var x provider.ErrRateLimited; return errors.As(e, &x) && x.RetryAfter == 0 }, "rate limited with 0"},
		{"401", 401, nil, `{"error":"bad key"}`,
			func(e error) bool { var x provider.ErrAuth; return errors.As(e, &x) && x.Provider == "mini" }, "auth"},
		{"403", 403, nil, `{"error":"forbidden"}`,
			func(e error) bool { var x provider.ErrAuth; return errors.As(e, &x) }, "auth"},
		{"400 context length", 400, nil, `{"error":{"message":"This model's maximum context length is 32768 tokens"}}`,
			func(e error) bool { var x provider.ErrContextTooLong; return errors.As(e, &x) && x.Limit == 32768 }, "context too long with the model's limit"},
		{"404 model not found", 404, nil, `{"error":{"message":"model \"qwen\" not found, try pulling it first"}}`,
			func(e error) bool { var x provider.ErrModelNotFound; return errors.As(e, &x) && x.Model == "qwen" }, "model not found"},
		{"500", 500, nil, `boom`,
			func(e error) bool { var x provider.ErrTransient; return errors.As(e, &x) }, "transient"},
		{"503", 503, nil, ``,
			func(e error) bool { var x provider.ErrTransient; return errors.As(e, &x) }, "transient"},
		{"400 other", 400, nil, `{"error":"unknown field"}`,
			func(e error) bool {
				var a provider.ErrContextTooLong
				var b provider.ErrTransient
				return e != nil && !errors.As(e, &a) && !errors.As(e, &b)
			}, "a plain error, neither context nor transient"},
		{"404 not a model problem", 404, nil, `page missing`,
			func(e error) bool { var x provider.ErrModelNotFound; return e != nil && !errors.As(e, &x) }, "a plain error, not model_not_found"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := newProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for k, v := range c.header {
					w.Header().Set(k, v)
				}
				w.WriteHeader(c.status)
				io.WriteString(w, c.body)
			}), secretKey)
			_, err := p.Complete(context.Background(), request())
			if !c.check(err) {
				t.Errorf("err = %#v, want %s", err, c.message)
			}
		})
	}
}

func TestErrorsNeverCarryTheKeyOrThePrompt(t *testing.T) {
	// The server echoes the whole request back in its error body, the way some gateways do.
	for _, status := range []int{400, 401, 404, 429, 500} {
		p := newProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			w.WriteHeader(status)
			io.WriteString(w, "maximum context length model not found "+string(b)+r.Header.Get("Authorization"))
		}), secretKey)
		_, err := p.Complete(context.Background(), request())
		if err == nil {
			t.Fatalf("status %d: no error", status)
		}
		if s := err.Error(); strings.Contains(s, secretKey) || strings.Contains(s, canary) {
			t.Errorf("status %d: error text leaks the key or the prompt: %q", status, s)
		}
	}
}

func TestTransportFailureIsTransientAndDoesNotNameTheURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening any more
	p := provider.NewOpenAI(provider.Config{Name: "mini", BaseURL: url + "/v1", APIKey: secretKey, HTTPClient: http.DefaultClient})
	_, err := p.Complete(context.Background(), request())
	var tr provider.ErrTransient
	if !errors.As(err, &tr) {
		t.Fatalf("err = %v, want ErrTransient", err)
	}
	if strings.Contains(err.Error(), secretKey) || strings.Contains(err.Error(), canary) {
		t.Errorf("error leaks: %v", err)
	}
}

func TestCancellationReturnsTheContextError(t *testing.T) {
	release := make(chan struct{})
	p := newProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}), "")
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := p.Complete(ctx, request())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestReplyProblemsAreTransient(t *testing.T) {
	for name, body := range map[string]string{
		"not json":     `<html>`,
		"no choices":   `{"choices":[]}`,
		"error object": `{"error":{"message":"overloaded"},"choices":[{"message":{"content":"x"}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			p := newProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }), "")
			_, err := p.Complete(context.Background(), request())
			var tr provider.ErrTransient
			if !errors.As(err, &tr) {
				t.Errorf("err = %v, want ErrTransient", err)
			}
		})
	}
}

func TestFakeCountsCallsAndRecordsRequests(t *testing.T) {
	f := provider.NewFake("fake", []provider.ModelInfo{{ID: "m", ContextTokens: 100}}, func(call int, req provider.Request) (provider.Response, error) {
		if call == 1 {
			return provider.Response{}, provider.ErrRateLimited{RetryAfter: time.Second}
		}
		return provider.Response{Text: "ok", Model: req.Model}, nil
	})
	if f.Name() != "fake" || len(f.Models()) != 1 {
		t.Fatalf("name/models: %q %v", f.Name(), f.Models())
	}
	if _, err := f.Complete(context.Background(), provider.Request{Model: "m"}); err == nil {
		t.Fatal("first call should be rate limited")
	}
	if r, err := f.Complete(context.Background(), provider.Request{Model: "m"}); err != nil || r.Text != "ok" {
		t.Fatalf("second call = %+v, %v", r, err)
	}
	if f.Calls() != 2 || len(f.Requests()) != 2 {
		t.Errorf("calls=%d requests=%d", f.Calls(), len(f.Requests()))
	}
}

func TestFakeIsSafeForConcurrentUseAndHonorsCancellation(t *testing.T) {
	f := provider.NewFake("fake", nil, nil)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); f.Complete(context.Background(), provider.Request{Model: "m"}) }()
	}
	wg.Wait()
	if f.Calls() != 40 {
		t.Errorf("calls = %d, want 40", f.Calls())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Complete(ctx, provider.Request{}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled context: err = %v", err)
	}
	if f.Calls() != 40 {
		t.Error("a cancelled call must not reach the script")
	}
}

// thinking imitates qwen3.6: unless the request says reasoning_effort none the
// answer goes to a non-standard reasoning field, content is empty and the
// budget runs out.
func thinking(seen *[]map[string]any) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		json.Unmarshal(b, &body)
		*seen = append(*seen, body)
		if body["reasoning_effort"] == "none" {
			io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"[1]"}}]}`)
			return
		}
		io.WriteString(w, `{"choices":[{"finish_reason":"length","message":{"content":"","reasoning":"hmm `+canary+`"}}]}`)
	})
}

func newThinking(t *testing.T, effort string, seen *[]map[string]any) provider.Provider {
	t.Helper()
	srv := httptest.NewServer(thinking(seen))
	t.Cleanup(srv.Close)
	return provider.NewOpenAI(provider.Config{
		Name: "mini", BaseURL: srv.URL + "/v1", MaxConcurrent: 1, HTTPClient: srv.Client(), ReasoningEffort: effort,
		Models: []provider.ModelInfo{{ID: "qwen", ContextTokens: 32768}},
	})
}

func TestReasoningEffortIsSentOnlyWhenConfigured(t *testing.T) {
	var seen []map[string]any
	p := newThinking(t, "none", &seen)
	got, err := p.Complete(context.Background(), request())
	if err != nil || got.Text != "[1]" {
		t.Fatalf("got %+v err %v", got, err)
	}
	if seen[0]["reasoning_effort"] != "none" {
		t.Errorf("body = %v", seen[0])
	}

	seen = nil
	p = newThinking(t, "", &seen)
	p.Complete(context.Background(), request())
	if _, has := seen[0]["reasoning_effort"]; has {
		t.Errorf("reasoning_effort sent when not configured: %v", seen[0])
	}
}

func TestEmptyContentWithLengthIsATruncationError(t *testing.T) {
	var seen []map[string]any
	p := newThinking(t, "", &seen)
	_, err := p.Complete(context.Background(), request())
	var tr provider.ErrTruncated
	if !errors.As(err, &tr) {
		t.Fatalf("err = %v, want ErrTruncated", err)
	}
	if strings.Contains(err.Error(), canary) || !strings.Contains(err.Error(), "mini") {
		t.Errorf("error text %q", err)
	}
}

func TestEmptyContentForAnotherReasonIsAnEmptyReplyError(t *testing.T) {
	p := newProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"  \n","reasoning":"`+canary+`"}}]}`)
	}), "")
	_, err := p.Complete(context.Background(), request())
	var er provider.ErrEmptyReply
	if !errors.As(err, &er) {
		t.Fatalf("err = %v, want ErrEmptyReply", err)
	}
	if strings.Contains(err.Error(), canary) {
		t.Errorf("error text %q leaks reply text", err)
	}
}
