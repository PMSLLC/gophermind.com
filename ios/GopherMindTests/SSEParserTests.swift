import XCTest
@testable import GopherMind

final class SSEParserTests: XCTestCase {
    /// A realistic multi-frame stream: a couple of `token`s, a multi-line
    /// `assistant` frame (data reassembly), a `tool_call`, a `tool_result`,
    /// an `approval-needed`, a `usage`, and the terminal `done` — matching
    /// the frame format and typed events in docs/mobile-serve.md.
    func testFullFrameSequence() {
        let stream = [
            "event: token",
            "data: Hel",
            "",
            "event: token",
            "data: lo",
            "",
            "event: assistant",
            "data: line one",
            "data: line two",
            "",
            "event: tool_call",
            "data: {\"name\":\"read_file\",\"args\":\"{\\\"path\\\":\\\"a.txt\\\"}\"}",
            "",
            "event: tool_result",
            "data: {\"name\":\"read_file\",\"text\":\"file contents\"}",
            "",
            "event: approval-needed",
            "data: {\"approval_id\":\"appr-1\",\"tool\":\"delete_file\",\"args\":\"{\\\"path\\\":\\\"a.txt\\\"}\"}",
            "",
            "event: usage",
            "data: {\"PromptTokens\":10,\"CompletionTokens\":5,\"TotalTokens\":15,\"CostUSD\":0.0023}",
            "",
            "event: done",
            "data:",
            "",
        ].joined(separator: "\n")

        var parser = SSEParser()
        let events = parser.feed(stream)

        XCTAssertEqual(events, [
            .token("Hel"),
            .token("lo"),
            .assistant("line one\nline two"),
            .toolCall(name: "read_file", args: "{\"path\":\"a.txt\"}"),
            .toolResult(name: "read_file", text: "file contents"),
            .approvalNeeded(approvalID: "appr-1", tool: "delete_file", args: "{\"path\":\"a.txt\"}"),
            .usage(prompt: 10, completion: 5, total: 15, costUSD: 0.0023),
            .done,
        ])
    }

    func testMultiLineDataReassemblyJoinsWithNewline() {
        var parser = SSEParser()
        var events: [AgentEvent] = []
        for line in ["event: assistant", "data: alpha", "data: beta", "data: gamma", ""] {
            if let event = parser.feed(line: line) {
                events.append(event)
            }
        }
        XCTAssertEqual(events, [.assistant("alpha\nbeta\ngamma")])
    }

    func testFeedingLineByLineMatchesFeedingWholeString() {
        let lines = ["event: token", "data: hi", ""]

        var lineByLine = SSEParser()
        var lineByLineEvents: [AgentEvent] = []
        for line in lines {
            if let event = lineByLine.feed(line: line) {
                lineByLineEvents.append(event)
            }
        }

        var whole = SSEParser()
        let wholeEvents = whole.feed(lines.joined(separator: "\n"))

        XCTAssertEqual(lineByLineEvents, [.token("hi")])
        XCTAssertEqual(wholeEvents, lineByLineEvents)
    }

    func testNoEventLineDefaultsToMessageAndIsSkippedLeniently() {
        // Per SSE spec, a frame with no `event:` line defaults to "message".
        // AgentEvent has no case for it, so the parser skips it rather than
        // crashing or misdecoding it as some other typed event.
        var parser = SSEParser()
        var events: [AgentEvent] = []
        for line in ["data: untyped payload", ""] {
            if let event = parser.feed(line: line) {
                events.append(event)
            }
        }
        XCTAssertTrue(events.isEmpty)
    }

    func testUnknownEventNameIsSkippedLeniently() {
        var parser = SSEParser()
        var events: [AgentEvent] = []
        for line in ["event: some-future-event", "data: whatever", ""] {
            if let event = parser.feed(line: line) {
                events.append(event)
            }
        }
        XCTAssertTrue(events.isEmpty)
    }

    func testErrorFrameFollowedByDone() {
        var parser = SSEParser()
        let events = parser.feed([
            "event: error",
            "data: run failed",
            "",
            "event: done",
            "data:",
            "",
        ].joined(separator: "\n"))

        XCTAssertEqual(events, [.error("run failed"), .done])
    }

    func testConsecutiveBlankLinesDoNotYieldExtraEvents() {
        var parser = SSEParser()
        let events = parser.feed([
            "event: token",
            "data: a",
            "",
            "",
            "event: token",
            "data: b",
            "",
        ].joined(separator: "\n"))

        XCTAssertEqual(events, [.token("a"), .token("b")])
    }
}
