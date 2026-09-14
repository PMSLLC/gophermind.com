package serve

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
)

// ServeMetrics holds process counters exported on the webhook's /metrics
// endpoint in Prometheus text exposition format, for dashboards and alerting.
type ServeMetrics struct {
	requests atomic.Int64
	errors   atomic.Int64
	// PromptTokens is the running total of prompt tokens consumed, added to
	// by the caller after each turn.
	PromptTokens atomic.Int64
	// CompletionTokens is the running total of completion tokens produced,
	// added to by the caller after each turn.
	CompletionTokens atomic.Int64
}

// counter renders one Prometheus counter with HELP/TYPE metadata.
func counter(b *strings.Builder, name, help string, v int64) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s counter\n%s %d\n", name, help, name, name, v)
}

// Render returns the metrics in Prometheus text exposition format.
func (m *ServeMetrics) Render() string {
	var b strings.Builder
	counter(&b, "gophermind_requests_total", "Total webhook run requests.", m.requests.Load())
	counter(&b, "gophermind_errors_total", "Total webhook run errors.", m.errors.Load())
	counter(&b, "gophermind_prompt_tokens_total", "Total prompt tokens consumed.", m.PromptTokens.Load())
	counter(&b, "gophermind_completion_tokens_total", "Total completion tokens produced.", m.CompletionTokens.Load())
	return b.String()
}

// metricsHandler serves the metrics (unauthenticated, like the health probes).
func metricsHandler(m *ServeMetrics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte(m.Render()))
	}
}
