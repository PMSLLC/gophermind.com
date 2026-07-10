package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServeMetricsRender(t *testing.T) {
	m := &serveMetrics{}
	m.requests.Add(3)
	m.errors.Add(1)
	m.promptTokens.Add(1200)
	m.completionTokens.Add(340)

	out := m.Render()
	// Prometheus text exposition format: HELP/TYPE lines + metric samples.
	for _, want := range []string{
		"# HELP gophermind_requests_total",
		"# TYPE gophermind_requests_total counter",
		"gophermind_requests_total 3",
		"gophermind_errors_total 1",
		"gophermind_prompt_tokens_total 1200",
		"gophermind_completion_tokens_total 340",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("metrics output missing %q:\n%s", want, out)
		}
	}
}

func TestMetricsHandler(t *testing.T) {
	m := &serveMetrics{}
	m.requests.Add(5)
	rr := httptest.NewRecorder()
	metricsHandler(m)(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rr.Code != 200 {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "gophermind_requests_total 5") {
		t.Errorf("handler body missing counter:\n%s", rr.Body.String())
	}
}
