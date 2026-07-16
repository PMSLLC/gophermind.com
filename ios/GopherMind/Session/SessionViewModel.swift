import Foundation

/// Drives one conversation: owns the transcript, the current session id, and
/// the in-flight stream for a turn.
@MainActor
final class SessionViewModel: ObservableObject {
    @Published private(set) var items: [ConversationItem] = []
    @Published var inputText: String = ""
    @Published private(set) var isStreaming: Bool = false

    private let service: GopherMindService
    private(set) var sessionID: String?

    init(service: GopherMindService, sessionID: String? = nil) {
        self.service = service
        self.sessionID = sessionID
    }

    /// Appends a `.user` item, then streams the turn, folding each
    /// `AgentEvent` into `items` via `reduce` until the stream ends.
    func send(_ task: String) async {
        let trimmed = task.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, !isStreaming else { return }

        items.append(ConversationItem(kind: .user(trimmed)))
        isStreaming = true
        defer { isStreaming = false }

        do {
            let sid = try await resolvedSessionID()
            let stream = try service.stream(sessionID: sid, task: trimmed)
            for try await event in stream {
                items = Self.reduce(items, event)
            }
        } catch {
            items = Self.reduce(items, .error("Connection failed: \(error.localizedDescription)"))
        }
    }

    private func resolvedSessionID() async throws -> String {
        if let sessionID {
            return sessionID
        }
        let id = try await service.createSession()
        sessionID = id
        return id
    }

    // MARK: - Pure reducer

    /// Folds one `AgentEvent` into the current transcript. No side effects,
    /// no actor isolation needed — this is the unit-tested core (see
    /// `ReducerTests`).
    nonisolated static func reduce(_ items: [ConversationItem], _ event: AgentEvent) -> [ConversationItem] {
        var items = items

        switch event {
        case .token(let text), .assistant(let text):
            if case .assistant(let existing) = items.last?.kind {
                items[items.count - 1].kind = .assistant(existing + text)
            } else {
                items.append(ConversationItem(kind: .assistant(text)))
            }

        case .toolCall(let name, let args):
            items.append(ConversationItem(kind: .tool(name: name, args: args, result: nil)))

        case .toolResult(let name, let text):
            if let index = pendingToolIndex(in: items, matchingName: name) ?? pendingToolIndex(in: items, matchingName: nil) {
                if case .tool(let n, let a, _) = items[index].kind {
                    items[index].kind = .tool(name: n, args: a, result: text)
                }
            }

        case .usage(let prompt, let completion, let total, let costUSD):
            if let index = items.lastIndex(where: { if case .usage = $0.kind { return true } else { return false } }) {
                items[index].kind = .usage(prompt: prompt, completion: completion, total: total, costUSD: costUSD)
            } else {
                items.append(ConversationItem(kind: .usage(prompt: prompt, completion: completion, total: total, costUSD: costUSD)))
            }

        case .approvalNeeded(let approvalID, let tool, let args):
            items.append(ConversationItem(kind: .approvalPending(approvalID: approvalID, tool: tool, args: args)))

        case .done:
            break

        case .error(let text):
            items.append(ConversationItem(kind: .errorLine(text)))
        }

        return items
    }

    /// Finds the most recent `.tool` row still awaiting a result. When
    /// `matchingName` is provided, only rows for that tool name match;
    /// pass `nil` to fall back to the most recent pending row regardless
    /// of name.
    private nonisolated static func pendingToolIndex(in items: [ConversationItem], matchingName name: String?) -> Int? {
        items.lastIndex(where: {
            guard case .tool(let n, _, let result) = $0.kind, result == nil else { return false }
            guard let name else { return true }
            return n == name
        })
    }
}
