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
    // Exposed so the connection-debug screen can decode with the exact same
    // strategy the app uses in production (so its "Decode" check is faithful).
    static let decoder: JSONDecoder = {
        let decoder = JSONDecoder()
        // Go's time.Time marshals RFC3339 with variable fractional seconds up to
        // nanosecond precision (e.g. "2026-07-17T23:49:34.717505559Z"), which the
        // stock .iso8601 strategy CANNOT parse — it would throw and fail the whole
        // response. Parse leniently: try fractional, then plain, then strip the
        // sub-second fraction and retry. Never fail decode over a display date.
        decoder.dateDecodingStrategy = .custom { dec in
            let raw = try dec.singleValueContainer().decode(String.self)
            let frac = ISO8601DateFormatter()
            frac.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            if let d = frac.date(from: raw) { return d }
            let plain = ISO8601DateFormatter()
            plain.formatOptions = [.withInternetDateTime]
            if let d = plain.date(from: raw) { return d }
            if let r = raw.range(of: #"\.\d+"#, options: .regularExpression) {
                var s = raw; s.removeSubrange(r)
                if let d = plain.date(from: s) { return d }
            }
            return Date(timeIntervalSince1970: 0)
        }
        return decoder
    }()

    init(configuration: Configuration, urlSession: URLSession = .shared) {
        self.configuration = configuration
        self.urlSession = urlSession
    }

    // MARK: - Session lifecycle

    /// `POST /session` → `{"id": "<id>"}`. Pass `id` to choose one (must
    /// match `^[A-Za-z0-9._-]+$` server-side); omit to get a generated id.
    /// Pass `model` to pin the session to a specific model (stored
    /// server-side in a sidecar file next to the session); omit to use the
    /// server's default model. Pass `mode` to pin the session to a specific
    /// mode (also a sidecar file), which determines the system prompt a
    /// brand-new session starts with; omit to use the default coding prompt.
    func createSession(id: String? = nil, model: String? = nil, mode: String? = nil) async throws -> String {
        struct CreateRequest: Encodable { let id: String?; let model: String?; let mode: String? }
        struct CreateResponse: Decodable { let id: String }

        let body: RequestBuilder.Body
        if id != nil || model != nil || mode != nil {
            body = .json(try Self.encoder.encode(CreateRequest(id: id, model: model, mode: mode)))
        } else {
            body = .none
        }
        let data = try await send(path: "/session", method: "POST", body: body)
        return try Self.decoder.decode(CreateResponse.self, from: data).id
    }

    /// `GET /models` → `{"models": [...]}`, the endpoint's available models.
    func listModels() async throws -> [String] {
        struct ModelsResponse: Decodable { let models: [String] }
        let data = try await send(path: "/models", method: "GET")
        return try Self.decoder.decode(ModelsResponse.self, from: data).models
    }

    /// `GET /modes` → `{"modes": [...]}`, the fixed set of session modes the
    /// app can offer in its mode picker.
    func getModes() async throws -> [Mode] {
        struct ModesResponse: Decodable { let modes: [Mode] }
        let data = try await send(path: "/modes", method: "GET")
        return try Self.decoder.decode(ModesResponse.self, from: data).modes
    }

    /// `GET /session/{id}/config` → the session's stored model and mode, so
    /// the app can show what it's actually running with.
    func getSessionConfig(sessionID: String) async throws -> SessionConfig {
        let data = try await send(path: "/session/\(sessionID)/config", method: "GET")
        return try Self.decoder.decode(SessionConfig.self, from: data)
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

    /// `PATCH /session/{id}` with `{"name":"..."}` → `204`. An empty name
    /// clears the custom name, reverting to the server-derived title.
    func renameSession(_ id: String, name: String) async throws {
        struct RenameRequest: Encodable { let name: String }
        let body = try Self.encoder.encode(RenameRequest(name: name))
        _ = try await send(path: "/session/\(id)", method: "PATCH", body: .json(body))
    }

    /// `GET /session/{id}/messages` → the session's stored conversation, one
    /// `StoredMessage` per persisted JSONL line (system/user/assistant/tool).
    func getMessages(sessionID: String) async throws -> [StoredMessage] {
        let data = try await send(path: "/session/\(sessionID)/messages", method: "GET")
        return try Self.decoder.decode([StoredMessage].self, from: data)
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

                    // NOTE: do NOT use `bytes.lines` — URLSession.AsyncBytes.lines
                    // collapses empty lines, but SSE frames are terminated by a
                    // BLANK line, so the parser would never see a frame boundary
                    // and no event would ever be yielded. Split the raw byte stream
                    // ourselves, preserving empty lines.
                    var parser = SSEParser()
                    var buffer: [UInt8] = []
                    func flushLine() -> Bool {
                        var line = String(decoding: buffer, as: UTF8.self)
                        buffer.removeAll(keepingCapacity: true)
                        if line.hasSuffix("\r") { line.removeLast() }   // tolerate CRLF
                        guard let event = parser.feed(line: line) else { return false }
                        continuation.yield(event)
                        return { if case .done = event { return true } else { return false } }()
                    }
                    for try await byte in bytes {
                        try Task.checkCancellation()
                        if byte == 0x0A {            // \n → complete a line (may be empty)
                            if flushLine() { break }
                        } else {
                            buffer.append(byte)
                        }
                    }
                    _ = flushLine()                  // final line without a trailing newline
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
