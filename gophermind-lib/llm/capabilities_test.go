package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// newModelsServer returns an httptest server that serves body at /v1/models and
// counts how many times that path is hit (to assert caching skips re-probes).
func newModelsServer(t *testing.T, body string) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			atomic.AddInt32(&hits, 1)
			w.Write([]byte(body))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestProbeParsesEndpointContextLength(t *testing.T) {
	// vLLM-style: reports context_length and max_tokens, omits a tools flag.
	srv, _ := newModelsServer(t, `{"data":[{"id":"my-model","context_length":40960,"max_tokens":6000}]}`)
	c := New(srv.URL, "", "my-model", 5*time.Second, false)

	caps := c.ProbeCapabilities(context.Background())
	if caps.ContextWindow != 40960 {
		t.Errorf("ContextWindow = %d, want 40960", caps.ContextWindow)
	}
	if caps.Source.ContextWindow != "endpoint" {
		t.Errorf("ctx source = %q, want endpoint", caps.Source.ContextWindow)
	}
	if caps.MaxOutputTokens != 6000 {
		t.Errorf("MaxOutputTokens = %d, want 6000", caps.MaxOutputTokens)
	}
	if caps.Source.MaxOutputTokens != "endpoint" {
		t.Errorf("maxout source = %q, want endpoint", caps.Source.MaxOutputTokens)
	}
	// No tool flag reported and "my-model" is not in the table => default.
	if caps.Source.SupportsTools != "default" {
		t.Errorf("tools source = %q, want default", caps.Source.SupportsTools)
	}
}

func TestProbeParsesEndpointToolFlag(t *testing.T) {
	// max_model_len alias + explicit supports_tools:false.
	srv, _ := newModelsServer(t, `{"data":[{"id":"m","max_model_len":65536,"supports_tools":false}]}`)
	c := New(srv.URL, "", "m", 5*time.Second, false)

	caps := c.ProbeCapabilities(context.Background())
	if caps.ContextWindow != 65536 || caps.Source.ContextWindow != "endpoint" {
		t.Errorf("ctx = %d (%s), want 65536 endpoint", caps.ContextWindow, caps.Source.ContextWindow)
	}
	if caps.SupportsTools != false || caps.Source.SupportsTools != "endpoint" {
		t.Errorf("tools = %t (%s), want false endpoint", caps.SupportsTools, caps.Source.SupportsTools)
	}
}

func TestProbeFallsBackToTableForKnownModel(t *testing.T) {
	// Endpoint serves the model id but reports NO capability fields. The
	// built-in table for gpt-4o should fill them in.
	srv, _ := newModelsServer(t, `{"data":[{"id":"openai/gpt-4o-mini"}]}`)
	c := New(srv.URL, "", "openai/gpt-4o-mini", 5*time.Second, false)

	caps := c.ProbeCapabilities(context.Background())
	if caps.ContextWindow != 128000 || caps.Source.ContextWindow != "table" {
		t.Errorf("ctx = %d (%s), want 128000 table", caps.ContextWindow, caps.Source.ContextWindow)
	}
	if caps.MaxOutputTokens != 16384 || caps.Source.MaxOutputTokens != "table" {
		t.Errorf("maxout = %d (%s), want 16384 table", caps.MaxOutputTokens, caps.Source.MaxOutputTokens)
	}
	if !caps.SupportsTools || caps.Source.SupportsTools != "table" {
		t.Errorf("tools = %t (%s), want true table", caps.SupportsTools, caps.Source.SupportsTools)
	}
}

func TestProbeDefaultsForUnknownModel(t *testing.T) {
	// Unknown model, no capability fields anywhere => conservative defaults.
	srv, _ := newModelsServer(t, `{"data":[{"id":"totally-unknown-9000"}]}`)
	c := New(srv.URL, "", "totally-unknown-9000", 5*time.Second, false)

	caps := c.ProbeCapabilities(context.Background())
	if caps.ContextWindow != defaultContextWindow || caps.Source.ContextWindow != "default" {
		t.Errorf("ctx = %d (%s), want %d default", caps.ContextWindow, caps.Source.ContextWindow, defaultContextWindow)
	}
	if caps.MaxOutputTokens != defaultMaxOutputTokens || caps.Source.MaxOutputTokens != "default" {
		t.Errorf("maxout = %d (%s), want %d default", caps.MaxOutputTokens, caps.Source.MaxOutputTokens, defaultMaxOutputTokens)
	}
	if caps.SupportsTools != defaultSupportsTools || caps.Source.SupportsTools != "default" {
		t.Errorf("tools = %t (%s), want %t default", caps.SupportsTools, caps.Source.SupportsTools, defaultSupportsTools)
	}
}

func TestProbeDegradesOnNetworkError(t *testing.T) {
	// Point at a closed server: the probe must not error, and should degrade
	// to the table (known model) for capabilities.
	srv, _ := newModelsServer(t, `{}`)
	url := srv.URL
	srv.Close() // force a connection failure

	c := New(url, "", "claude-3-5-sonnet", 200*time.Millisecond, false)
	caps := c.ProbeCapabilities(context.Background())
	if caps.ContextWindow != 200000 || caps.Source.ContextWindow != "table" {
		t.Errorf("ctx = %d (%s), want 200000 table on net error", caps.ContextWindow, caps.Source.ContextWindow)
	}
}

func TestProbeDegradesOnParseError(t *testing.T) {
	// Garbage body => parse error => degrade to default for an unknown model.
	srv, _ := newModelsServer(t, `{not json at all`)
	c := New(srv.URL, "", "mystery-model", 5*time.Second, false)
	caps := c.ProbeCapabilities(context.Background())
	if caps.Source.ContextWindow != "default" {
		t.Errorf("ctx source = %q, want default on parse error", caps.Source.ContextWindow)
	}
}

func TestProbeRejectsAbsurdContextWindow(t *testing.T) {
	// A hostile server returns a 2-billion context window and a negative
	// max output. Both must be rejected; resolution falls back to the table.
	srv, _ := newModelsServer(t, `{"data":[{"id":"gpt-4o","context_length":2000000000,"max_output_tokens":-5}]}`)
	c := New(srv.URL, "", "gpt-4o", 5*time.Second, false)

	caps := c.ProbeCapabilities(context.Background())
	if caps.ContextWindow != 128000 || caps.Source.ContextWindow != "table" {
		t.Errorf("ctx = %d (%s), want clamped to table 128000", caps.ContextWindow, caps.Source.ContextWindow)
	}
	if caps.MaxOutputTokens != 16384 || caps.Source.MaxOutputTokens != "table" {
		t.Errorf("maxout = %d (%s), want clamped to table 16384", caps.MaxOutputTokens, caps.Source.MaxOutputTokens)
	}
}

func TestProbeCachesPerEndpointModel(t *testing.T) {
	srv, hits := newModelsServer(t, `{"data":[{"id":"m","context_length":12345}]}`)
	c := New(srv.URL, "", "m", 5*time.Second, false)

	first := c.ProbeCapabilities(context.Background())
	second := c.ProbeCapabilities(context.Background())

	if got := atomic.LoadInt32(hits); got != 1 {
		t.Errorf("/v1/models hit %d times, want 1 (second call should be cached)", got)
	}
	if first != second {
		t.Errorf("cached result differs: %+v vs %+v", first, second)
	}
	if first.ContextWindow != 12345 {
		t.Errorf("ContextWindow = %d, want 12345", first.ContextWindow)
	}
}

func TestProbeRespectsContextCancellation(t *testing.T) {
	srv, _ := newModelsServer(t, `{"data":[{"id":"claude-3-haiku"}]}`)
	c := New(srv.URL, "", "claude-3-haiku", 5*time.Second, false)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	caps := c.ProbeCapabilities(ctx)
	// Probe fails (cancelled) but degrades to the table for a known model.
	if caps.ContextWindow != 200000 || caps.Source.ContextWindow != "table" {
		t.Errorf("ctx = %d (%s), want table fallback on cancellation", caps.ContextWindow, caps.Source.ContextWindow)
	}
}
