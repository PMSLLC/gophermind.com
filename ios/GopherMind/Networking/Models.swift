import Foundation

/// `GET /session` list entry. Raw Go struct field names — no JSON tags on the
/// server — so the keys are capitalized exactly as shown in
/// `docs/mobile-serve.md` (`session.Info`).
struct SessionInfo: Decodable, Equatable, Identifiable {
    let id: String
    let path: String
    let size: Int
    let modTime: Date
    let messages: Int
    let title: String

    enum CodingKeys: String, CodingKey {
        case id = "ID"
        case path = "Path"
        case size = "Size"
        case modTime = "ModTime"
        case messages = "Messages"
        case title = "Title"
    }
}

/// One line of `GET /session/{id}/messages` — the agent's stored
/// conversation, in the OpenAI chat-completion message shape it already
/// persists as JSONL (`internal/agent.ExportJSONL`). JSON keys are
/// lowercase/snake_case (unlike `SessionInfo`, which mirrors a Go struct with
/// no JSON tags).
struct StoredMessage: Decodable {
    let role: String
    let content: String?
    let toolCalls: [ToolCall]?
    let toolCallID: String?

    enum CodingKeys: String, CodingKey {
        case role
        case content
        case toolCalls = "tool_calls"
        case toolCallID = "tool_call_id"
    }

    struct ToolCall: Decodable {
        let id: String?
        let function: Function
    }

    struct Function: Decodable {
        let name: String
        let arguments: String
    }
}
