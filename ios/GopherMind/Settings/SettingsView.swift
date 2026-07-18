import SwiftUI

struct SettingsView: View {
    @ObservedObject var settings: AppSettings
    @State private var setupLink = ""
    @State private var setupMessage: String?

    var body: some View {
        Form {
            Section {
                TextField("Paste setup link", text: $setupLink)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                Button("Apply setup link") {
                    if let cfg = PairingConfig.parse(setupLink) {
                        settings.apply(cfg)
                        setupMessage = "Applied — connected to \(URL(string: cfg.serverURL)?.host ?? cfg.serverURL)."
                        setupLink = ""
                    } else {
                        setupMessage = "That doesn't look like a valid setup link."
                    }
                }
                .disabled(setupLink.trimmingCharacters(in: .whitespaces).isEmpty)
                if let setupMessage {
                    Text(setupMessage).font(.footnote).foregroundStyle(.secondary)
                }
            } header: {
                Text("Quick Setup")
            } footer: {
                Text("Scan the setup QR (or paste its link) to fill in the server, token, and HMAC automatically.")
            }

            Section("Server") {
                TextField("Server URL", text: $settings.serverURL)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .keyboardType(.URL)

                SecureField("Bearer Token", text: $settings.bearerToken)

                SecureField("HMAC Secret (optional)", text: $settings.hmacSecret)
            }

            Section("Approvals") {
                Stepper(
                    "Approval Timeout: \(Int(settings.approvalTimeout))s",
                    value: $settings.approvalTimeout,
                    in: 5...300,
                    step: 5
                )
            }

            Section("Diagnostics") {
                NavigationLink("Connection Debug") {
                    ConnectionDebugView(settings: settings)
                }
            }
        }
        .navigationTitle("Settings")
    }
}

#Preview {
    NavigationStack {
        SettingsView(settings: AppSettings())
    }
}
