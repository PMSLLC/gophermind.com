import SwiftUI

/// App root: saved sessions, newest-first. Tapping one continues it
/// server-side (the server retains that session's memory) — see the
/// LIMITATION note below. A toolbar button starts a fresh session; Settings
/// is reachable from here too.
///
/// LIMITATION: there is no GET-session-messages endpoint on the server (a
/// future addition), so opening an existing session continues its
/// server-side memory but the transcript view starts EMPTY for this app
/// launch — we deliberately do not try to replay history from `SessionInfo`
/// (it only carries a title/count/mtime, not the messages themselves).
struct SessionListView: View {
    @ObservedObject var settings: AppSettings
    @StateObject private var viewModel: SessionListViewModel
    var onSelect: (String) -> Void
    var onNewSession: () -> Void

    init(settings: AppSettings, onSelect: @escaping (String) -> Void, onNewSession: @escaping () -> Void) {
        self.settings = settings
        self.onSelect = onSelect
        self.onNewSession = onNewSession
        _viewModel = StateObject(wrappedValue: SessionListViewModel(service: GopherMindService(settings: settings)))
    }

    var body: some View {
        content
            .navigationTitle("Sessions")
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    NavigationLink("Settings") {
                        SettingsView(settings: settings)
                    }
                }
                ToolbarItem(placement: .topBarTrailing) {
                    Button {
                        onNewSession()
                    } label: {
                        Label("New Session", systemImage: "square.and.pencil")
                    }
                }
            }
            .task { await viewModel.load() }
            .alert("Error", isPresented: errorPresented) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(viewModel.errorMessage ?? "")
            }
    }

    @ViewBuilder
    private var content: some View {
        if viewModel.isLoading && viewModel.sessions.isEmpty {
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if viewModel.sessions.isEmpty {
            emptyState
        } else {
            List {
                ForEach(viewModel.sessions) { session in
                    Button {
                        onSelect(session.id)
                    } label: {
                        SessionRow(session: session)
                    }
                    .tint(.primary)
                    .swipeActions {
                        Button(role: .destructive) {
                            Task { await viewModel.delete(session.id) }
                        } label: {
                            Label("Delete", systemImage: "trash")
                        }
                    }
                }
            }
            .listStyle(.plain)
            .refreshable { await viewModel.load() }
        }
    }

    private var emptyState: some View {
        VStack(spacing: 12) {
            Image(systemName: "bubble.left.and.bubble.right")
                .font(.system(size: 40))
                .foregroundStyle(.secondary)
            Text("No sessions yet")
                .font(.headline)
            Text("Start a new conversation to see it here.")
                .font(.subheadline)
                .foregroundStyle(.secondary)
            Button("New Session", action: onNewSession)
                .buttonStyle(.borderedProminent)
                .padding(.top, 4)
        }
        .multilineTextAlignment(.center)
        .padding()
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .refreshable { await viewModel.load() }
    }

    private var errorPresented: Binding<Bool> {
        Binding(
            get: { viewModel.errorMessage != nil },
            set: { isPresented in if !isPresented { viewModel.errorMessage = nil } }
        )
    }
}

private struct SessionRow: View {
    let session: SessionInfo

    private static let dateFormatter: RelativeDateTimeFormatter = {
        let formatter = RelativeDateTimeFormatter()
        formatter.unitsStyle = .abbreviated
        return formatter
    }()

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            // The session ID is the meaningful name (for these, the repo name);
            // the server "title" is just the first message (a long seed prompt).
            Text(session.id.isEmpty ? "Untitled session" : session.id)
                .font(.body.weight(.medium))
                .lineLimit(1)
            Text("\(session.messages) messages · \(Self.dateFormatter.localizedString(for: session.modTime, relativeTo: Date()))")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .padding(.vertical, 2)
    }
}

#Preview {
    NavigationStack {
        SessionListView(settings: AppSettings(), onSelect: { _ in }, onNewSession: {})
    }
}
