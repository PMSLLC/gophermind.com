import XCTest
@testable import GopherMind

/// Exercises `SessionViewModel.reduce` directly — a pure fold, no UI or
/// network involved.
final class ReducerTests: XCTestCase {
    func testTokensAccumulateIntoOneAssistantItem() {
        var items: [ConversationItem] = []
        items = SessionViewModel.reduce(items, .token("Hel"))
        items = SessionViewModel.reduce(items, .token("lo"))

        XCTAssertEqual(items.count, 1)
        XCTAssertEqual(items[0].kind, .assistant("Hello"))
    }

    func testToolCallThenResultCollapseIntoOneRow() {
        var items: [ConversationItem] = []
        items = SessionViewModel.reduce(items, .toolCall(name: "search", args: "{\"q\":\"gophers\"}"))
        XCTAssertEqual(items.count, 1)
        XCTAssertEqual(items[0].kind, .tool(name: "search", args: "{\"q\":\"gophers\"}", result: nil))

        items = SessionViewModel.reduce(items, .toolResult(name: "search", text: "3 results"))
        XCTAssertEqual(items.count, 1)
        XCTAssertEqual(items[0].kind, .tool(name: "search", args: "{\"q\":\"gophers\"}", result: "3 results"))
    }

    func testApprovalNeededYieldsApprovalPendingWithCorrectFields() {
        var items: [ConversationItem] = []
        items = SessionViewModel.reduce(items, .approvalNeeded(approvalID: "ap-1", tool: "shell", args: "rm -rf /"))

        XCTAssertEqual(items.count, 1)
        XCTAssertEqual(items[0].kind, .approvalPending(approvalID: "ap-1", tool: "shell", args: "rm -rf /"))
    }

    func testErrorYieldsErrorLine() {
        var items: [ConversationItem] = []
        items = SessionViewModel.reduce(items, .error("boom"))

        XCTAssertEqual(items.count, 1)
        XCTAssertEqual(items[0].kind, .errorLine("boom"))
    }

    func testUsageUpdatesTheExistingIndicatorInPlace() {
        var items: [ConversationItem] = []
        items = SessionViewModel.reduce(items, .usage(prompt: 10, completion: 5, total: 15, costUSD: 0.001))
        items = SessionViewModel.reduce(items, .usage(prompt: 20, completion: 8, total: 28, costUSD: 0.002))

        XCTAssertEqual(items.count, 1)
        XCTAssertEqual(items[0].kind, .usage(prompt: 20, completion: 8, total: 28, costUSD: 0.002))
    }

    func testDoneIsANoOp() {
        let before: [ConversationItem] = [ConversationItem(kind: .user("hi"))]
        let after = SessionViewModel.reduce(before, .done)
        XCTAssertEqual(after, before)
    }

    /// A full, realistic turn: user message set up by `send`, streamed
    /// tokens, a tool call + result, more tokens, then done.
    func testRealisticEventSequenceProducesExpectedItemList() {
        var items: [ConversationItem] = [ConversationItem(kind: .user("What's the weather?"))]

        items = SessionViewModel.reduce(items, .token("Let"))
        items = SessionViewModel.reduce(items, .token(" me check."))
        items = SessionViewModel.reduce(items, .toolCall(name: "weather", args: "{\"city\":\"SF\"}"))
        items = SessionViewModel.reduce(items, .toolResult(name: "weather", text: "62F, foggy"))
        items = SessionViewModel.reduce(items, .token("It's 62F and foggy."))
        items = SessionViewModel.reduce(items, .done)

        XCTAssertEqual(items.count, 4)
        XCTAssertEqual(items[0].kind, .user("What's the weather?"))
        XCTAssertEqual(items[1].kind, .assistant("Let me check."))
        XCTAssertEqual(items[2].kind, .tool(name: "weather", args: "{\"city\":\"SF\"}", result: "62F, foggy"))
        XCTAssertEqual(items[3].kind, .assistant("It's 62F and foggy."))
    }
}
