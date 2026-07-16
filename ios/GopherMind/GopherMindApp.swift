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

/// Placeholder root screen. Networking/streaming lands in A2.
struct ContentView: View {
    @ObservedObject var settings: AppSettings

    var body: some View {
        NavigationStack {
            VStack(spacing: 16) {
                Text("GopherMind")
                    .font(.largeTitle)
                    .bold()
                Text("No server connected yet.")
                    .foregroundStyle(.secondary)
            }
            .padding()
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
