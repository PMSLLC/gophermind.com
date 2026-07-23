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
    /// Custom display name set via rename; empty when unset. Old servers omit
    /// the field, so it decodes as "".
    let name: String

    enum CodingKeys: String, CodingKey {
        case id = "ID"
        case path = "Path"
        case size = "Size"
        case modTime = "ModTime"
        case messages = "Messages"
        case title = "Title"
        case name = "Name"
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        path = try c.decode(String.self, forKey: .path)
        size = try c.decode(Int.self, forKey: .size)
        modTime = try c.decode(Date.self, forKey: .modTime)
        messages = try c.decode(Int.self, forKey: .messages)
        title = try c.decode(String.self, forKey: .title)
        name = (try? c.decode(String.self, forKey: .name)) ?? ""
    }

    /// Memberwise init for tests and local construction.
    init(id: String, path: String, size: Int, modTime: Date, messages: Int, title: String, name: String = "") {
        self.id = id; self.path = path; self.size = size
        self.modTime = modTime; self.messages = messages; self.title = title; self.name = name
    }

    /// The label to show: the custom name if set, otherwise the session id.
    var displayName: String {
        name.isEmpty ? id : name
    }
}

/// One entry of `GET /modes` — a session mode the app can offer in its mode
/// picker (`{"id":"conversational","label":"Conversational"}`).
struct Mode: Decodable, Equatable, Identifiable {
    let id: String
    let label: String
}

/// `GET /session/{id}/config` — a session's stored model and mode, so the
/// app can display what it's actually running with. Empty strings mean
/// "unset" (server default model / coding mode).
struct SessionConfig: Decodable, Equatable {
    let model: String
    let mode: String
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
