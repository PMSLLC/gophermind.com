import XCTest
@testable import GopherMind

final class PairingConfigTests: XCTestCase {
    func testRoundTripThroughLink() throws {
        let cfg = PairingConfig(serverURL: "http://192.0.2.10:8090",
                                bearerToken: "test-bearer-token-xyz",
                                hmacSecret: nil)
        let parsed = try XCTUnwrap(PairingConfig.parse(cfg.link))
        XCTAssertEqual(parsed.serverURL, cfg.serverURL)
        XCTAssertEqual(parsed.bearerToken, cfg.bearerToken)
        XCTAssertNil(parsed.hmacSecret)
    }

    func testRoundTripWithHMAC() throws {
        let cfg = PairingConfig(serverURL: "https://box:8443", bearerToken: "tok", hmacSecret: "sek")
        let parsed = try XCTUnwrap(PairingConfig.parse(cfg.link))
        XCTAssertEqual(parsed, cfg)
    }

    func testParsesBareBlobAndTrimsWhitespace() throws {
        let cfg = PairingConfig(serverURL: "http://h:1", bearerToken: "t", hmacSecret: nil)
        let blob = cfg.link.replacingOccurrences(of: "gophermind://setup?c=", with: "")
        XCTAssertEqual(PairingConfig.parse("  \n\(blob)\n ")?.serverURL, "http://h:1")
    }

    func testRejectsGarbage() {
        XCTAssertNil(PairingConfig.parse(""))
        XCTAssertNil(PairingConfig.parse("not a link"))
        XCTAssertNil(PairingConfig.parse("https://example.com"))
        XCTAssertNil(PairingConfig.parse("gophermind://setup?c=%%%notbase64"))
    }
}
