import SwiftUI

/// Interactive card for a gated tool call. Renders tool name + args
/// (monospaced, scrollable if long) plus Approve/Deny while pending; once
/// decided or expired, the buttons are gone and the outcome is shown
/// instead. `onDecide` fires `SessionViewModel.decide(approvalID:approved:)`.
struct ApprovalCard: View {
    enum State {
        case pending(approvalID: String, tool: String, args: String)
        case decided(tool: String, args: String, approved: Bool)
        case expired(tool: String, args: String)
    }

    let state: State
    var onDecide: (String, Bool) -> Void = { _, _ in }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(tool)
                .font(.subheadline.weight(.semibold))

            ScrollView(.horizontal, showsIndicators: false) {
                Text(args)
                    .font(.system(.caption, design: .monospaced))
                    .foregroundStyle(.secondary)
            }

            footer
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(tint.opacity(0.1))
        .clipShape(RoundedRectangle(cornerRadius: 10))
    }

    @ViewBuilder
    private var footer: some View {
        switch state {
        case .pending(let approvalID, _, _):
            HStack(spacing: 12) {
                Button("Deny", role: .destructive) {
                    onDecide(approvalID, false)
                }
                .buttonStyle(.bordered)

                Button("Approve") {
                    onDecide(approvalID, true)
                }
                .buttonStyle(.borderedProminent)
            }

        case .decided(_, _, let approved):
            Label(approved ? "Approved" : "Denied", systemImage: approved ? "checkmark.circle.fill" : "xmark.circle.fill")
                .font(.footnote.weight(.medium))
                .foregroundStyle(approved ? .green : .red)

        case .expired:
            Label("Expired — no longer actionable", systemImage: "clock.badge.exclamationmark")
                .font(.footnote.weight(.medium))
                .foregroundStyle(.secondary)
        }
    }

    private var tool: String {
        switch state {
        case .pending(_, let tool, _), .decided(let tool, _, _), .expired(let tool, _):
            return tool
        }
    }

    private var args: String {
        switch state {
        case .pending(_, _, let args), .decided(_, let args, _), .expired(_, let args):
            return args
        }
    }

    private var tint: Color {
        switch state {
        case .pending: return .orange
        case .decided(_, _, let approved): return approved ? .green : .red
        case .expired: return .secondary
        }
    }
}

#Preview {
    VStack(spacing: 12) {
        ApprovalCard(state: .pending(approvalID: "ap-1", tool: "shell", args: "rm -rf /tmp/build"))
        ApprovalCard(state: .decided(tool: "shell", args: "rm -rf /tmp/build", approved: true))
        ApprovalCard(state: .decided(tool: "shell", args: "rm -rf /tmp/build", approved: false))
        ApprovalCard(state: .expired(tool: "shell", args: "rm -rf /tmp/build"))
    }
    .padding()
}
