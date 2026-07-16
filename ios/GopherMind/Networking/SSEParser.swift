import Foundation

/// A pure, stateful line parser for the SSE stream described in
/// `docs/mobile-serve.md`: `event: <name>\ndata: <line>\n[data: <line>\n...]\n\n`.
///
/// No networking inside — feed it lines (or a raw multi-line string) and it
/// yields decoded `AgentEvent`s as frames complete on a blank line. This
/// makes it directly unit-testable against a `String` fixture.
struct SSEParser {
    private var eventName: String?
    private var dataLines: [String] = []
    private var sawAnyLineThisFrame = false

    init() {}

    /// Feeds one line (no trailing newline) of the SSE stream. Returns the
    /// decoded event if this line was the blank line that completed a frame,
    /// otherwise `nil` (still accumulating, or the completed frame's event
    /// was unrecognized and is being skipped leniently).
    mutating func feed(line: String) -> AgentEvent? {
        if line.isEmpty {
            defer {
                eventName = nil
                dataLines = []
                sawAnyLineThisFrame = false
            }
            // Ignore stray blank lines that don't terminate an accumulated frame
            // (e.g. consecutive blank lines between frames).
            guard sawAnyLineThisFrame else { return nil }

            // No `event:` line defaults to the standard SSE event name "message".
            let name = eventName ?? "message"
            let data = dataLines.joined(separator: "\n")
            return Self.decode(event: name, data: data)
        }

        sawAnyLineThisFrame = true

        if line.hasPrefix("event:") {
            eventName = Self.stripFieldPrefix(line, prefix: "event:")
        } else if line.hasPrefix("data:") {
            dataLines.append(Self.stripFieldPrefix(line, prefix: "data:"))
        }
        // Other fields (id:, retry:, comments starting with ':') are ignored.

        return nil
    }

    /// Convenience for tests/fixtures: splits `text` on "\n" and feeds each
    /// line in order, returning every decoded event in sequence.
    mutating func feed(_ text: String) -> [AgentEvent] {
        var results: [AgentEvent] = []
        for line in text.split(separator: "\n", omittingEmptySubsequences: false) {
            if let event = feed(line: String(line)) {
                results.append(event)
            }
        }
        return results
    }

    private static func stripFieldPrefix(_ line: String, prefix: String) -> String {
        var value = String(line.dropFirst(prefix.count))
        if value.hasPrefix(" ") {
            value.removeFirst()
        }
        return value
    }

    // MARK: - Frame -> AgentEvent

    private static func decode(event: String, data: String) -> AgentEvent? {
        switch event {
        case "token":
            return .token(data)
        case "assistant":
            return .assistant(data)
        case "tool_call":
            guard let payload = try? JSONDecoder().decode(ToolCallPayload.self, from: Data(data.utf8)) else {
                return nil
            }
            return .toolCall(name: payload.name, args: payload.args)
        case "tool_result":
            guard let payload = try? JSONDecoder().decode(ToolResultPayload.self, from: Data(data.utf8)) else {
                return nil
            }
            return .toolResult(name: payload.name, text: payload.text)
        case "usage":
            guard let payload = try? JSONDecoder().decode(UsagePayload.self, from: Data(data.utf8)) else {
                return nil
            }
            return .usage(
                prompt: payload.promptTokens,
                completion: payload.completionTokens,
                total: payload.totalTokens,
                costUSD: payload.costUSD
            )
        case "approval-needed":
            guard let payload = try? JSONDecoder().decode(ApprovalNeededPayload.self, from: Data(data.utf8)) else {
                return nil
            }
            return .approvalNeeded(approvalID: payload.approvalID, tool: payload.tool, args: payload.args)
        case "error":
            return .error(data)
        case "done":
            return .done
        default:
            // Unknown/default ("message") event names: skip leniently.
            return nil
        }
    }
}

// MARK: - JSON payload shapes (private to the parser)

private struct ToolCallPayload: Decodable {
    let name: String
    let args: String
}

private struct ToolResultPayload: Decodable {
    let name: String
    let text: String
}

/// Raw Go struct field names — no JSON tags on the server — so the keys are
/// capitalized exactly as shown in `docs/mobile-serve.md`.
private struct UsagePayload: Decodable {
    let promptTokens: Int
    let completionTokens: Int
    let totalTokens: Int
    let costUSD: Double

    enum CodingKeys: String, CodingKey {
        case promptTokens = "PromptTokens"
        case completionTokens = "CompletionTokens"
        case totalTokens = "TotalTokens"
        case costUSD = "CostUSD"
    }
}

private struct ApprovalNeededPayload: Decodable {
    let approvalID: String
    let tool: String
    let args: String

    enum CodingKeys: String, CodingKey {
        case approvalID = "approval_id"
        case tool
        case args
    }
}
