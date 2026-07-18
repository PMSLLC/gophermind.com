import XCTest
@testable import GopherMind

/// Regression test for the ServiceError-error-0 bug: Go's time.Time marshals
/// RFC3339 with up to nanosecond fractional seconds, which the stock .iso8601
/// strategy cannot parse. The app's decoder must tolerate all these shapes.
final class DateDecodingTests: XCTestCase {
    private func decode(_ json: String) throws -> [SessionInfo] {
        try APIClient.decoder.decode([SessionInfo].self, from: Data(json.utf8))
    }

    func testNanosecondModTimeDecodes() throws {
        // The exact shape the server produced that broke the app.
        let json = """
        [{"ID":"AudioBooks","Path":"/x/AudioBooks.jsonl","Size":1734,\
        "ModTime":"2026-07-17T23:49:34.717505559Z","Messages":3,"Title":"t"}]
        """
        let sessions = try decode(json)
        XCTAssertEqual(sessions.count, 1)
        XCTAssertEqual(sessions[0].id, "AudioBooks")
        // Must be a real date, not the 1970 fallback.
        XCTAssertGreaterThan(sessions[0].modTime.timeIntervalSince1970, 1_700_000_000)
    }

    func testWholeSecondAndMillisecondModTimesDecode() throws {
        let json = """
        [{"ID":"a","Path":"p","Size":1,"ModTime":"2026-07-17T23:49:34Z","Messages":0,"Title":""},
         {"ID":"b","Path":"p","Size":1,"ModTime":"2026-07-17T23:49:34.717Z","Messages":0,"Title":""}]
        """
        let sessions = try decode(json)
        XCTAssertEqual(sessions.count, 2)
        for s in sessions {
            XCTAssertGreaterThan(s.modTime.timeIntervalSince1970, 1_700_000_000)
        }
    }

    func testEmptyListStillDecodes() throws {
        XCTAssertEqual(try decode("[]").count, 0)
    }
}
