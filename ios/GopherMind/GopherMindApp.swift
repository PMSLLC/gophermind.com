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
/// its server-side memory — see `SessionListView`'s LIMITATION note; a
/// session just created via the New Session sheet is also routed here,
/// since by the time we navigate it already has an id), or a
/// push-notification deep-link into a pending approval.
private enum SessionRoute: Hashable {
    case existing(id: String)
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
    @State private var pairedHost: String?

    var body: some View {
        NavigationStack(path: $path) {
            SessionListView(
                settings: settings,
                onSelect: { id in path.append(SessionRoute.existing(id: id)) },
                onNewSession: { id in path.append(SessionRoute.existing(id: id)) }
            )
            // Rebuild (and reload sessions) whenever the configured server changes,
            // e.g. right after a setup link is applied.
            .id(settings.serverURL)
            .navigationDestination(for: SessionRoute.self) { route in
                switch route {
                case .existing(let id):
                    ConversationView(viewModel: SessionViewModel(service: GopherMindService(settings: settings), sessionID: id))
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
        // One-tap setup: a gophermind://setup?c=<base64> link carries the whole
        // config (server URL + token + optional HMAC) into the app.
        .onOpenURL { url in
            guard let cfg = PairingConfig.parse(url.absoluteString) else { return }
            settings.apply(cfg)
            pairedHost = URL(string: cfg.serverURL)?.host ?? cfg.serverURL
        }
        .alert("Configured", isPresented: Binding(
            get: { pairedHost != nil },
            set: { if !$0 { pairedHost = nil } }
        )) {
            Button("OK") { pairedHost = nil }
        } message: {
            Text("Connected to \(pairedHost ?? ""). Your sessions should load now.")
        }
    }
}

#Preview {
    ContentView(settings: AppSettings(), router: PushRouter.shared)
}
