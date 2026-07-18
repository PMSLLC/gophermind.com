import SwiftUI
import UIKit

/// Scrolling transcript + input bar for a single session. Streams a live
/// token feed and tool activity; approvals render as interactive
/// `ApprovalCard`s (see `SessionViewModel.decide`).
struct ConversationView: View {
    @ObservedObject var viewModel: SessionViewModel

    var body: some View {
        VStack(spacing: 0) {
            if let subtitle = configSubtitle {
                Text(subtitle)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .center)
                    .padding(.top, 6)
            }

            if viewModel.items.isEmpty {
                emptyState
            } else {
                ScrollViewReader { proxy in
                    ScrollView {
                        LazyVStack(alignment: .leading, spacing: 12) {
                            ForEach(viewModel.items) { item in
                                ConversationItemRow(item: item) { approvalID, approved in
                                    UINotificationFeedbackGenerator().notificationOccurred(approved ? .success : .warning)
                                    _ = viewModel.decide(approvalID: approvalID, approved: approved)
                                }
                                .id(item.id)
                            }
                        }
                        .padding()
                    }
                    .onChange(of: viewModel.items) { _, newItems in
                        guard let lastID = newItems.last?.id else { return }
                        withAnimation(.easeOut(duration: 0.2)) {
                            proxy.scrollTo(lastID, anchor: .bottom)
                        }
                    }
                }
            }

            Divider()
            inputBar
        }
        .task {
            // Opening an existing session (sessionID already set, nothing on
            // screen yet) loads its stored transcript; a brand-new session
            // (sessionID nil) is a no-op — see `loadHistoryIfNeeded`.
            await viewModel.loadHistoryIfNeeded()
            await viewModel.loadConfigIfNeeded()
        }
    }

    /// "<model> · <Mode>" (e.g. "qwen3.6-35b · Conversational") for the
    /// header, from `viewModel.config`; `nil` before it loads or when both
    /// model and mode are unset (nothing meaningful to show).
    private var configSubtitle: String? {
        guard let config = viewModel.config, !(config.model.isEmpty && config.mode.isEmpty) else { return nil }
        let modeLabel = config.mode.isEmpty ? "Coding" : config.mode.capitalized
        guard !config.model.isEmpty else { return modeLabel }
        return "\(config.model) · \(modeLabel)"
    }

    private var emptyState: some View {
        VStack(spacing: 8) {
            Image(systemName: "bubble.left.and.bubble.right")
                .font(.system(size: 32))
                .foregroundStyle(.secondary)
            Text("Say something to get started")
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private var inputBar: some View {
        HStack(alignment: .bottom, spacing: 8) {
            TextField("Message", text: $viewModel.inputText, axis: .vertical)
                .textFieldStyle(.roundedBorder)
                .lineLimit(1...5)
                .disabled(viewModel.isStreaming)

            Button {
                let task = viewModel.inputText
                viewModel.inputText = ""
                UIImpactFeedbackGenerator(style: .light).impactOccurred()
                Task { await viewModel.send(task) }
            } label: {
                Image(systemName: "arrow.up.circle.fill")
                    .font(.system(size: 28))
            }
            .disabled(viewModel.isStreaming || viewModel.inputText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
        }
        .padding()
    }
}

/// Renders one `ConversationItem` per its `Kind`. `onDecide` is forwarded to
/// `ApprovalCard` for `.approvalPending` rows.
private struct ConversationItemRow: View {
    let item: ConversationItem
    var onDecide: (String, Bool) -> Void = { _, _ in }

    var body: some View {
        switch item.kind {
        case .user(let text):
            HStack {
                Spacer(minLength: 40)
                Text(text)
                    .padding(10)
                    .background(Color.accentColor.opacity(0.15))
                    .clipShape(RoundedRectangle(cornerRadius: 12))
            }

        case .assistant(let text):
            HStack {
                Text(text)
                    .padding(10)
                    .background(Color.secondary.opacity(0.1))
                    .clipShape(RoundedRectangle(cornerRadius: 12))
                Spacer(minLength: 40)
            }

        case .tool(let name, let args, let result):
            HStack(alignment: .top, spacing: 8) {
                if result == nil {
                    ProgressView()
                        .controlSize(.small)
                } else {
                    Image(systemName: "checkmark.circle")
                        .foregroundStyle(.secondary)
                }
                VStack(alignment: .leading, spacing: 2) {
                    Text("\(name)(\(args))")
                        .font(.system(.caption, design: .monospaced))
                    if let result {
                        Text(result)
                            .font(.system(.caption2, design: .monospaced))
                            .foregroundStyle(.secondary)
                    }
                }
            }
            .padding(8)
            .background(Color.secondary.opacity(0.08))
            .clipShape(RoundedRectangle(cornerRadius: 8))

        case .approvalPending(let approvalID, let tool, let args):
            ApprovalCard(state: .pending(approvalID: approvalID, tool: tool, args: args), onDecide: onDecide)

        case .approvalDecided(_, let tool, let args, let approved):
            ApprovalCard(state: .decided(tool: tool, args: args, approved: approved))

        case .approvalExpired(_, let tool, let args):
            ApprovalCard(state: .expired(tool: tool, args: args))

        case .usage(let prompt, let completion, let total, let costUSD):
            Text("tokens: \(total) (in \(prompt), out \(completion)) · $\(costUSD, specifier: "%.4f")")
                .font(.caption2)
                .foregroundStyle(.secondary)

        case .errorLine(let text):
            Text(text)
                .font(.footnote)
                .foregroundStyle(.red)
                .padding(8)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Color.red.opacity(0.1))
                .clipShape(RoundedRectangle(cornerRadius: 8))
        }
    }
}

#Preview {
    NavigationStack {
        ConversationView(viewModel: SessionViewModel(service: GopherMindService(settings: AppSettings())))
    }
}
