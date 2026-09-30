// Package provider defines how the executor talks to an LLM. Every free provider
// (Groq, Together, Gemini, OpenRouter, a local Ollama, and so on) is one
// implementation. All HTTP goes through the harness proxy; providers must accept
// an *http.Client and never build their own.
package provider

import (
	"context"
	"net/http"
	"time"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role    Role
	Content string
}

type Request struct {
	Model     string
	Messages  []Message
	MaxTokens int
	// MaxGrownTokens, when positive, caps the doubled budget the router retries
	// with after a truncated reply; zero means the router's default cap.
	MaxGrownTokens int
	Temperature    float64 // executor uses 0 for implementation, 0.2 for planning
	// StopSequences are optional; the executor uses them to cut off trailing prose after a code fence.
	StopSequences []string
}

type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

type Response struct {
	Text     string
	Usage    Usage
	Model    string // the model the provider actually served, for the attempt log
	Duration time.Duration
}

// Provider is one LLM API. Name() is the provider segment of "provider/model" in gophermind.yaml.
type Provider interface {
	Name() string
	// Models returns the model IDs this provider is configured with and their context sizes.
	Models() []ModelInfo
	Complete(ctx context.Context, req Request) (Response, error)
}

type ModelInfo struct {
	ID            string
	ContextTokens int
}

// Config is what the harness passes when constructing a provider from gophermind.yaml.
// APIKey is resolved from the vault at construction time and must not be logged.
type Config struct {
	Name          string
	BaseURL       string
	APIKey        string
	MaxConcurrent int
	HTTPClient    *http.Client // routed through the proxy; never nil
	Models        []ModelInfo
	// ReasoningEffort is sent as the top-level reasoning_effort field when set
	// (none, low, medium, high). Thinking models such as qwen3.6 need "none" to
	// answer in content instead of spending the budget on hidden reasoning.
	ReasoningEffort string
}

// Typed errors the router switches on. Any other error is treated as Transient.

// ErrRateLimited: the router puts the provider in cooldown for RetryAfter (or the configured
// default) and moves to the next model. The attempt is logged with VerdictError and does not
// count against the chain.
type ErrRateLimited struct{ RetryAfter time.Duration }

func (e ErrRateLimited) Error() string { return "provider: rate limited" }

// ErrContextTooLong: the prompt exceeds the model's window. The router does not retry this
// model; if every model in the tier reports it, the node goes to needs_revision with
// failure_reason "context_too_long" so the planner splits it.
type ErrContextTooLong struct{ PromptTokens, Limit int }

func (e ErrContextTooLong) Error() string { return "provider: context too long" }

// ErrAuth: bad or missing API key. The router marks the provider unavailable for the rest of
// the run and warns once. It is a configuration error, not a model failure.
type ErrAuth struct{ Provider string }

func (e ErrAuth) Error() string { return "provider: authentication failed" }

// ErrTransient: 5xx, connection reset, timeout. Retried with backoff up to
// rate_limits.backoff_max_seconds, then treated like ErrRateLimited.
type ErrTransient struct{ Cause error }

func (e ErrTransient) Error() string { return "provider: transient: " + e.Cause.Error() }
func (e ErrTransient) Unwrap() error { return e.Cause }
