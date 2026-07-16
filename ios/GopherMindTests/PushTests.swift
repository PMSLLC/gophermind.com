import XCTest
@testable import GopherMind

/// Exercises the pure APNs helpers (A5) — device-token hex formatting and
/// push-payload deep-link extraction. Deterministic and offline: no
/// UNUserNotificationCenter, no real device token, no network.
final class PushTests: XCTestCase {
    func testDeviceTokenHexFormatsAsLowercaseHex() {
        let data = Data([0x01, 0xab, 0xff, 0x00, 0x7e])
        XCTAssertEqual(deviceTokenHex(data), "01abff007e")
    }

    func testDeviceTokenHexOfEmptyDataIsEmptyString() {
        XCTAssertEqual(deviceTokenHex(Data()), "")
    }

    func testApprovalRouteExtractsFieldsFromRepresentativeUserInfo() {
        // Shape sent by cmd/gophermind/apns.go's newApprovalNotifier: aps +
        // session_id/approval_id merged at the top level. `tool` isn't
        // currently sent server-side but is read opportunistically.
        let userInfo: [AnyHashable: Any] = [
            "aps": [
                "alert": ["title": "Approval needed", "body": "gophermind wants to run shell"],
                "sound": "default",
            ],
            "session_id": "sess-1",
            "approval_id": "appr-1",
            "tool": "shell",
        ]

        let route = approvalRoute(from: userInfo)

        XCTAssertEqual(route, ApprovalRoute(sessionID: "sess-1", approvalID: "appr-1", tool: "shell"))
    }

    func testApprovalRouteToolIsOptionalWhenAbsent() {
        // Matches the actual current server payload, which omits `tool`.
        let userInfo: [AnyHashable: Any] = [
            "aps": ["alert": ["title": "Approval needed", "body": "gophermind wants to run shell"]],
            "session_id": "sess-1",
            "approval_id": "appr-1",
        ]

        let route = approvalRoute(from: userInfo)

        XCTAssertEqual(route, ApprovalRoute(sessionID: "sess-1", approvalID: "appr-1", tool: nil))
    }

    func testApprovalRouteReturnsNilWhenSessionIDMissing() {
        let userInfo: [AnyHashable: Any] = ["approval_id": "appr-1", "tool": "shell"]
        XCTAssertNil(approvalRoute(from: userInfo))
    }

    func testApprovalRouteReturnsNilWhenApprovalIDMissing() {
        let userInfo: [AnyHashable: Any] = ["session_id": "sess-1", "tool": "shell"]
        XCTAssertNil(approvalRoute(from: userInfo))
    }

    func testApprovalRouteReturnsNilForUnrelatedPush() {
        let userInfo: [AnyHashable: Any] = [
            "aps": ["alert": "Just a plain notification"],
        ]
        XCTAssertNil(approvalRoute(from: userInfo))
    }
}
