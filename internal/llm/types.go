// Package llm is a minimal client for an OpenAI-compatible chat-completions
// endpoint that supports native tool calling. The wire types mirror the
// OpenAI schema exactly so the served model can emit tool_calls directly.
package llm

// Message is one entry in the conversation. Roles: system, user, assistant, tool.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"` // empty on assistant tool-call turns
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"` // set on role:"tool" results
	Name       string     `json:"name,omitempty"`         // tool name on a result (optional)
}

// ToolCall is a single function invocation requested by the assistant.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // always "function"
	Function FunctionCall `json:"function"`
}

// FunctionCall carries the tool name and its arguments. Arguments is a JSON
// *string* on the wire (e.g. `{"path":"main.go"}`), not a nested object —
// each tool unmarshals it itself.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool is a function definition advertised to the model in each request.
type Tool struct {
	Type     string   `json:"type"` // always "function"
	Function Function `json:"function"`
}

// Function describes a callable tool and its JSON-Schema parameters.
type Function struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"` // JSON Schema object
}

// ChatRequest is the request body for POST /v1/chat/completions.
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []Tool    `json:"tools,omitempty"`
	ToolChoice  any       `json:"tool_choice,omitempty"` // string "auto"/"none"/"required" or {"type":"function","function":{"name":"..."}}
	Temperature float64   `json:"temperature"`
	// TopP is nucleus-sampling mass. It is a pointer so an unset top_p (nil) is
	// omitted from the wire entirely, while an explicit 0-or-greater value is
	// sent. Temperature, by contrast, is always sent (0 is the default).
	TopP          *float64       `json:"top_p,omitempty"`
	Stream        bool           `json:"stream"`
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`
}

// StreamOptions tweaks streaming behavior. IncludeUsage asks the server to emit
// a final SSE chunk carrying the usage block (omitted by default when streaming).
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// Usage is the token-accounting block returned with a chat completion. Counts
// are reported by the server and are treated as untrusted: negative or absurd
// values are clamped before they reach the session accumulator.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatResponse is the (non-streaming) response body.
type ChatResponse struct {
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage `json:"usage,omitempty"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}
