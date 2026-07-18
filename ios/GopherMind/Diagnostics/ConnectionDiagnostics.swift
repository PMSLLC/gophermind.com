import Foundation

/// One line of the connection diagnostic report.
struct DiagStep: Identifiable {
    enum Status: String { case ok, warn, fail, info }
    let id = UUID()
    let title: String
    let status: Status
    let detail: String
}

/// Runs a sequence of low-level checks against the configured server and reports
/// exactly WHERE a failure is: config, network reachability, auth, or response
/// decoding. Deliberately does NOT go through the typed service layer — it uses
/// raw URLSession so it can tell a "can't connect" apart from a "got 200 but the
/// app couldn't parse it" (the class of bug that produced a generic ServiceError).
@MainActor
final class ConnectionDiagnostics: ObservableObject {
    @Published private(set) var steps: [DiagStep] = []
    @Published private(set) var running = false

    private let settings: AppSettings
    init(settings: AppSettings) { self.settings = settings }

    /// A plain-text version of the results, for the "Copy report" button.
    var report: String {
        steps.map { "[\($0.status.rawValue.uppercased())] \($0.title)\n\($0.detail)" }
            .joined(separator: "\n\n")
    }

    func run() async {
        running = true
        steps = []
        defer { running = false }

        // 1) Configuration ---------------------------------------------------
        let raw = settings.serverURL.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !raw.isEmpty else {
            add("Configuration", .fail,
                "Server URL is empty. Set it in Settings, e.g. http://10.0.0.5:8090")
            return
        }
        guard let base = URL(string: raw),
              let scheme = base.scheme?.lowercased(), scheme == "http" || scheme == "https",
              let host = base.host, !host.isEmpty else {
            add("Configuration", .fail,
                "Server URL isn't valid: \"\(raw)\".\nIt needs scheme + host, e.g. http://10.0.0.5:8090")
            return
        }
        let portStr = base.port.map { ":\($0)" } ?? ""
        let tokenSet = !settings.bearerToken.isEmpty
        add("Configuration", tokenSet ? .ok : .warn, """
            Base URL: \(scheme)://\(host)\(portStr)
            Bearer token: \(tokenSet ? "set (\(settings.bearerToken.count) chars)" : "MISSING — set it in Settings")
            HMAC: \(settings.hmacSecret.isEmpty ? "off" : "on")
            """)

        // 2) Reachability: GET /healthz (no auth) ----------------------------
        let (hc, _, herr) = await perform(URLRequest(url: base.appendingPathComponent("healthz")))
        if let herr {
            add("Reachability (/healthz)", .fail, """
                Couldn't reach the server: \(herr)
                Check: are you on the VPN/network that routes to \(host)? Is the URL + port right? Is the server running?
                """)
            return
        }
        add("Reachability (/healthz)", hc == 200 ? .ok : .warn,
            "Server responded (HTTP \(hc.map(String.init) ?? "?")). The network path is good.")

        // 3) Auth is enforced: GET /session WITHOUT a token → expect 401 -----
        let (nc, _, nerr) = await perform(URLRequest(url: base.appendingPathComponent("session")))
        if nerr == nil {
            add("Auth enforced", nc == 401 ? .ok : .warn,
                nc == 401
                ? "Unauthenticated request correctly rejected (401)."
                : "Expected 401 without a token but got HTTP \(nc.map(String.init) ?? "?"). Is this really a gophermind server?")
        }

        // 4) Authenticated: GET /session WITH bearer(+HMAC) → expect 200 -----
        let builder = RequestBuilder(baseURL: base,
                                     bearerToken: settings.bearerToken,
                                     hmacSecret: settings.hmacSecret.isEmpty ? nil : settings.hmacSecret)
        let (ac, adata, aerr) = await perform(builder.build(path: "session", method: "GET"))
        if let aerr { add("Authenticated (/session)", .fail, "Request failed: \(aerr)"); return }
        switch ac {
        case 200:
            add("Authenticated (/session)", .ok, "HTTP 200 — token accepted (\(adata?.count ?? 0) bytes).")
        case 401:
            add("Authenticated (/session)", .fail,
                "401 Unauthorized — the bearer token (or HMAC secret) is wrong. Re-check Settings.")
            return
        default:
            add("Authenticated (/session)", .fail,
                "HTTP \(ac.map(String.init) ?? "?") (expected 200).\nBody: \(preview(adata))")
            return
        }

        // 5) Decode with the REAL app decoder --------------------------------
        guard let data = adata else { return }
        do {
            let sessions = try APIClient.decoder.decode([SessionInfo].self, from: data)
            add("Decode response", .ok,
                "Parsed \(sessions.count) session(s) successfully — the app can display these. ✅ Connection fully working.")
        } catch {
            add("Decode response", .fail, """
                Server returned 200 but the app couldn't parse the response — this is a CLIENT bug, not a connection problem.
                Error: \(error)
                Raw (first 300 chars): \(preview(data))
                """)
        }
    }

    // MARK: - Helpers

    private func add(_ title: String, _ status: DiagStep.Status, _ detail: String) {
        steps.append(DiagStep(title: title, status: status, detail: detail))
    }

    private func perform(_ request: URLRequest, timeout: TimeInterval = 8) async -> (Int?, Data?, String?) {
        var req = request
        req.timeoutInterval = timeout
        do {
            let (data, resp) = try await URLSession.shared.data(for: req)
            return ((resp as? HTTPURLResponse)?.statusCode, data, nil)
        } catch {
            return (nil, nil, Self.humanError(error))
        }
    }

    private func preview(_ data: Data?) -> String {
        guard let data, let s = String(data: data, encoding: .utf8) else { return "<none>" }
        return s.count > 300 ? String(s.prefix(300)) + "…" : s
    }

    private static func humanError(_ error: Error) -> String {
        if let ue = error as? URLError {
            switch ue.code {
            case .cannotConnectToHost: return "cannot connect to host (port closed / server down / firewall)"
            case .cannotFindHost, .dnsLookupFailed: return "cannot resolve host (DNS / wrong hostname)"
            case .timedOut: return "timed out — unreachable (wrong network/VPN, or blocked by a firewall)"
            case .notConnectedToInternet: return "no internet connection"
            case .appTransportSecurityRequiresSecureConnection, .secureConnectionFailed:
                return "blocked by App Transport Security (plain http not allowed to this host)"
            default: return "\(ue.code) — \(ue.localizedDescription)"
            }
        }
        let ns = error as NSError
        return "\(ns.domain) \(ns.code): \(ns.localizedDescription)"
    }
}
