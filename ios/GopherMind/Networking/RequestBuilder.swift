import Foundation

/// Pure request-building helper for the `gophermind serve` contract in
/// `docs/mobile-serve.md`. No networking — just turns (path, method, body)
/// into a fully-headered `URLRequest`, so it's unit-testable on its own.
///
/// Sets on every request:
/// - `Authorization: Bearer <token>`
/// - `Content-Type: application/json` for JSON bodies only (never for the
///   raw-text stream body).
/// - `X-Hub-Signature-256: sha256=<hex>` when an HMAC secret is configured —
///   HMAC-SHA256 of the exact raw body bytes sent (empty body if none).
struct RequestBuilder {
    /// The body to attach to the request.
    enum Body {
        /// No body at all (e.g. `GET`/`DELETE`, or `POST /session` with no id).
        case none
        /// A JSON-encoded body. Sets `Content-Type: application/json`.
        case json(Data)
        /// A raw, non-JSON body (the `/session/{id}/stream` task text).
        case raw(Data)
    }

    let baseURL: URL
    let bearerToken: String
    let hmacSecret: String?

    func build(path: String, method: String, body: Body = .none) -> URLRequest {
        var request = URLRequest(url: baseURL.appendingPathComponent(path))
        request.httpMethod = method
        request.setValue("Bearer \(bearerToken)", forHTTPHeaderField: "Authorization")

        let bodyData: Data
        switch body {
        case .none:
            bodyData = Data()
        case .json(let data):
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            bodyData = data
        case .raw(let data):
            bodyData = data
        }

        if case .none = body {
            // Leave httpBody nil for bodyless requests (GET/DELETE).
        } else {
            request.httpBody = bodyData
        }

        if let hmacSecret, !hmacSecret.isEmpty {
            let digest = HMACSigner.hexDigest(body: bodyData, secret: hmacSecret)
            request.setValue("sha256=\(digest)", forHTTPHeaderField: "X-Hub-Signature-256")
        }

        return request
    }
}
