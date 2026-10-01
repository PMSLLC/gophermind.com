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

// NewHTTPClient returns the client provider calls should use: it never follows a
// redirect (a 307 would re-POST the prompt to another host), ignores the proxy
// environment and gives up after timeout (0 means the caller's context alone
// bounds the call).
func NewHTTPClient(timeout time.Duration) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	return &http.Client{Timeout: timeout, Transport: tr, CheckRedirect: refuseRedirect}
}

var errRedirect = errors.New("the server answered with a redirect")

func refuseRedirect(*http.Request, []*http.Request) error { return errRedirect }

// NewOpenAI builds the client. cfg.HTTPClient is used as given, except that it never
// follows a redirect; when it is nil NewHTTPClient(0) is used.
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
	// ReasoningEffort is omitted when empty.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

type wireReply struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      wireMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error json.RawMessage `json:"error"`
}

func (o *OpenAI) Complete(ctx context.Context, req Request) (Response, error) {
	wr := wireRequest{Model: req.Model, MaxTokens: req.MaxTokens, Temperature: req.Temperature, Stop: req.StopSequences, ReasoningEffort: o.cfg.ReasoningEffort}
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
		client = NewHTTPClient(0)
	} else {
		// Whatever client the caller built, a redirect is never followed: it would
		// re-send the prompt (a 307 or 308 keeps the POST body) to another host.
		c := *client
		c.CheckRedirect = refuseRedirect
		client = &c
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
	// A reply cut at the token limit is never a success, whether or not some
	// text arrived: a partial reply is not usable and a bigger budget may fix it.
	if wp.Choices[0].FinishReason == "length" {
		return Response{}, ErrTruncated{Provider: o.cfg.Name}
	}
	if strings.TrimSpace(wp.Choices[0].Message.Content) == "" {
		return Response{}, ErrEmptyReply{Provider: o.cfg.Name}
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
