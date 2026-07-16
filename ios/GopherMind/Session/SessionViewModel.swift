import Foundation

/// Drives one conversation: owns the transcript, the current session id, and
/// the in-flight stream for a turn.
@MainActor
final class SessionViewModel: ObservableObject {
    @Published private(set) var items: [ConversationItem] = []
    @Published var inputText: String = ""
    @Published private(set) var isStreaming: Bool = false

    private let service: GopherMindServicing
    private(set) var sessionID: String?

    /// `items` lets tests seed a transcript (e.g. a pending approval) without
    /// a mutable public setter; production call sites rely on the `[]` default.
    init(service: GopherMindServicing, sessionID: String? = nil, items: [ConversationItem] = []) {
        self.service = service
        self.sessionID = sessionID
        self.items = items
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
        let id = try await service.createSession(id: nil)
        sessionID = id
        return id
    }

    /// Records the user's Approve/Deny choice for a still-pending approval,
    /// then tells the server so it can unblock the paused (still-streaming)
    /// turn. The item flips to `.approvalDecided` synchronously/optimistically;
    /// the network call runs in an unstructured `Task` and, on failure,
    /// surfaces an error line rather than reverting the decision or crashing.
    ///
    /// No-ops if `approvalID` doesn't match a currently-pending item (covers
    /// double-decide: once decided, the item is no longer `.approvalPending`).
    /// Returns the in-flight `Task` so tests can await it; `nil` when nothing
    /// was decided.
    @discardableResult
    func decide(approvalID: String, approved: Bool) -> Task<Void, Never>? {
        guard let index = items.firstIndex(where: {
            if case .approvalPending(let id, _, _) = $0.kind { return id == approvalID }
            return false
        }) else { return nil }
        guard case .approvalPending(_, let tool, let args) = items[index].kind else { return nil }

        items[index].kind = .approvalDecided(approvalID: approvalID, tool: tool, args: args, approved: approved)

        guard let sid = sessionID else {
            items = Self.reduce(items, .error("No active session for approval"))
            return nil
        }

        return Task {
            do {
                try await self.service.approve(sessionID: sid, approvalID: approvalID, approved: approved)
            } catch {
                self.items = Self.reduce(self.items, .error("Failed to send approval: \(error.localizedDescription)"))
            }
        }
    }

    /// Switches to the session named by a push deep-link (A5) and, if not
    /// already present, appends a `.approvalPending` row for its approval so
    /// `ApprovalCard` renders Approve/Deny immediately. The live SSE frame
    /// that would normally add this row isn't available here — streaming
    /// doesn't run while the app is backgrounded, which is exactly when this
    /// path fires.
    func openApprovalRoute(_ route: ApprovalRoute) {
        if sessionID != route.sessionID {
            sessionID = route.sessionID
            items = []
        }
        let alreadyPresent = items.contains {
            if case .approvalPending(let id, _, _) = $0.kind { return id == route.approvalID }
            return false
        }
        guard !alreadyPresent else { return }
        items.append(ConversationItem(kind: .approvalPending(approvalID: route.approvalID, tool: route.tool ?? "tool", args: "")))
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
            items = expirePendingApprovals(items)

        case .error(let text):
            items.append(ConversationItem(kind: .errorLine(text)))
            items = expirePendingApprovals(items)
        }

        return items
    }

    /// A turn ending (`.done` or `.error`) leaves no one to answer a still-open
    /// approval — the server auto-denies on timeout — so any `.approvalPending`
    /// rows become non-interactive `.approvalExpired` rows.
    private nonisolated static func expirePendingApprovals(_ items: [ConversationItem]) -> [ConversationItem] {
        var items = items
        for index in items.indices {
            if case .approvalPending(let id, let tool, let args) = items[index].kind {
                items[index].kind = .approvalExpired(approvalID: id, tool: tool, args: args)
            }
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
