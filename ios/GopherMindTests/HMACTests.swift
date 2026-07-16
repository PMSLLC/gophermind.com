import XCTest
@testable import GopherMind

/// Known body+secret pairs with expected lowercase-hex HMAC-SHA256 digests,
/// computed independently (Python `hmac`/`hashlib`, not the Swift code under
/// test):
///
///   python3 -c "import hmac,hashlib; print(hmac.new(b'secret123', b'hello world', hashlib.sha256).hexdigest())"
final class HMACTests: XCTestCase {
    func testKnownBodyAndSecretProducesKnownDigest() {
        let digest = HMACSigner.hexDigest(body: Data("hello world".utf8), secret: "secret123")
        XCTAssertEqual(digest, "57938295649097379cddb382dd6c82d5e0460645a8fd01674a48a76de6142646")
    }

    func testKnownJSONBodyProducesKnownDigest() {
        let body = #"{"approval_id":"a1","approved":true}"#
        let digest = HMACSigner.hexDigest(body: Data(body.utf8), secret: "top-secret-hmac-key")
        XCTAssertEqual(digest, "f413aa90aeac3448e4ca3b28775bfc1bc4cd390e3742dec5c015669eeed4827b")
    }

    func testEmptyBodyProducesKnownDigest() {
        let digest = HMACSigner.hexDigest(body: Data(), secret: "another-secret")
        XCTAssertEqual(digest, "d860e29f7346b0bc6ac0f8f3308c923ce7899e3993350440043fd48f42d48e61")
    }

    func testDigestIsLowercaseHex() {
        let digest = HMACSigner.hexDigest(body: Data("x".utf8), secret: "y")
        XCTAssertEqual(digest, digest.lowercased())
        XCTAssertTrue(digest.allSatisfy { $0.isHexDigit })
        XCTAssertEqual(digest.count, 64) // SHA-256 -> 32 bytes -> 64 hex chars
    }
}
