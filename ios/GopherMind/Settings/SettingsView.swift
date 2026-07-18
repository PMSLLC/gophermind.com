import SwiftUI

struct SettingsView: View {
    @ObservedObject var settings: AppSettings

    var body: some View {
        Form {
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
