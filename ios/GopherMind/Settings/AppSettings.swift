import Foundation
import Combine

/// Holds app-wide connection settings. Non-secret values persist to
/// UserDefaults; secrets (bearer token, HMAC secret) persist to the Keychain.
final class AppSettings: ObservableObject {
    private enum DefaultsKey {
        static let serverURL = "serverURL"
        static let approvalTimeout = "approvalTimeout"
    }

    private enum KeychainKey {
        static let bearerToken = "bearerToken"
        static let hmacSecret = "hmacSecret"
    }

    private let defaults: UserDefaults
    private let keychain: Keychain

    @Published var serverURL: String {
        didSet { defaults.set(serverURL, forKey: DefaultsKey.serverURL) }
    }

    @Published var approvalTimeout: Double {
        didSet { defaults.set(approvalTimeout, forKey: DefaultsKey.approvalTimeout) }
    }

    @Published var bearerToken: String {
        didSet { keychain.set(bearerToken, for: KeychainKey.bearerToken) }
    }

    @Published var hmacSecret: String {
        didSet { keychain.set(hmacSecret, for: KeychainKey.hmacSecret) }
    }

    init(defaults: UserDefaults = .standard, keychain: Keychain = Keychain()) {
        self.defaults = defaults
        self.keychain = keychain

        self.serverURL = defaults.string(forKey: DefaultsKey.serverURL) ?? ""
        self.approvalTimeout = defaults.object(forKey: DefaultsKey.approvalTimeout) as? Double ?? 30
        self.bearerToken = keychain.get(KeychainKey.bearerToken) ?? ""
        self.hmacSecret = keychain.get(KeychainKey.hmacSecret) ?? ""
    }
}
