import SwiftUI

@main
struct GopherMindApp: App {
    // Bridges push registration (A5) into the app; `settings` is handed to
    // it below once it exists, since AppDelegate needs it to build a
    // `GopherMindService` for the device-token upload.
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    @StateObject private var settings = AppSettings()
    @StateObject private var router = PushRouter.shared

    var body: some Scene {
        WindowGroup {
            ContentView(settings: settings, router: router)
                .onAppear { appDelegate.settings = settings }
        }
    }
}

/// One entry in the root nav stack: either an existing session (continues
/// its server-side memory — see `SessionListView`'s LIMITATION note), a
/// fresh one, or a push-notification deep-link into a pending approval.
private enum SessionRoute: Hashable {
    case existing(id: String)
    case new
    case approval(ApprovalRoute)
}

/// Root screen: the session list, with a nav path down into a conversation
/// (existing session, new session, or a push deep-link) and a link through
/// to Settings from the list's toolbar. Observes `PushRouter.pendingRoute`
/// to deep-link into a session when an approval-needed push is tapped (A5).
struct ContentView: View {
    @ObservedObject var settings: AppSettings
    @ObservedObject var router: PushRouter
    @State private var path = NavigationPath()

    var body: some View {
        NavigationStack(path: $path) {
            SessionListView(
                settings: settings,
                onSelect: { id in path.append(SessionRoute.existing(id: id)) },
                onNewSession: { path.append(SessionRoute.new) }
            )
            .navigationDestination(for: SessionRoute.self) { route in
                switch route {
                case .existing(let id):
                    ConversationView(viewModel: SessionViewModel(service: GopherMindService(settings: settings), sessionID: id))
                case .new:
                    ConversationView(viewModel: SessionViewModel(service: GopherMindService(settings: settings)))
                case .approval(let approvalRoute):
                    let viewModel = SessionViewModel(service: GopherMindService(settings: settings))
                    ConversationView(viewModel: viewModel)
                        .onAppear { viewModel.openApprovalRoute(approvalRoute) }
                }
            }
        }
        .onChange(of: router.pendingRoute) { _, route in
            guard let route else { return }
            path.append(SessionRoute.approval(route))
            router.pendingRoute = nil
        }
    }
}

#Preview {
    ContentView(settings: AppSettings(), router: PushRouter.shared)
}
