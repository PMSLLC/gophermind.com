import XCTest
@testable import GopherMind

final class RequestBuildingTests: XCTestCase {
    private let baseURL = URL(string: "http://192.168.1.50:8080")!

    func testBearerHeaderIsAlwaysPresent() {
        let builder = RequestBuilder(baseURL: baseURL, bearerToken: "tok-123", hmacSecret: nil)
        let request = builder.build(path: "/session", method: "GET")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer tok-123")
    }

    func testNoHMACHeaderWhenSecretIsNil() {
        let builder = RequestBuilder(baseURL: baseURL, bearerToken: "tok", hmacSecret: nil)
        let request = builder.build(path: "/session", method: "GET")
        XCTAssertNil(request.value(forHTTPHeaderField: "X-Hub-Signature-256"))
    }

    func testNoHMACHeaderWhenSecretIsEmptyString() {
        let builder = RequestBuilder(baseURL: baseURL, bearerToken: "tok", hmacSecret: "")
        let request = builder.build(path: "/session", method: "GET")
        XCTAssertNil(request.value(forHTTPHeaderField: "X-Hub-Signature-256"))
    }

    func testHMACHeaderPresentAndCorrectWhenSecretSet() {
        let body = Data(#"{"id":"abc"}"#.utf8)
        let builder = RequestBuilder(baseURL: baseURL, bearerToken: "tok", hmacSecret: "shh")
        let request = builder.build(path: "/session", method: "POST", body: .json(body))

        let expected = "sha256=" + HMACSigner.hexDigest(body: body, secret: "shh")
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-Hub-Signature-256"), expected)
    }

    func testHMACSignsExactBodyBytesSentIncludingEmptyBody() {
        // GET/DELETE with no body: HMAC must still be computed, over an empty body.
        let builder = RequestBuilder(baseURL: baseURL, bearerToken: "tok", hmacSecret: "shh")
        let request = builder.build(path: "/session/abc", method: "DELETE")

        let expected = "sha256=" + HMACSigner.hexDigest(body: Data(), secret: "shh")
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-Hub-Signature-256"), expected)
    }

    func testJSONEndpointsSetContentTypeApplicationJSON() {
        let builder = RequestBuilder(baseURL: baseURL, bearerToken: "tok", hmacSecret: nil)
        let request = builder.build(path: "/devices", method: "POST", body: .json(Data(#"{"device_token":"x","platform":"ios"}"#.utf8)))
        XCTAssertEqual(request.value(forHTTPHeaderField: "Content-Type"), "application/json")
    }

    func testStreamRequestBodyIsRawTaskTextNotJSON() {
        let builder = RequestBuilder(baseURL: baseURL, bearerToken: "tok", hmacSecret: nil)
        let taskText = "list the files in this repo"
        let request = builder.build(path: "/session/abc/stream", method: "POST", body: .raw(Data(taskText.utf8)))

        XCTAssertEqual(request.httpBody, Data(taskText.utf8))
        XCTAssertEqual(String(data: request.httpBody!, encoding: .utf8), taskText)
        XCTAssertNil(request.value(forHTTPHeaderField: "Content-Type"))
    }

    func testStreamRequestBodyIsHMACSignedWhenSecretSet() {
        let taskText = "do the thing"
        let builder = RequestBuilder(baseURL: baseURL, bearerToken: "tok", hmacSecret: "shh")
        let request = builder.build(path: "/session/abc/stream", method: "POST", body: .raw(Data(taskText.utf8)))

        let expected = "sha256=" + HMACSigner.hexDigest(body: Data(taskText.utf8), secret: "shh")
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-Hub-Signature-256"), expected)
        // Still no Content-Type — raw text stream body must never be JSON.
        XCTAssertNil(request.value(forHTTPHeaderField: "Content-Type"))
    }

    func testGETRequestHasNoBody() {
        let builder = RequestBuilder(baseURL: baseURL, bearerToken: "tok", hmacSecret: nil)
        let request = builder.build(path: "/session", method: "GET")
        XCTAssertNil(request.httpBody)
    }

    func testMethodAndPathAreApplied() {
        let builder = RequestBuilder(baseURL: baseURL, bearerToken: "tok", hmacSecret: nil)
        let request = builder.build(path: "/session/xyz", method: "DELETE")
        XCTAssertEqual(request.httpMethod, "DELETE")
        XCTAssertEqual(request.url?.absoluteString, "http://192.168.1.50:8080/session/xyz")
    }
}
