import XCTest
@testable import GopherMind

final class KeychainTests: XCTestCase {
    private let testKey = "unitTestKey"
    private lazy var keychain = Keychain(service: "com.jbrahy.gophermind.tests")

    override func tearDown() {
        keychain.delete(testKey)
        super.tearDown()
    }

    func testKeychainRoundTrip() {
        // The iOS Simulator test host has a real (if ephemeral) Keychain,
        // so this exercises the actual Security framework calls.
        XCTAssertTrue(keychain.set("s3cr3t-value", for: testKey))
        XCTAssertEqual(keychain.get(testKey), "s3cr3t-value")

        XCTAssertTrue(keychain.delete(testKey))
        XCTAssertNil(keychain.get(testKey))
    }

    func testAppSettingsPersistsNonSecretFieldsToUserDefaults() {
        // Fallback coverage in case the simulator's Keychain is ever
        // unavailable: verifies the UserDefaults-backed half of AppSettings
        // independent of Keychain access.
        let suiteName = "com.jbrahy.gophermind.tests.defaults"
        let defaults = UserDefaults(suiteName: suiteName)!
        defaults.removePersistentDomain(forName: suiteName)

        let settings = AppSettings(defaults: defaults, keychain: keychain)
        settings.serverURL = "https://example.tailnet.ts.net"
        settings.approvalTimeout = 45

        XCTAssertEqual(defaults.string(forKey: "serverURL"), "https://example.tailnet.ts.net")
        XCTAssertEqual(defaults.double(forKey: "approvalTimeout"), 45)

        defaults.removePersistentDomain(forName: suiteName)
    }
}
