package llm

import (
	"errors"
	"testing"
)

func TestContextLimitFromError(t *testing.T) {
	// The exact error a llama.cpp server returned in the field.
	real := errors.New(`iteration 11: status 400: {"error":{"code":400,"message":"request (116709 tokens) exceeds the available context size (98304 tokens), try increasing it","type":"exceed_context_size_error","n_prompt_tokens":116709,"n_ctx":98304}}`)
	if n, ok := ContextLimitFromError(real); !ok || n != 98304 {
		t.Errorf("llama.cpp error: got (%d, %v), want (98304, true)", n, ok)
	}

	for name, err := range map[string]error{
		"nil":                           nil,
		"other 400":                     errors.New(`status 400: {"error":{"message":"bad tools schema"}}`),
		"rate limit":                    errors.New(`status 429: {"error":{"type":"rate_limit"}}`),
		"n_ctx alone (not an overflow)": errors.New(`status 500: {"n_ctx":4096}`),
	} {
		if n, ok := ContextLimitFromError(err); ok {
			t.Errorf("%s: got (%d, true), want not ok", name, n)
		}
	}
}
