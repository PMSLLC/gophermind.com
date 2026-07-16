import Foundation

/// One row of the conversation transcript. `kind` is the union of everything
/// `SessionViewModel.reduce` can produce from an `AgentEvent` stream.
struct ConversationItem: Identifiable, Equatable {
    let id: UUID
    var kind: Kind

    init(id: UUID = UUID(), kind: Kind) {
        self.id = id
        self.kind = kind
    }

    enum Kind: Equatable {
        /// A message the user sent.
        case user(String)
        /// Assistant prose — grows in place as `token`/`assistant` events
        /// arrive back-to-back (see `SessionViewModel.reduce`).
        case assistant(String)
        /// A tool invocation. `result` is `nil` until its `tool_result` lands.
        case tool(name: String, args: String, result: String?)
        /// A gated tool call blocked on approval; interactive (see `ApprovalCard`).
        case approvalPending(approvalID: String, tool: String, args: String)
        /// A pending approval the user has acted on; `approved` records the choice.
        case approvalDecided(approvalID: String, tool: String, args: String, approved: Bool)
        /// A pending approval still unresolved when its turn ended. The server
        /// auto-denies on timeout, so it's no longer actionable.
        case approvalExpired(approvalID: String, tool: String, args: String)
        /// Running per-session usage totals for the turn.
        case usage(prompt: Int, completion: Int, total: Int, costUSD: Double)
        /// A surfaced error line.
        case errorLine(String)
    }
}
