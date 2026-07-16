import Foundation

/// A decoded frame from the `/session/{id}/stream` SSE endpoint.
///
/// One case per typed event in `docs/mobile-serve.md` ("Typed events" table).
/// Field names/JSON keys are matched exactly to the server contract:
/// - `usage` carries the server's raw (capitalized, no-JSON-tag) Go struct
///   field names: `PromptTokens`/`CompletionTokens`/`TotalTokens`/`CostUSD`.
/// - `approval-needed` data is `{"approval_id":...,"tool":...,"args":...}`.
/// - `tool_call` data is `{"name":...,"args":...}`; `tool_result` data is
///   `{"name":...,"text":...}`.
enum AgentEvent: Equatable {
    /// `token` — a streamed text delta of the model's output. Raw text, not JSON.
    case token(String)
    /// `assistant` — the assistant's final prose for the turn. Raw text, not JSON.
    case assistant(String)
    /// `tool_call` — one tool invocation. `args` is the tool's raw (unparsed) args string.
    case toolCall(name: String, args: String)
    /// `tool_result` — the result of a tool invocation.
    case toolResult(name: String, text: String)
    /// `usage` — running per-session usage totals for the turn.
    case usage(prompt: Int, completion: Int, total: Int, costUSD: Double)
    /// `approval-needed` — a gated tool call blocked on `POST /session/{id}/approve`.
    case approvalNeeded(approvalID: String, tool: String, args: String)
    /// `done` — always sent exactly once, last. Definitive end-of-turn signal.
    case done
    /// `error` — at most once; the server never discloses the real error text.
    case error(String)
}
