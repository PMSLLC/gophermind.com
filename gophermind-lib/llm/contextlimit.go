package llm

import (
	"regexp"
	"strconv"
	"strings"
)

var nCtxField = regexp.MustCompile(`"n_ctx"\s*:\s*(\d+)`)

// ContextLimitFromError reports the context window a server stated when it
// rejected a request as too large. llama.cpp answers with an
// exceed_context_size_error whose body carries the real n_ctx, which can be
// smaller than the model's trained window or whatever a lookup table assumed.
// It returns ok=false for any other error, including one that merely mentions
// n_ctx.
func ContextLimitFromError(err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	msg := err.Error()
	if !strings.Contains(msg, "exceed_context_size_error") {
		return 0, false
	}
	m := nCtxField.FindStringSubmatch(msg)
	if m == nil {
		return 0, false
	}
	n, convErr := strconv.Atoi(m[1])
	if convErr != nil || n <= 0 {
		return 0, false
	}
	return n, true
}
