import Foundation

/// A transferable connection config. Encoded into a `gophermind://setup?c=<b64>`
/// link (or bare base64 string) so the whole configuration — server URL, bearer
/// token, optional HMAC — moves to the app in one tap/scan/paste instead of being
/// hand-typed. Generate the link with `scripts/gophermind-setup-link` (server side).
struct PairingConfig: Codable, Equatable {
    let serverURL: String
    let bearerToken: String
    var hmacSecret: String?

    enum CodingKeys: String, CodingKey {
        case serverURL = "u"
        case bearerToken = "t"
        case hmacSecret = "h"
    }

    /// Accepts either a full `gophermind://setup?c=<base64url>` link or the bare
    /// base64url payload (for paste). Returns nil if it isn't a valid setup blob.
    static func parse(_ input: String) -> PairingConfig? {
        let trimmed = input.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }

        var blob = trimmed
        if let comps = URLComponents(string: trimmed), comps.scheme == "gophermind" {
            guard comps.host == "setup" || comps.path.contains("setup"),
                  let c = comps.queryItems?.first(where: { $0.name == "c" })?.value else { return nil }
            blob = c
        }
        guard let data = Data(base64URLEncoded: blob),
              let cfg = try? JSONDecoder().decode(PairingConfig.self, from: data) else { return nil }
        return cfg
    }

    /// The shareable setup link for this config.
    var link: String {
        guard let json = try? JSONEncoder().encode(self) else { return "" }
        return "gophermind://setup?c=\(json.base64URLEncodedString())"
    }
}

extension AppSettings {
    /// Import a paired config into the live settings (persists via each didSet).
    func apply(_ cfg: PairingConfig) {
        serverURL = cfg.serverURL.trimmingCharacters(in: .whitespacesAndNewlines)
        bearerToken = cfg.bearerToken
        hmacSecret = cfg.hmacSecret ?? ""
    }
}

extension Data {
    /// base64url (RFC 4648 §5): '+'→'-', '/'→'_', no padding.
    func base64URLEncodedString() -> String {
        base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }

    init?(base64URLEncoded s: String) {
        var b = s.replacingOccurrences(of: "-", with: "+")
                 .replacingOccurrences(of: "_", with: "/")
        while b.count % 4 != 0 { b.append("=") }
        self.init(base64Encoded: b)
    }
}
