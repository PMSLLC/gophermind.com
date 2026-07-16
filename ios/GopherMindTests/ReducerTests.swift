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

    // MARK: - token/assistant dedup (A6)

    /// Tokens "Hel"+"lo" then the committed `assistant` "Hello" must not
    /// double the prose: the assistant event replaces (not appends to) the
    /// accumulated text since it's a prefix match.
    func testTokensThenMatchingAssistantReplacesRatherThanDuplicates() {
        var items: [ConversationItem] = []
        items = SessionViewModel.reduce(items, .token("Hel"))
        items = SessionViewModel.reduce(items, .token("lo"))
        items = SessionViewModel.reduce(items, .assistant("Hello"))

        XCTAssertEqual(items.count, 1)
        XCTAssertEqual(items[0].kind, .assistant("Hello"))
    }

    /// The accumulated text need only be a *prefix* of the assistant event's
    /// text (not an exact match) to count as the same prose.
    func testTokenThenPrefixMatchingAssistantReplaces() {
        var items: [ConversationItem] = []
        items = SessionViewModel.reduce(items, .token("Hi"))
        items = SessionViewModel.reduce(items, .assistant("Hi there"))

        XCTAssertEqual(items.count, 1)
        XCTAssertEqual(items[0].kind, .assistant("Hi there"))
    }

    /// A standalone `assistant` event with no preceding tokens (e.g. a
    /// warning/critic note) gets its own line, not folded into anything.
    func testStandaloneAssistantWithNoPrecedingTokensGetsItsOwnLine() {
        var items: [ConversationItem] = [ConversationItem(kind: .user("hi"))]
        items = SessionViewModel.reduce(items, .assistant("\u{26A0} warning"))

        XCTAssertEqual(items.count, 2)
        XCTAssertEqual(items[1].kind, .assistant("\u{26A0} warning"))
    }

    /// Once an assistant item is finalized by a committed `assistant` event,
    /// further `token`s start a NEW assistant item rather than reopening it.
    func testTokensAfterFinalizedAssistantStartANewItem() {
        var items: [ConversationItem] = []
        items = SessionViewModel.reduce(items, .token("Hel"))
        items = SessionViewModel.reduce(items, .assistant("Hello"))
        items = SessionViewModel.reduce(items, .token("More"))

        XCTAssertEqual(items.count, 2)
        XCTAssertEqual(items[0].kind, .assistant("Hello"))
        XCTAssertEqual(items[1].kind, .assistant("More"))
    }
}
