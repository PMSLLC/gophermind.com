import Foundation
import Combine

/// A deep-link target extracted from an "approval needed" APNs push. Built
/// by the pure `approvalRoute(from:)` helper below and consumed by the app
/// root to switch to the right session and surface its pending approval
/// (see `SessionViewModel.openApprovalRoute`).
struct ApprovalRoute: Equatable, Hashable {
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

/// One entry in the root nav stack: either an existing session (continues
/// its server-side memory — see `SessionListView`'s LIMITATION note; a
/// session just created via the New Session sheet is also routed here,
/// since by the time we navigate it already has an id), or a
/// push-notification deep-link into a pending approval.
enum SessionRoute: Hashable {
    case existing(id: String)
    case approval(ApprovalRoute)
}

/// The session id a route refers to, regardless of whether it's an existing
/// session or an approval deep-link into one -- what `applyPushRoute` uses
/// to tell "already viewing this session" from "navigating to a new one".
func sessionID(of route: SessionRoute) -> String {
    switch route {
    case .existing(let id): return id
    case .approval(let approvalRoute): return approvalRoute.sessionID
    }
}

/// Computes the nav path to apply when a push delivers `route`, given the
/// current path. A push for the session already on top of the stack
/// replaces that entry instead of appending a new one -- e.g. a second
/// approval-needed push arriving while the first one's ConversationView is
/// already showing pushed a duplicate screen for the same session, so
/// Back had to be tapped twice to leave it. Pure, so it is testable without
/// a live NavigationStack.
func applyPushRoute(_ route: ApprovalRoute, to path: [SessionRoute]) -> [SessionRoute] {
    var path = path
    let newRoute = SessionRoute.approval(route)
    if let last = path.last, sessionID(of: last) == route.sessionID {
        path[path.count - 1] = newRoute
    } else {
        path.append(newRoute)
    }
    return path
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
