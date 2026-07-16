import Foundation
import CryptoKit

/// Computes the `X-Hub-Signature-256` value described in
/// `docs/mobile-serve.md`: HMAC-SHA256 of the raw request body under the
/// shared `GOPHERMIND_SERVE_HMAC_SECRET`, as lowercase hex.
enum HMACSigner {
    static func hexDigest(body: Data, secret: String) -> String {
        let key = SymmetricKey(data: Data(secret.utf8))
        let mac = HMAC<SHA256>.authenticationCode(for: body, using: key)
        return mac.map { String(format: "%02x", $0) }.joined()
    }
}
