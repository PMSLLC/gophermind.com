package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// maxReply bounds how much of a reply body is read.
const maxReply = 8 << 20

// OpenAI talks to any server that speaks the OpenAI chat completions protocol:
// Ollama, Kilo Code's gateway, OVHcloud, and most cloud APIs.
type OpenAI struct{ cfg Config }

var _ Provider = (*OpenAI)(nil)

// NewOpenAI builds the client. cfg.HTTPClient is used as given; when it is nil
// http.DefaultClient is used (the harness proxy plan replaces this).
func NewOpenAI(cfg Config) Provider { return &OpenAI{cfg: cfg} }

func (o *OpenAI) Name() string        { return o.cfg.Name }
func (o *OpenAI) Models() []ModelInfo { return o.cfg.Models }

type wireMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type wireRequest struct {
	Model       string        `json:"model"`
	Messages    []wireMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature"`
	Stop        []string      `json:"stop,omitempty"`
}

type wireReply struct {
	Model   string `json:"model"`
	Choices []struct {
		Message wireMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error json.RawMessage `json:"error"`
}

func (o *OpenAI) Complete(ctx context.Context, req Request) (Response, error) {
	wr := wireRequest{Model: req.Model, MaxTokens: req.MaxTokens, Temperature: req.Temperature, Stop: req.StopSequences}
	for _, m := range req.Messages {
		wr.Messages = append(wr.Messages, wireMessage{Role: string(m.Role), Content: m.Content})
	}
	body, err := json.Marshal(wr)
	if err != nil {
		return Response{}, fmt.Errorf("provider %s: %w", o.cfg.Name, err)
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(o.cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("provider %s: bad base_url", o.cfg.Name)
	}
	hreq.Header.Set("Content-Type", "application/json")
	if o.cfg.APIKey != "" {
		hreq.Header.Set("Authorization", "Bearer "+o.cfg.APIKey)
	}
	client := o.cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	start := time.Now()
	resp, err := client.Do(hreq)
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return Response{}, cerr
		}
		// A *url.Error carries the URL; report only the underlying cause.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return Response{}, ErrTransient{Cause: fmt.Errorf("provider %s: %w", o.cfg.Name, err)}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxReply))
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return Response{}, cerr
		}
		return Response{}, ErrTransient{Cause: fmt.Errorf("provider %s: reading reply: %w", o.cfg.Name, err)}
	}

	if resp.StatusCode != http.StatusOK {
		return Response{}, o.statusError(resp, raw, req)
	}
	var wp wireReply
	if err := json.Unmarshal(raw, &wp); err != nil {
		return Response{}, ErrTransient{Cause: fmt.Errorf("provider %s: reply is not JSON", o.cfg.Name)}
	}
	if len(wp.Error) > 0 && string(wp.Error) != "null" {
		return Response{}, ErrTransient{Cause: fmt.Errorf("provider %s: reply carried an error object", o.cfg.Name)}
	}
	if len(wp.Choices) == 0 {
		return Response{}, ErrTransient{Cause: fmt.Errorf("provider %s: reply had no choices", o.cfg.Name)}
	}
	served := wp.Model
	if served == "" {
		served = req.Model
	}
	return Response{
		Text:     wp.Choices[0].Message.Content,
		Usage:    Usage{PromptTokens: wp.Usage.PromptTokens, CompletionTokens: wp.Usage.CompletionTokens},
		Model:    served,
		Duration: time.Since(start),
	}, nil
}

// statusError maps a non-200 reply to a typed error. Messages name the
// provider and the status only: a reply body can echo the prompt, and a
// prompt, a key, or a secret must never end up in an error string.
func (o *OpenAI) statusError(resp *http.Response, raw []byte, req Request) error {
	text := strings.ToLower(string(raw))
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		var after time.Duration
		if secs, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); err == nil && secs > 0 {
			after = time.Duration(secs) * time.Second
		}
		return ErrRateLimited{RetryAfter: after}
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return ErrAuth{Provider: o.cfg.Name}
	case resp.StatusCode == http.StatusBadRequest && looksLikeContextError(text):
		limit := 0
		for _, m := range o.cfg.Models {
			if m.ID == req.Model {
				limit = m.ContextTokens
			}
		}
		return ErrContextTooLong{Limit: limit}
	case resp.StatusCode == http.StatusNotFound && strings.Contains(text, "model") &&
		(strings.Contains(text, "not found") || strings.Contains(text, "does not exist")):
		return ErrModelNotFound{Model: req.Model}
	case resp.StatusCode >= 500:
		return ErrTransient{Cause: fmt.Errorf("provider %s: HTTP %d", o.cfg.Name, resp.StatusCode)}
	}
	return fmt.Errorf("provider %s: HTTP %d", o.cfg.Name, resp.StatusCode)
}

func looksLikeContextError(lower string) bool {
	for _, marker := range []string{"context length", "context_length", "maximum context", "context window", "too many tokens", "prompt is too long"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
