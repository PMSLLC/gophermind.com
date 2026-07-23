import Foundation

/// Server operations `SessionListViewModel` depends on. `GopherMindService`
/// already implements all three (see `Networking/GopherMindService.swift`);
/// this narrower protocol just lets tests inject a fake without touching the
/// network, mirroring `GopherMindServicing` for the conversation side.
@MainActor
protocol SessionListServicing {
    func listSessions() async throws -> [SessionInfo]
    func deleteSession(_ id: String) async throws
    func renameSession(_ id: String, name: String) async throws
}

extension GopherMindService: SessionListServicing {}

/// Drives the session list screen: loads `[SessionInfo]` newest-first and
/// handles swipe-to-delete. No streaming, no transcript — see
/// `SessionViewModel` for that.
@MainActor
final class SessionListViewModel: ObservableObject {
    @Published private(set) var sessions: [SessionInfo] = []
    @Published private(set) var isLoading = false
    @Published var errorMessage: String?

    private let service: SessionListServicing

    init(service: SessionListServicing) {
        self.service = service
    }

    func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            let list = try await service.listSessions()
            sessions = list.sorted { $0.modTime > $1.modTime }
        } catch {
            errorMessage = "Couldn't load sessions: \(error.localizedDescription)"
        }
    }

    func delete(_ id: String) async {
        let previous = sessions
        sessions.removeAll { $0.id == id }
        do {
            try await service.deleteSession(id)
        } catch {
            sessions = previous
            errorMessage = "Couldn't delete session: \(error.localizedDescription)"
        }
    }

    /// Renames a session, updating the row optimistically and rolling back if
    /// the server rejects it. A blank name clears the custom name server-side.
    func rename(_ id: String, to name: String) async {
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        let previous = sessions
        if let i = sessions.firstIndex(where: { $0.id == id }) {
            let s = sessions[i]
            sessions[i] = SessionInfo(id: s.id, path: s.path, size: s.size,
                                      modTime: s.modTime, messages: s.messages,
                                      title: s.title, name: trimmed)
        }
        do {
            try await service.renameSession(id, name: trimmed)
        } catch {
            sessions = previous
            errorMessage = "Couldn't rename session: \(error.localizedDescription)"
        }
    }
}
