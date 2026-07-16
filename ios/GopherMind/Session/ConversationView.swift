import SwiftUI

/// Scrolling transcript + input bar for a single session. Streams a live
/// token feed and tool activity; approvals render read-only (A4 adds
/// Approve/Deny).
struct ConversationView: View {
    @ObservedObject var viewModel: SessionViewModel

    var body: some View {
        VStack(spacing: 0) {
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 12) {
                        ForEach(viewModel.items) { item in
                            ConversationItemRow(item: item)
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

            Divider()
            inputBar
        }
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

/// Renders one `ConversationItem` per its `Kind`.
private struct ConversationItemRow: View {
    let item: ConversationItem

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

        case .approvalPending(_, let tool, _):
            Text("awaiting approval: \(tool)")
                .font(.footnote)
                .foregroundStyle(.orange)
                .padding(8)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Color.orange.opacity(0.1))
                .clipShape(RoundedRectangle(cornerRadius: 8))

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
