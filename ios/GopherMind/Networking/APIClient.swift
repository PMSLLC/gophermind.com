import Foundation

/// Client for the `gophermind serve` HTTP + SSE contract in
/// `docs/mobile-serve.md`. Owns request building, response validation, and
/// SSE decoding; no UI dependency.
actor APIClient {
    struct Configuration: Sendable {
        var baseURL: URL
        var bearerToken: String
        var hmacSecret: String?
    }

    enum APIError: Error, Equatable {
        case invalidResponse
        case badRequest(String?)
        case unauthorized
        case notFound
        case conflict
        case rateLimited
        case server(status: Int)
    }

    private let configuration: Configuration
    private let urlSession: URLSession

    private var requestBuilder: RequestBuilder {
        RequestBuilder(baseURL: configuration.baseURL, bearerToken: configuration.bearerToken, hmacSecret: configuration.hmacSecret)
    }

    private static let encoder = JSONEncoder()
    private static let decoder: JSONDecoder = {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return decoder
    }()

    init(configuration: Configuration, urlSession: URLSession = .shared) {
        self.configuration = configuration
        self.urlSession = urlSession
    }

    // MARK: - Session lifecycle

    /// `POST /session` → `{"id": "<id>"}`. Pass `id` to choose one (must
    /// match `^[A-Za-z0-9._-]+$` server-side); omit to get a generated id.
    func createSession(id: String? = nil) async throws -> String {
        struct CreateRequest: Encodable { let id: String }
        struct CreateResponse: Decodable { let id: String }

        let body: RequestBuilder.Body
        if let id {
            body = .json(try Self.encoder.encode(CreateRequest(id: id)))
        } else {
            body = .none
        }
        let data = try await send(path: "/session", method: "POST", body: body)
        return try Self.decoder.decode(CreateResponse.self, from: data).id
    }

    /// `GET /session` → array of saved sessions (never null; `[]` when empty).
    func listSessions() async throws -> [SessionInfo] {
        let data = try await send(path: "/session", method: "GET")
        return try Self.decoder.decode([SessionInfo].self, from: data)
    }

    /// `DELETE /session/{id}` → `204` on success.
    func deleteSession(_ id: String) async throws {
        _ = try await send(path: "/session/\(id)", method: "DELETE")
    }

    // MARK: - Devices / approvals

    /// `POST /devices` → registers an APNs device token for push.
    func registerDevice(token: String) async throws {
        struct DeviceRequest: Encodable {
            let device_token: String
            let platform: String
        }
        let body = try Self.encoder.encode(DeviceRequest(device_token: token, platform: "ios"))
        _ = try await send(path: "/devices", method: "POST", body: .json(body))
    }

    /// `POST /session/{id}/approve` → resolves a pending `approval-needed`.
    func approve(sessionID: String, approvalID: String, approved: Bool) async throws {
        struct ApproveRequest: Encodable {
            let approval_id: String
            let approved: Bool
        }
        let body = try Self.encoder.encode(ApproveRequest(approval_id: approvalID, approved: approved))
        _ = try await send(path: "/session/\(sessionID)/approve", method: "POST", body: .json(body))
    }

    // MARK: - Streaming

    /// `POST /session/{id}/stream` with the **raw task text** as the body
    /// (not JSON) — decodes the SSE response into `AgentEvent`s as they
    /// arrive. Finishes normally after yielding `.done`.
    nonisolated func stream(sessionID: String, task: String) -> AsyncThrowingStream<AgentEvent, Error> {
        AsyncThrowingStream { continuation in
            let streamingTask = Task {
                do {
                    let request = await self.buildStreamRequest(sessionID: sessionID, task: task)
                    let (bytes, response) = try await self.urlSession.bytes(for: request)

                    guard let http = response as? HTTPURLResponse else {
                        throw APIError.invalidResponse
                    }
                    guard http.statusCode == 200 else {
                        throw Self.error(forStatus: http.statusCode, body: nil)
                    }

                    var parser = SSEParser()
                    for try await line in bytes.lines {
                        try Task.checkCancellation()
                        guard let event = parser.feed(line: line) else { continue }
                        continuation.yield(event)
                        if case .done = event {
                            break
                        }
                    }
                    continuation.finish()
                } catch {
                    continuation.finish(throwing: error)
                }
            }
            continuation.onTermination = { _ in streamingTask.cancel() }
        }
    }

    private func buildStreamRequest(sessionID: String, task: String) -> URLRequest {
        requestBuilder.build(path: "/session/\(sessionID)/stream", method: "POST", body: .raw(Data(task.utf8)))
    }

    // MARK: - Plumbing

    private func send(path: String, method: String, body: RequestBuilder.Body = .none) async throws -> Data {
        let request = requestBuilder.build(path: path, method: method, body: body)
        let (data, response) = try await urlSession.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw APIError.invalidResponse
        }
        guard (200..<300).contains(http.statusCode) else {
            throw Self.error(forStatus: http.statusCode, body: data)
        }
        return data
    }

    private static func error(forStatus status: Int, body: Data?) -> APIError {
        switch status {
        case 400:
            return .badRequest(body.flatMap { String(data: $0, encoding: .utf8) })
        case 401:
            return .unauthorized
        case 404:
            return .notFound
        case 409:
            return .conflict
        case 429:
            return .rateLimited
        default:
            return .server(status: status)
        }
    }
}
