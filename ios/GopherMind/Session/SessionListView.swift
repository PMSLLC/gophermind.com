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
    private let service: GopherMindServicing
    var onSelect: (String) -> Void
    /// Called with the id of a session just created via the New Session
    /// sheet's model picker, so the caller can navigate straight into it.
    var onNewSession: (String) -> Void
    @State private var showingNewSessionSheet = false
    @State private var renameTarget: SessionInfo?
    @State private var renameText = ""

    init(settings: AppSettings, onSelect: @escaping (String) -> Void, onNewSession: @escaping (String) -> Void) {
        self.settings = settings
        self.onSelect = onSelect
        self.onNewSession = onNewSession
        self.service = GopherMindService(settings: settings)
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
                        showingNewSessionSheet = true
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
            .sheet(isPresented: $showingNewSessionSheet) {
                NewSessionSheet(service: service, onCreated: onNewSession)
            }
            .alert("Rename session", isPresented: renamePresented) {
                TextField("Name", text: $renameText)
                Button("Cancel", role: .cancel) { renameTarget = nil }
                Button("Save") {
                    if let target = renameTarget {
                        let newName = renameText
                        Task { await viewModel.rename(target.id, to: newName) }
                    }
                    renameTarget = nil
                }
            } message: {
                Text("Leave blank to use the default title.")
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
                    .swipeActions(edge: .trailing) {
                        Button(role: .destructive) {
                            Task { await viewModel.delete(session.id) }
                        } label: {
                            Label("Delete", systemImage: "trash")
                        }
                        Button {
                            renameTarget = session
                            renameText = session.name
                        } label: {
                            Label("Rename", systemImage: "pencil")
                        }
                        .tint(.blue)
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
            Button("New Session") { showingNewSessionSheet = true }
                .buttonStyle(.borderedProminent)
                .padding(.top, 4)
        }
        .multilineTextAlignment(.center)
        .padding()
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .refreshable { await viewModel.load() }
    }

    private var renamePresented: Binding<Bool> {
        Binding(
            get: { renameTarget != nil },
            set: { presented in if !presented { renameTarget = nil } }
        )
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
            // Prefer the custom name (rename); fall back to the session ID,
            // which for these is the repo name. The server "title" is just the
            // first message (a long seed prompt), so it is not shown here.
            Text(session.displayName.isEmpty ? "Untitled session" : session.displayName)
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
        SessionListView(settings: AppSettings(), onSelect: { _ in }, onNewSession: { _ in })
    }
}
