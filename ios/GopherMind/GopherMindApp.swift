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

/// Root screen: the live conversation, with a nav link through to Settings.
/// Also observes `PushRouter.pendingRoute` to deep-link into a session when
/// an approval-needed push is tapped (A5).
struct ContentView: View {
    @ObservedObject var settings: AppSettings
    @ObservedObject var router: PushRouter
    @StateObject private var viewModel: SessionViewModel

    init(settings: AppSettings, router: PushRouter) {
        self.settings = settings
        self.router = router
        _viewModel = StateObject(wrappedValue: SessionViewModel(service: GopherMindService(settings: settings)))
    }

    var body: some View {
        NavigationStack {
            ConversationView(viewModel: viewModel)
                .navigationTitle("GopherMind")
                .toolbar {
                    ToolbarItem(placement: .topBarTrailing) {
                        NavigationLink("Settings") {
                            SettingsView(settings: settings)
                        }
                    }
                }
        }
        .onChange(of: router.pendingRoute) { _, route in
            guard let route else { return }
            viewModel.openApprovalRoute(route)
            router.pendingRoute = nil
        }
    }
}

#Preview {
    ContentView(settings: AppSettings(), router: PushRouter.shared)
}
