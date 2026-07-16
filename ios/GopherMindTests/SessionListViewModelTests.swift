import XCTest
@testable import GopherMind

/// Exercises `SessionListViewModel` against a fake `SessionListServicing`
/// that returns canned data instead of touching the network —
/// deterministic and offline.
@MainActor
final class SessionListViewModelTests: XCTestCase {
    func testLoadSortsSessionsNewestFirst() async {
        let older = SessionInfo(id: "a", path: "/a", size: 10, modTime: Date(timeIntervalSince1970: 100), messages: 2, title: "Older")
        let newer = SessionInfo(id: "b", path: "/b", size: 20, modTime: Date(timeIntervalSince1970: 200), messages: 5, title: "Newer")
        let service = FakeSessionListService(sessions: [older, newer])
        let viewModel = SessionListViewModel(service: service)

        await viewModel.load()

        XCTAssertEqual(viewModel.sessions.map(\.id), ["b", "a"])
        XCTAssertNil(viewModel.errorMessage)
    }

    func testLoadFailureSurfacesErrorMessage() async {
        let service = FakeSessionListService(sessions: [])
        service.listError = FakeSessionListService.SimulatedError.boom
        let viewModel = SessionListViewModel(service: service)

        await viewModel.load()

        XCTAssertTrue(viewModel.sessions.isEmpty)
        XCTAssertNotNil(viewModel.errorMessage)
    }

    func testDeleteRemovesSessionOptimisticallyOnSuccess() async {
        let session = SessionInfo(id: "a", path: "/a", size: 10, modTime: Date(), messages: 1, title: "One")
        let service = FakeSessionListService(sessions: [session])
        let viewModel = SessionListViewModel(service: service)
        await viewModel.load()

        await viewModel.delete("a")

        XCTAssertTrue(viewModel.sessions.isEmpty)
        XCTAssertEqual(service.deleteCalls, ["a"])
        XCTAssertNil(viewModel.errorMessage)
    }

    func testDeleteFailureRestoresSessionAndSurfacesError() async {
        let session = SessionInfo(id: "a", path: "/a", size: 10, modTime: Date(), messages: 1, title: "One")
        let service = FakeSessionListService(sessions: [session])
        service.deleteError = FakeSessionListService.SimulatedError.boom
        let viewModel = SessionListViewModel(service: service)
        await viewModel.load()

        await viewModel.delete("a")

        XCTAssertEqual(viewModel.sessions.map(\.id), ["a"])
        XCTAssertNotNil(viewModel.errorMessage)
    }
}

@MainActor
private final class FakeSessionListService: SessionListServicing {
    enum SimulatedError: Error {
        case boom
    }

    private var sessions: [SessionInfo]
    var listError: Error?
    var deleteError: Error?
    private(set) var deleteCalls: [String] = []

    init(sessions: [SessionInfo]) {
        self.sessions = sessions
    }

    func listSessions() async throws -> [SessionInfo] {
        if let listError { throw listError }
        return sessions
    }

    func deleteSession(_ id: String) async throws {
        deleteCalls.append(id)
        if let deleteError { throw deleteError }
        sessions.removeAll { $0.id == id }
    }
}
