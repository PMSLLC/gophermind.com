package main

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
)

// serveMetrics holds process counters exported on the webhook's /metrics
// endpoint in Prometheus text exposition format, for dashboards and alerting.
type serveMetrics struct {
	requests         atomic.Int64
	errors           atomic.Int64
	promptTokens     atomic.Int64
	completionTokens atomic.Int64
}

// counter renders one Prometheus counter with HELP/TYPE metadata.
func counter(b *strings.Builder, name, help string, v int64) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s counter\n%s %d\n", name, help, name, name, v)
}

// Render returns the metrics in Prometheus text exposition format.
func (m *serveMetrics) Render() string {
	var b strings.Builder
	counter(&b, "gophermind_requests_total", "Total webhook run requests.", m.requests.Load())
	counter(&b, "gophermind_errors_total", "Total webhook run errors.", m.errors.Load())
	counter(&b, "gophermind_prompt_tokens_total", "Total prompt tokens consumed.", m.promptTokens.Load())
	counter(&b, "gophermind_completion_tokens_total", "Total completion tokens produced.", m.completionTokens.Load())
	return b.String()
}

// metricsHandler serves the metrics (unauthenticated, like the health probes).
func metricsHandler(m *serveMetrics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte(m.Render()))
	}
}
