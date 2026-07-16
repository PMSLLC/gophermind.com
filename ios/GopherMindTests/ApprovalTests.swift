import XCTest
@testable import GopherMind

/// Exercises `SessionViewModel.decide` against a fake `GopherMindServicing`
/// that captures `approve` calls instead of touching the network —
/// deterministic and offline.
@MainActor
final class ApprovalTests: XCTestCase {
    private func makeViewModel(pending: [(id: String, tool: String, args: String)], service: FakeGopherMindService) -> SessionViewModel {
        var items: [ConversationItem] = []
        for approval in pending {
            items = SessionViewModel.reduce(items, .approvalNeeded(approvalID: approval.id, tool: approval.tool, args: approval.args))
        }
        return SessionViewModel(service: service, sessionID: "sess-1", items: items)
    }

    func testDecideApprovedMarksItemDecidedAndRecordsApproveCall() async {
        let service = FakeGopherMindService()
        let viewModel = makeViewModel(pending: [(id: "ap-1", tool: "shell", args: "rm -rf /tmp")], service: service)

        await viewModel.decide(approvalID: "ap-1", approved: true)?.value

        XCTAssertEqual(viewModel.items.count, 1)
        XCTAssertEqual(viewModel.items[0].kind, .approvalDecided(approvalID: "ap-1", tool: "shell", args: "rm -rf /tmp", approved: true))
        XCTAssertEqual(service.approveCalls, [.init(sessionID: "sess-1", approvalID: "ap-1", approved: true)])
    }

    func testDecideDeniedMarksItemDecidedAndRecordsApproveCall() async {
        let service = FakeGopherMindService()
        let viewModel = makeViewModel(pending: [(id: "ap-1", tool: "shell", args: "rm -rf /tmp")], service: service)

        await viewModel.decide(approvalID: "ap-1", approved: false)?.value

        XCTAssertEqual(viewModel.items.count, 1)
        XCTAssertEqual(viewModel.items[0].kind, .approvalDecided(approvalID: "ap-1", tool: "shell", args: "rm -rf /tmp", approved: false))
        XCTAssertEqual(service.approveCalls, [.init(sessionID: "sess-1", approvalID: "ap-1", approved: false)])
    }

    func testDoubleDecideIsANoOpAndServiceCalledOnce() async {
        let service = FakeGopherMindService()
        let viewModel = makeViewModel(pending: [(id: "ap-1", tool: "shell", args: "rm -rf /tmp")], service: service)

        await viewModel.decide(approvalID: "ap-1", approved: true)?.value
        let secondTask = viewModel.decide(approvalID: "ap-1", approved: false)
        await secondTask?.value

        XCTAssertNil(secondTask)
        XCTAssertEqual(viewModel.items[0].kind, .approvalDecided(approvalID: "ap-1", tool: "shell", args: "rm -rf /tmp", approved: true))
        XCTAssertEqual(service.approveCalls.count, 1)
    }

    func testTurnEndExpiresStillPendingApproval() {
        var items: [ConversationItem] = []
        items = SessionViewModel.reduce(items, .approvalNeeded(approvalID: "ap-1", tool: "shell", args: "rm -rf /tmp"))
        items = SessionViewModel.reduce(items, .done)

        XCTAssertEqual(items.count, 1)
        XCTAssertEqual(items[0].kind, .approvalExpired(approvalID: "ap-1", tool: "shell", args: "rm -rf /tmp"))
    }

    func testTurnEndOnErrorAlsoExpiresStillPendingApproval() {
        var items: [ConversationItem] = []
        items = SessionViewModel.reduce(items, .approvalNeeded(approvalID: "ap-1", tool: "shell", args: "rm -rf /tmp"))
        items = SessionViewModel.reduce(items, .error("boom"))

        XCTAssertEqual(items.count, 2)
        XCTAssertEqual(items[0].kind, .approvalExpired(approvalID: "ap-1", tool: "shell", args: "rm -rf /tmp"))
        XCTAssertEqual(items[1].kind, .errorLine("boom"))
    }

    func testApproveServiceErrorSurfacesErrorLineWithoutCrashing() async {
        let service = FakeGopherMindService()
        service.approveError = FakeGopherMindService.SimulatedError.boom
        let viewModel = makeViewModel(pending: [(id: "ap-1", tool: "shell", args: "rm -rf /tmp")], service: service)

        await viewModel.decide(approvalID: "ap-1", approved: true)?.value

        XCTAssertEqual(viewModel.items.count, 2)
        XCTAssertEqual(viewModel.items[0].kind, .approvalDecided(approvalID: "ap-1", tool: "shell", args: "rm -rf /tmp", approved: true))
        guard case .errorLine = viewModel.items[1].kind else {
            return XCTFail("expected an error line after a failed approve, got \(viewModel.items[1].kind)")
        }
    }
}

/// Records `approve` calls instead of making network requests; `stream`
/// yields no events (unused by these tests).
@MainActor
private final class FakeGopherMindService: GopherMindServicing {
    struct ApproveCall: Equatable {
        let sessionID: String
        let approvalID: String
        let approved: Bool
    }

    enum SimulatedError: Error {
        case boom
    }

    private(set) var approveCalls: [ApproveCall] = []
    var approveError: Error?

    func createSession(id: String?) async throws -> String {
        id ?? "generated-session"
    }

    func approve(sessionID: String, approvalID: String, approved: Bool) async throws {
        approveCalls.append(ApproveCall(sessionID: sessionID, approvalID: approvalID, approved: approved))
        if let approveError {
            throw approveError
        }
    }

    func stream(sessionID: String, task: String) throws -> AsyncThrowingStream<AgentEvent, Error> {
        AsyncThrowingStream { continuation in
            continuation.finish()
        }
    }
}
