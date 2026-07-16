import SwiftUI

@main
struct GopherMindApp: App {
    @StateObject private var settings = AppSettings()

    var body: some Scene {
        WindowGroup {
            ContentView(settings: settings)
        }
    }
}

/// Root screen: the live conversation, with a nav link through to Settings.
struct ContentView: View {
    @ObservedObject var settings: AppSettings
    @StateObject private var viewModel: SessionViewModel

    init(settings: AppSettings) {
        self.settings = settings
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
    }
}

#Preview {
    ContentView(settings: AppSettings())
}
