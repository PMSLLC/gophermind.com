import UIKit
import UserNotifications

/// Bridges UIKit's push-notification lifecycle into the app. Wired via
/// `@UIApplicationDelegateAdaptor` in `GopherMindApp`. `settings` is injected
/// by the app root right after launch (see `GopherMindApp.body`), so the
/// device-token upload and the actionable-notification approve/deny actions
/// can build a `GopherMindService` pointed at whatever server is currently
/// configured.
final class AppDelegate: NSObject, UIApplicationDelegate {
    var settings: AppSettings?

    func application(
        _ application: UIApplication,
        didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil
    ) -> Bool {
        UNUserNotificationCenter.current().delegate = self
        registerApprovalCategory()

        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound]) { granted, error in
            if let error {
                NSLog("GopherMind: notification authorization request failed: \(error)")
            }
            guard granted else { return }
            DispatchQueue.main.async {
                application.registerForRemoteNotifications()
            }
        }
        return true
    }

    /// Uploads the APNs device token to the server so it can push
    /// approval-needed alerts to this device (`POST /devices`).
    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        guard let settings else {
            NSLog("GopherMind: got a device token before settings were injected; dropping it")
            return
        }
        let token = deviceTokenHex(deviceToken)
        Task {
            do {
                try await GopherMindService(settings: settings).registerDevice(token: token)
            } catch {
                NSLog("GopherMind: failed to upload device token: \(error)")
            }
        }
    }

    /// Non-fatal: the app works fine without push, e.g. no APNs entitlement
    /// on this build, no network at registration time, simulator without a
    /// signed-in Apple ID for push in some configurations.
    func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {
        NSLog("GopherMind: failed to register for remote notifications: \(error)")
    }

    /// Stretch: an "APPROVAL" category with Approve/Deny actions so the user
    /// can decide straight from the notification without opening the app.
    private func registerApprovalCategory() {
        let approve = UNNotificationAction(
            identifier: PushRouter.approveActionID,
            title: "Approve",
            options: [.authenticationRequired]
        )
        let deny = UNNotificationAction(
            identifier: PushRouter.denyActionID,
            title: "Deny",
            options: [.destructive, .authenticationRequired]
        )
        let category = UNNotificationCategory(
            identifier: PushRouter.approvalCategoryID,
            actions: [approve, deny],
            intentIdentifiers: [],
            options: []
        )
        UNUserNotificationCenter.current().setNotificationCategories([category])
    }
}

// `nonisolated` here (rather than isolating the conformance to `@MainActor`,
// which needs `AppDelegate` itself to be main-actor-isolated) because
// `UNUserNotificationCenterDelegate`'s requirements are plain `nonisolated`
// in the SDK. The MainActor-only work inside (building a `GopherMindService`,
// touching `PushRouter.shared`) hops over explicitly via `Task { @MainActor in ... }`.
extension AppDelegate: UNUserNotificationCenterDelegate {
    /// Shows the alert banner even while the app is in the foreground.
    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification,
        withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
    ) {
        completionHandler([.banner, .sound])
    }

    /// Tapping the notification (or its default action) deep-links into the
    /// session via `PushRouter`; tapping the Approve/Deny actions resolves
    /// the approval directly against the server.
    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse,
        withCompletionHandler completionHandler: @escaping () -> Void
    ) {
        defer { completionHandler() }

        guard let route = approvalRoute(from: response.notification.request.content.userInfo) else { return }
        let actionIdentifier = response.actionIdentifier

        Task { @MainActor in
            switch actionIdentifier {
            case PushRouter.approveActionID, PushRouter.denyActionID:
                guard let settings else {
                    NSLog("GopherMind: got a notification action before settings were injected; dropping it")
                    return
                }
                let approved = actionIdentifier == PushRouter.approveActionID
                do {
                    try await GopherMindService(settings: settings)
                        .approve(sessionID: route.sessionID, approvalID: route.approvalID, approved: approved)
                } catch {
                    NSLog("GopherMind: failed to send approval from notification action: \(error)")
                }

            default:
                PushRouter.shared.pendingRoute = route
            }
        }
    }
}
