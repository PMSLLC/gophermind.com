import SwiftUI

/// A diagnostics screen (Settings → Connection Debug) that runs a sequence of
/// checks against the configured server and shows, step by step, exactly where
/// a problem is: config, reachability, auth, or response decoding.
struct ConnectionDebugView: View {
    @StateObject private var diag: ConnectionDiagnostics

    init(settings: AppSettings) {
        _diag = StateObject(wrappedValue: ConnectionDiagnostics(settings: settings))
    }

    var body: some View {
        List {
            Section {
                Button {
                    Task { await diag.run() }
                } label: {
                    HStack {
                        Text(diag.running ? "Running…" : "Run diagnostics")
                        if diag.running {
                            Spacer()
                            ProgressView()
                        }
                    }
                }
                .disabled(diag.running)
            } footer: {
                Text("Checks configuration, network reachability, authentication, and response decoding against your configured server.")
            }

            if !diag.steps.isEmpty {
                Section("Results") {
                    ForEach(diag.steps) { step in
                        VStack(alignment: .leading, spacing: 4) {
                            HStack(spacing: 8) {
                                Image(systemName: icon(step.status))
                                    .foregroundStyle(color(step.status))
                                Text(step.title)
                                    .font(.headline)
                            }
                            Text(step.detail)
                                .font(.system(.footnote, design: .monospaced))
                                .foregroundStyle(.secondary)
                                .textSelection(.enabled)
                        }
                        .padding(.vertical, 2)
                    }
                }

                Section {
                    Button {
                        UIPasteboard.general.string = diag.report
                    } label: {
                        Label("Copy full report", systemImage: "doc.on.doc")
                    }
                }
            }
        }
        .navigationTitle("Connection Debug")
        .navigationBarTitleDisplayMode(.inline)
        .task { await diag.run() }   // auto-run on open
    }

    private func icon(_ s: DiagStep.Status) -> String {
        switch s {
        case .ok:   return "checkmark.circle.fill"
        case .warn: return "exclamationmark.triangle.fill"
        case .fail: return "xmark.octagon.fill"
        case .info: return "info.circle"
        }
    }

    private func color(_ s: DiagStep.Status) -> Color {
        switch s {
        case .ok:   return .green
        case .warn: return .orange
        case .fail: return .red
        case .info: return .blue
        }
    }
}

#Preview {
    NavigationStack {
        ConnectionDebugView(settings: AppSettings())
    }
}
