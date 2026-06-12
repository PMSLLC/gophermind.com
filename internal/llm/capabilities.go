package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// Capabilities describes what the active model+endpoint can do, so the agent
// can adapt truncation and iteration limits to the real backend instead of
// guessing. Values are resolved in priority order — endpoint-reported, then a
// built-in table of known model families, then conservative defaults — and the
// Source fields record where each came from for observability.
type Capabilities struct {
	ContextWindow   int  // max input tokens the model accepts
	MaxOutputTokens int  // max tokens the model will generate in one turn
	SupportsTools   bool // native tool/function calling

	// Source records the provenance of each resolved value: "endpoint" (the
	// /models response reported it), "table" (matched the built-in family
	// table), or "default" (conservative fallback). Useful for debugging which
	// path produced a given capability.
	Source CapabilitySource
}

// CapabilitySource records, per capability, where its value was resolved from.
type CapabilitySource struct {
	ContextWindow   string `json:"context_window"`
	MaxOutputTokens string `json:"max_output_tokens"`
	SupportsTools   string `json:"supports_tools"`
}

// Conservative final-fallback capabilities. Chosen low enough to work safely on
// any unknown model: a small context window and output cap, with tool calling
// assumed available (the harness is tool-driven; most modern endpoints support
// it, and a model that ignores tools simply never emits tool_calls).
const (
	defaultContextWindow   = 8192
	defaultMaxOutputTokens = 2048
	defaultSupportsTools   = true
)

// Sanity bounds for capability values reported by an UNTRUSTED endpoint. A
// hostile or buggy server must not be able to set a multi-billion context
// window (which could later drive huge allocations) or a non-positive one.
// Anything outside [1, max] is ignored and resolution falls back to the table
// or default.
const (
	maxSaneContextWindow   = 10_000_000
	maxSaneMaxOutputTokens = 10_000_000
)

// modelCapability is a built-in capability record for a known model family.
type modelCapability struct {
	contextWindow   int
	maxOutputTokens int
	supportsTools   bool
}

// knownModels maps a lowercase model-name substring to capabilities for common
// families. Matched by substring so endpoint-specific prefixes/suffixes (e.g.
// "openai/gpt-4o-mini", "gpt-4o-mini-2024-07-18") still resolve. The longest
// matching key wins so more specific families take precedence.
var knownModels = map[string]modelCapability{
	"gpt-4o":            {128000, 16384, true},
	"gpt-4-turbo":       {128000, 4096, true},
	"gpt-4":             {8192, 4096, true},
	"gpt-3.5-turbo":     {16385, 4096, true},
	"claude-3-5-sonnet": {200000, 8192, true},
	"claude-3-5-haiku":  {200000, 8192, true},
	"claude-3-opus":     {200000, 4096, true},
	"claude-3-sonnet":   {200000, 4096, true},
	"claude-3-haiku":    {200000, 4096, true},
	"llama-3.1":         {131072, 4096, true},
	"llama-3":           {8192, 4096, true},
	"mistral":           {32768, 4096, true},
	"mixtral":           {32768, 4096, true},
	"qwen2.5":           {32768, 8192, true},
	"qwen2":             {32768, 8192, true},
	"deepseek":          {65536, 8192, true},
}

// modelsCapabilityResponse captures the (varied) capability fields different
// OpenAI-compatible servers expose on GET /v1/models. None are guaranteed;
// every field is parsed defensively and may be absent/zero.
//
// Field aliases seen in the wild:
//   - context window: context_length (vLLM/together), max_model_len (vLLM),
//     context_window (some shims), max_context_length
//   - max output: max_output_tokens, max_tokens
//   - tools: capabilities.tools / supports_tools / tool_use (bool)
type modelsCapabilityResponse struct {
	Data []modelCapabilityEntry `json:"data"`
}

type modelCapabilityEntry struct {
	ID string `json:"id"`

	ContextLength    int `json:"context_length"`
	MaxModelLen      int `json:"max_model_len"`
	ContextWindow    int `json:"context_window"`
	MaxContextLength int `json:"max_context_length"`

	MaxOutputTokens int `json:"max_output_tokens"`
	MaxTokens       int `json:"max_tokens"`

	// Tool support is reported under several shapes; *bool distinguishes
	// "absent" (nil => unknown, don't override) from an explicit false.
	SupportsTools *bool `json:"supports_tools"`
	ToolUse       *bool `json:"tool_use"`
	Capabilities  *struct {
		Tools *bool `json:"tools"`
	} `json:"capabilities"`
}

// reportedContextWindow returns the first positive context-window alias the
// entry carries, or 0 when none is present.
func (e modelCapabilityEntry) reportedContextWindow() int {
	for _, v := range []int{e.ContextLength, e.MaxModelLen, e.ContextWindow, e.MaxContextLength} {
		if v > 0 {
			return v
		}
	}
	return 0
}

// reportedMaxOutput returns the first positive max-output alias, or 0.
func (e modelCapabilityEntry) reportedMaxOutput() int {
	for _, v := range []int{e.MaxOutputTokens, e.MaxTokens} {
		if v > 0 {
			return v
		}
	}
	return 0
}

// reportedTools returns the endpoint's tool-support flag, or nil when the entry
// says nothing about tools (so resolution does not override table/default).
func (e modelCapabilityEntry) reportedTools() *bool {
	if e.SupportsTools != nil {
		return e.SupportsTools
	}
	if e.ToolUse != nil {
		return e.ToolUse
	}
	if e.Capabilities != nil && e.Capabilities.Tools != nil {
		return e.Capabilities.Tools
	}
	return nil
}

// maxModelsResponseBytes caps how much of the /v1/models body is read so a
// hostile server cannot make probing exhaust memory with an unbounded response.
const maxModelsResponseBytes = 1 << 20 // 1 MiB — far larger than any real model list

// capCache is an in-memory, per Client cache of probed capabilities keyed by
// baseURL+model, so repeated lookups in one session never re-probe.
var (
	capCacheMu sync.Mutex
)

// capCacheKey is the per-endpoint+model cache key.
func capCacheKey(baseURL, model string) string { return baseURL + "\x00" + model }

// ProbeCapabilities resolves the capabilities of the client's active model
// against its endpoint. It never returns an error: any probe failure (network,
// HTTP, parse) degrades to the built-in table and then conservative defaults,
// so it is always safe to call at startup. Results are cached in-memory per
// endpoint+model on the Client, so a second call returns without re-probing.
//
// Resolution per capability is independent and priority-ordered:
//
//	endpoint-reported (validated)  >  built-in family table  >  conservative default
func (c *Client) ProbeCapabilities(ctx context.Context) Capabilities {
	key := capCacheKey(c.BaseURL, c.Model)

	capCacheMu.Lock()
	if c.capCache == nil {
		c.capCache = make(map[string]Capabilities)
	}
	if cached, ok := c.capCache[key]; ok {
		capCacheMu.Unlock()
		return cached
	}
	capCacheMu.Unlock()

	// Probe is best-effort; entry is nil when the endpoint reports nothing
	// usable (error, no matching model, no capability fields).
	entry := c.fetchModelEntry(ctx)
	caps := resolveCapabilities(c.Model, entry)

	capCacheMu.Lock()
	c.capCache[key] = caps
	capCacheMu.Unlock()
	return caps
}

// fetchModelEntry queries GET /v1/models and returns the entry matching the
// client's model (or the first entry when no id matches). It returns nil on any
// error so the caller degrades gracefully — it never surfaces an error and
// never logs the API key or full response with credentials.
func (c *Client) fetchModelEntry(ctx context.Context) *modelCapabilityEntry {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/models", nil)
	if err != nil {
		return nil
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil
	}

	// Bound the read: a hostile server must not exhaust memory.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxModelsResponseBytes))
	if err != nil {
		return nil
	}

	var parsed modelsCapabilityResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}
	if len(parsed.Data) == 0 {
		return nil
	}
	// Prefer the entry whose id matches the active model; fall back to the
	// first served model otherwise.
	for i := range parsed.Data {
		if parsed.Data[i].ID == c.Model {
			return &parsed.Data[i]
		}
	}
	return &parsed.Data[0]
}

// resolveCapabilities merges, per capability and in priority order, the
// endpoint-reported values (validated/clamped), the built-in family table, and
// conservative defaults. entry may be nil (no usable endpoint data).
func resolveCapabilities(model string, entry *modelCapabilityEntry) Capabilities {
	tbl, hasTable := lookupKnownModel(model)
	caps := Capabilities{}

	// Context window.
	if v, ok := validContextWindow(entry); ok {
		caps.ContextWindow = v
		caps.Source.ContextWindow = "endpoint"
	} else if hasTable {
		caps.ContextWindow = tbl.contextWindow
		caps.Source.ContextWindow = "table"
	} else {
		caps.ContextWindow = defaultContextWindow
		caps.Source.ContextWindow = "default"
	}

	// Max output tokens.
	if v, ok := validMaxOutput(entry); ok {
		caps.MaxOutputTokens = v
		caps.Source.MaxOutputTokens = "endpoint"
	} else if hasTable {
		caps.MaxOutputTokens = tbl.maxOutputTokens
		caps.Source.MaxOutputTokens = "table"
	} else {
		caps.MaxOutputTokens = defaultMaxOutputTokens
		caps.Source.MaxOutputTokens = "default"
	}

	// Tool support.
	if entry != nil && entry.reportedTools() != nil {
		caps.SupportsTools = *entry.reportedTools()
		caps.Source.SupportsTools = "endpoint"
	} else if hasTable {
		caps.SupportsTools = tbl.supportsTools
		caps.Source.SupportsTools = "table"
	} else {
		caps.SupportsTools = defaultSupportsTools
		caps.Source.SupportsTools = "default"
	}

	return caps
}

// validContextWindow returns the endpoint-reported context window only when it
// is present and within sane bounds; an untrusted, non-positive, or absurd
// value is rejected (ok=false) so resolution falls back to table/default.
func validContextWindow(entry *modelCapabilityEntry) (int, bool) {
	if entry == nil {
		return 0, false
	}
	v := entry.reportedContextWindow()
	if v <= 0 || v > maxSaneContextWindow {
		return 0, false
	}
	return v, true
}

// validMaxOutput returns the endpoint-reported max output only when present and
// within sane bounds; otherwise ok=false.
func validMaxOutput(entry *modelCapabilityEntry) (int, bool) {
	if entry == nil {
		return 0, false
	}
	v := entry.reportedMaxOutput()
	if v <= 0 || v > maxSaneMaxOutputTokens {
		return 0, false
	}
	return v, true
}

// lookupKnownModel finds the most specific built-in family entry whose key is a
// substring of the (lowercased) model name. The longest matching key wins.
func lookupKnownModel(model string) (modelCapability, bool) {
	name := strings.ToLower(model)
	var bestKey string
	var best modelCapability
	for k, v := range knownModels {
		if strings.Contains(name, k) && len(k) > len(bestKey) {
			bestKey = k
			best = v
		}
	}
	if bestKey == "" {
		return modelCapability{}, false
	}
	return best, true
}

// String renders capabilities compactly for logs/debug. It never contains any
// secret material — only resolved capability values and their sources.
func (c Capabilities) String() string {
	return fmt.Sprintf("ctx=%d(%s) maxout=%d(%s) tools=%t(%s)",
		c.ContextWindow, c.Source.ContextWindow,
		c.MaxOutputTokens, c.Source.MaxOutputTokens,
		c.SupportsTools, c.Source.SupportsTools)
}
