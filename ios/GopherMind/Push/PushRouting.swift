import Foundation
import Combine

/// A deep-link target extracted from an "approval needed" APNs push. Built
/// by the pure `approvalRoute(from:)` helper below and consumed by the app
/// root to switch to the right session and surface its pending approval
/// (see `SessionViewModel.openApprovalRoute`).
struct ApprovalRoute: Equatable {
    let sessionID: String
    let approvalID: String
    /// Not currently sent by the server (see `cmd/gophermind/apns.go`,
    /// `newApprovalNotifier`) — read opportunistically for forward
    /// compatibility. `nil` when the payload omits it.
    let tool: String?
}

/// Formats a device token as the lowercase hex string the server's
/// `POST /devices` expects (`APIClient.registerDevice`). Pure — safe to unit
/// test against a known `Data` without touching APNs.
func deviceTokenHex(_ data: Data) -> String {
    data.map { String(format: "%02x", $0) }.joined()
}

/// Extracts a session/approval deep-link from a push's `userInfo`. The
/// server sends `session_id` and `approval_id` at the top level of the
/// payload, alongside `aps` (see `apnsPusher.Push` / `newApprovalNotifier`
/// in `cmd/gophermind/apns.go`). Returns `nil` when either required key is
/// missing or not a string — e.g. a push with no deep-link data at all.
func approvalRoute(from userInfo: [AnyHashable: Any]) -> ApprovalRoute? {
    guard let sessionID = userInfo["session_id"] as? String,
          let approvalID = userInfo["approval_id"] as? String else {
        return nil
    }
    let tool = userInfo["tool"] as? String
    return ApprovalRoute(sessionID: sessionID, approvalID: approvalID, tool: tool)
}

/// Shared observable the app root watches to navigate when a push
/// notification is tapped. `AppDelegate` publishes into `pendingRoute` from
/// `UNUserNotificationCenterDelegate.userNotificationCenter(_:didReceive:withCompletionHandler:)`;
/// `ContentView` consumes and clears it.
@MainActor
final class PushRouter: ObservableObject {
    static let shared = PushRouter()

    /// Notification category / action identifiers for the actionable
    /// "Approve"/"Deny" buttons registered in `AppDelegate`.
    static let approvalCategoryID = "APPROVAL"
    static let approveActionID = "APPROVAL_APPROVE"
    static let denyActionID = "APPROVAL_DENY"

    @Published var pendingRoute: ApprovalRoute?

    private init() {}
}
