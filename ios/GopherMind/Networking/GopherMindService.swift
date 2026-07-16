import Foundation

/// Thin adapter between `AppSettings` and `APIClient`. The UI (A3) calls
/// through this rather than constructing an `APIClient` directly, so it
/// always uses whatever server URL/credentials are currently in settings.
@MainActor
final class GopherMindService {
    enum ServiceError: Error {
        case invalidServerURL
    }

    private let settings: AppSettings

    init(settings: AppSettings) {
        self.settings = settings
    }

    private func makeClient() throws -> APIClient {
        guard !settings.serverURL.isEmpty, let url = URL(string: settings.serverURL) else {
            throw ServiceError.invalidServerURL
        }
        let configuration = APIClient.Configuration(
            baseURL: url,
            bearerToken: settings.bearerToken,
            hmacSecret: settings.hmacSecret.isEmpty ? nil : settings.hmacSecret
        )
        return APIClient(configuration: configuration)
    }

    func createSession(id: String? = nil) async throws -> String {
        try await makeClient().createSession(id: id)
    }

    func listSessions() async throws -> [SessionInfo] {
        try await makeClient().listSessions()
    }

    func deleteSession(_ id: String) async throws {
        try await makeClient().deleteSession(id)
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
}
