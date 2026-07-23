import Foundation

/// Server operations `SessionViewModel` depends on. `GopherMindService` is
/// the live implementation; tests inject a fake conforming to this to
/// capture `approve` calls without touching the network (see
/// `GopherMindTests/ApprovalTests.swift`).
@MainActor
protocol GopherMindServicing {
    func createSession(id: String?, model: String?, mode: String?) async throws -> String
    func listModels() async throws -> [String]
    func getModes() async throws -> [Mode]
    func getSessionConfig(sessionID: String) async throws -> SessionConfig
    func approve(sessionID: String, approvalID: String, approved: Bool) async throws
    func stream(sessionID: String, task: String) throws -> AsyncThrowingStream<AgentEvent, Error>
    func getMessages(sessionID: String) async throws -> [StoredMessage]
}

/// Thin adapter between `AppSettings` and `APIClient`. The UI (A3) calls
/// through this rather than constructing an `APIClient` directly, so it
/// always uses whatever server URL/credentials are currently in settings.
@MainActor
final class GopherMindService: GopherMindServicing {
    enum ServiceError: LocalizedError {
        case invalidServerURL

        var errorDescription: String? {
            switch self {
            case .invalidServerURL:
                return "No valid Server URL. Open Settings and enter your server (with http:// or https://), e.g. http://10.0.0.5:8090"
            }
        }
    }

    private let settings: AppSettings

    init(settings: AppSettings) {
        self.settings = settings
    }

    private func makeClient() throws -> APIClient {
        // Trim pasted whitespace/newlines and require a real scheme+host —
        // URL(string:) returns nil on a stray space, which had surfaced as the
        // opaque "ServiceError error 0".
        let raw = settings.serverURL.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !raw.isEmpty, let url = URL(string: raw),
              let scheme = url.scheme?.lowercased(), scheme == "http" || scheme == "https",
              url.host?.isEmpty == false else {
            throw ServiceError.invalidServerURL
        }
        let configuration = APIClient.Configuration(
            baseURL: url,
            bearerToken: settings.bearerToken,
            hmacSecret: settings.hmacSecret.isEmpty ? nil : settings.hmacSecret
        )
        return APIClient(configuration: configuration)
    }

    func createSession(id: String? = nil, model: String? = nil, mode: String? = nil) async throws -> String {
        try await makeClient().createSession(id: id, model: model, mode: mode)
    }

    func listModels() async throws -> [String] {
        try await makeClient().listModels()
    }

    func getModes() async throws -> [Mode] {
        try await makeClient().getModes()
    }

    func getSessionConfig(sessionID: String) async throws -> SessionConfig {
        try await makeClient().getSessionConfig(sessionID: sessionID)
    }

    func listSessions() async throws -> [SessionInfo] {
        try await makeClient().listSessions()
    }

    func deleteSession(_ id: String) async throws {
        try await makeClient().deleteSession(id)
    }

    func renameSession(_ id: String, name: String) async throws {
        try await makeClient().renameSession(id, name: name)
    }

    func registerDevice(token: String) async throws {
        try await makeClient().registerDevice(token: token)
    }

    func approve(sessionID: String, approvalID: String, approved: Bool) async throws {
        try await makeClient().approve(sessionID: sessionID, approvalID: approvalID, approved: approved)
    }

    func stream(sessionID: String, task: String) throws -> AsyncThrowingStream<AgentEvent, Error> {
        try makeClient().stream(sessionID: sessionID, task: task)
    }

    func getMessages(sessionID: String) async throws -> [StoredMessage] {
        try await makeClient().getMessages(sessionID: sessionID)
    }
}
