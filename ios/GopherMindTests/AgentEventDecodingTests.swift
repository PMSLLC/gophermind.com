import XCTest
@testable import GopherMind

/// Verifies each SSE event/data pair decodes to the correct `AgentEvent`
/// case with the correct fields, per the "Typed events" table in
/// docs/mobile-serve.md.
final class AgentEventDecodingTests: XCTestCase {
    private func decodeOne(event: String?, data: [String]) -> AgentEvent? {
        var parser = SSEParser()
        var lines: [String] = []
        if let event {
            lines.append("event: \(event)")
        }
        lines.append(contentsOf: data.map { "data: \($0)" })
        lines.append("")

        var result: AgentEvent?
        for line in lines {
            if let event = parser.feed(line: line) {
                result = event
            }
        }
        return result
    }

    func testTokenIsRawTextNotJSON() {
        XCTAssertEqual(decodeOne(event: "token", data: ["not-json {"]), .token("not-json {"))
    }

    func testAssistantIsRawText() {
        XCTAssertEqual(decodeOne(event: "assistant", data: ["Final answer."]), .assistant("Final answer."))
    }

    func testToolCallDecodesNameAndArgs() {
        let event = decodeOne(event: "tool_call", data: [#"{"name":"grep","args":"pattern=foo"}"#])
        XCTAssertEqual(event, .toolCall(name: "grep", args: "pattern=foo"))
    }

    func testToolResultDecodesNameAndText() {
        let event = decodeOne(event: "tool_result", data: [#"{"name":"grep","text":"3 matches"}"#])
        XCTAssertEqual(event, .toolResult(name: "grep", text: "3 matches"))
    }

    func testUsageDecodesCapitalizedGoFieldNames() {
        let event = decodeOne(
            event: "usage",
            data: [#"{"PromptTokens":100,"CompletionTokens":42,"TotalTokens":142,"CostUSD":0.0157}"#]
        )
        XCTAssertEqual(event, .usage(prompt: 100, completion: 42, total: 142, costUSD: 0.0157))
    }

    func testApprovalNeededDecodesApprovalIDToolArgs() {
        let event = decodeOne(
            event: "approval-needed",
            data: [#"{"approval_id":"appr-42","tool":"run_shell","args":"rm -rf /tmp/x"}"#]
        )
        XCTAssertEqual(event, .approvalNeeded(approvalID: "appr-42", tool: "run_shell", args: "rm -rf /tmp/x"))
    }

    func testErrorIsPlainText() {
        XCTAssertEqual(decodeOne(event: "error", data: ["run failed"]), .error("run failed"))
    }

    func testDoneIgnoresEmptyData() {
        XCTAssertEqual(decodeOne(event: "done", data: []), .done)
        XCTAssertEqual(decodeOne(event: "done", data: [""]), .done)
    }

    func testMalformedToolCallJSONIsSkippedNotCrashed() {
        XCTAssertNil(decodeOne(event: "tool_call", data: ["not valid json"]))
    }

    func testMalformedUsageJSONIsSkippedNotCrashed() {
        XCTAssertNil(decodeOne(event: "usage", data: ["{\"PromptTokens\":\"oops\"}"]))
    }
}
