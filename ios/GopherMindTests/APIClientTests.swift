import XCTest
@testable import GopherMind

/// Exercises `APIClient.listModels()` and `createSession(model:)` against a
/// `URLProtocol` test double instead of a real server — deterministic and
/// offline, same intent as `RequestBuildingTests` but at the level that
/// actually encodes/decodes these two calls.
final class APIClientTests: XCTestCase {
    private let baseURL = URL(string: "http://192.168.1.50:8080")!
    private var urlSession: URLSession!

    override func setUp() {
        super.setUp()
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [MockURLProtocol.self]
        urlSession = URLSession(configuration: config)
    }

    override func tearDown() {
        MockURLProtocol.requestHandler = nil
        urlSession = nil
        super.tearDown()
    }

    private func makeClient() -> APIClient {
        APIClient(
            configuration: .init(baseURL: baseURL, bearerToken: "tok", hmacSecret: nil),
            urlSession: urlSession
        )
    }

    func testListModelsDecodesModelsArray() async throws {
        MockURLProtocol.requestHandler = { request in
            XCTAssertEqual(request.url?.path, "/models")
            XCTAssertEqual(request.httpMethod, "GET")
            let response = HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: nil)!
            return (response, Data(#"{"models":["gpt-4o","claude-x"]}"#.utf8))
        }

        let models = try await makeClient().listModels()

        XCTAssertEqual(models, ["gpt-4o", "claude-x"])
    }

    func testCreateSessionIncludesModelInRequestBody() async throws {
        var capturedBody: Data?
        MockURLProtocol.requestHandler = { request in
            capturedBody = request.capturedBodyData
            let response = HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: nil)!
            return (response, Data(#"{"id":"generated"}"#.utf8))
        }

        _ = try await makeClient().createSession(model: "gpt-4o")

        let body = try XCTUnwrap(capturedBody)
        let decoded = try XCTUnwrap(JSONSerialization.jsonObject(with: body) as? [String: String])
        XCTAssertEqual(decoded["model"], "gpt-4o")
        XCTAssertNil(decoded["id"])
    }

    func testCreateSessionWithNoIDOrModelSendsNoBody() async throws {
        var capturedBody: Data?
        var sawBody = false
        MockURLProtocol.requestHandler = { request in
            sawBody = true
            capturedBody = request.capturedBodyData
            let response = HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: nil)!
            return (response, Data(#"{"id":"generated"}"#.utf8))
        }

        _ = try await makeClient().createSession()

        XCTAssertTrue(sawBody)
        XCTAssertTrue(capturedBody?.isEmpty ?? true)
    }
}

/// Captures the outgoing request and returns a canned response, so
/// `APIClient` calls can be tested without a real network round trip.
private final class MockURLProtocol: URLProtocol {
    // Test-only global, set once at the start of each test and read from the
    // URL loading system's own thread; never mutated concurrently with a
    // read, so the isolation check is safe to opt out of here.
    nonisolated(unsafe) static var requestHandler: ((URLRequest) throws -> (HTTPURLResponse, Data))?

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        guard let handler = MockURLProtocol.requestHandler else {
            client?.urlProtocol(self, didFailWithError: URLError(.badURL))
            return
        }
        do {
            let (response, data) = try handler(request)
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: data)
            client?.urlProtocolDidFinishLoading(self)
        } catch {
            client?.urlProtocol(self, didFailWithError: error)
        }
    }

    override func stopLoading() {}
}

private extension URLRequest {
    /// `httpBody` on a request handed to a `URLProtocol` is sometimes moved
    /// into `httpBodyStream` by the loading system instead — read whichever
    /// is present so tests see the real bytes sent either way.
    var capturedBodyData: Data? {
        if let body = httpBody { return body }
        guard let stream = httpBodyStream else { return nil }
        stream.open()
        defer { stream.close() }
        var data = Data()
        let bufferSize = 4096
        var buffer = [UInt8](repeating: 0, count: bufferSize)
        while stream.hasBytesAvailable {
            let read = stream.read(&buffer, maxLength: bufferSize)
            if read > 0 {
                data.append(buffer, count: read)
            } else {
                break
            }
        }
        return data
    }
}
