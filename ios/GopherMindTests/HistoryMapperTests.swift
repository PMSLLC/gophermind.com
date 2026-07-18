import XCTest
@testable import GopherMind

/// Exercises `SessionViewModel.historyItems(from:)` directly — a pure map
/// from stored OpenAI-format messages to `ConversationItem` rows, no UI or
/// network involved.
final class HistoryMapperTests: XCTestCase {
    func testEmptyMessagesProduceEmptyItems() {
        XCTAssertEqual(SessionViewModel.historyItems(from: []), [])
    }

    func testSystemMessageIsSkipped() {
        let messages = [
            StoredMessage(role: "system", content: "You are a helpful agent.", toolCalls: nil, toolCallID: nil),
        ]
        XCTAssertEqual(SessionViewModel.historyItems(from: messages), [])
    }

    /// A realistic turn: system (dropped), user, assistant with a tool call,
    /// the tool's result, then a closing assistant message — the tool
    /// call + its result collapse into one `.tool` row carrying the result.
    func testRealisticSequenceCollapsesToolCallAndResult() {
        let messages: [StoredMessage] = [
            StoredMessage(role: "system", content: "sys", toolCalls: nil, toolCallID: nil),
            StoredMessage(role: "user", content: "search for gophers", toolCalls: nil, toolCallID: nil),
            StoredMessage(
                role: "assistant",
                content: "Let me look that up.",
                toolCalls: [
                    StoredMessage.ToolCall(id: "call-1", function: StoredMessage.Function(name: "search", arguments: "{\"q\":\"gophers\"}")),
                ],
                toolCallID: nil
            ),
            StoredMessage(role: "tool", content: "3 results", toolCalls: nil, toolCallID: "call-1"),
            StoredMessage(role: "assistant", content: "Found 3 results.", toolCalls: nil, toolCallID: nil),
        ]

        let items = SessionViewModel.historyItems(from: messages)

        XCTAssertEqual(items.map(\.kind), [
            .user("search for gophers"),
            .assistant("Let me look that up."),
            .tool(name: "search", args: "{\"q\":\"gophers\"}", result: "3 results"),
            .assistant("Found 3 results."),
        ])
    }

    func testToolResultWithoutIDFallsBackToMostRecentPendingRow() {
        let messages: [StoredMessage] = [
            StoredMessage(
                role: "assistant",
                content: nil,
                toolCalls: [
                    StoredMessage.ToolCall(id: nil, function: StoredMessage.Function(name: "read", arguments: "file.txt")),
                ],
                toolCallID: nil
            ),
            StoredMessage(role: "tool", content: "file contents", toolCalls: nil, toolCallID: nil),
        ]

        let items = SessionViewModel.historyItems(from: messages)

        XCTAssertEqual(items.map(\.kind), [
            .tool(name: "read", args: "file.txt", result: "file contents"),
        ])
    }

    func testEmptyAssistantContentDoesNotAppendABlankRow() {
        let messages: [StoredMessage] = [
            StoredMessage(role: "assistant", content: "", toolCalls: nil, toolCallID: nil),
        ]
        XCTAssertEqual(SessionViewModel.historyItems(from: messages), [])
    }
}
