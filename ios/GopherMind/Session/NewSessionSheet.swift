import SwiftUI

/// Sheet presented from the session list's "New Session" toolbar button:
/// lets the user pick a model (from `GET /models`) and a mode (from `GET
/// /modes`) before the session is created, then hands the newly created
/// session's id to `onCreated` so the caller can navigate straight into its
/// conversation.
struct NewSessionSheet: View {
    let service: GopherMindServicing
    var onCreated: (String) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var models: [String] = []
    @State private var selectedModel: String?
    @State private var isLoadingModels = true
    @State private var modelsErrorMessage: String?
    @State private var modes: [Mode] = []
    @State private var selectedMode: String = "coding"
    @State private var isLoadingModes = true
    @State private var modesErrorMessage: String?
    @State private var isCreating = false
    @State private var createErrorMessage: String?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    if isLoadingModels {
                        ProgressView()
                    } else if !models.isEmpty {
                        Picker("Model", selection: $selectedModel) {
                            ForEach(models, id: \.self) { model in
                                Text(model).tag(Optional(model))
                            }
                        }
                    } else {
                        // No models to choose from (endpoint unreachable or
                        // empty list) — Create still works, using whatever
                        // model the server defaults to.
                        Text(modelsErrorMessage ?? "No models available. Create will use the server's default model.")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                } header: {
                    Text("Model")
                }

                Section {
                    if isLoadingModes {
                        ProgressView()
                    } else if !modes.isEmpty {
                        Picker("Mode", selection: $selectedMode) {
                            ForEach(modes) { mode in
                                Text(mode.label).tag(mode.id)
                            }
                        }
                    } else {
                        // No modes to choose from (endpoint unreachable) —
                        // Create still works, using the default coding mode.
                        Text(modesErrorMessage ?? "No modes available. Create will use the default coding mode.")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                } header: {
                    Text("Mode")
                }

                if let createErrorMessage {
                    Section {
                        Text(createErrorMessage)
                            .font(.footnote)
                            .foregroundStyle(.red)
                    }
                }
            }
            .navigationTitle("New Session")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Create") { create() }
                        .disabled(isCreating)
                }
            }
            .task {
                await loadModels()
                await loadModes()
            }
        }
    }

    private func loadModels() async {
        isLoadingModels = true
        defer { isLoadingModels = false }
        do {
            let list = try await service.listModels()
            models = list
            selectedModel = list.first
        } catch {
            modelsErrorMessage = "Couldn't load models: \(error.localizedDescription)"
        }
    }

    private func loadModes() async {
        isLoadingModes = true
        defer { isLoadingModes = false }
        do {
            let list = try await service.getModes()
            modes = list
            if let first = list.first, !list.contains(where: { $0.id == selectedMode }) {
                selectedMode = first.id
            }
        } catch {
            modesErrorMessage = "Couldn't load modes: \(error.localizedDescription)"
        }
    }

    private func create() {
        guard !isCreating else { return }
        isCreating = true
        createErrorMessage = nil
        Task {
            defer { isCreating = false }
            do {
                let id = try await service.createSession(id: nil, model: selectedModel, mode: selectedMode)
                dismiss()
                onCreated(id)
            } catch {
                createErrorMessage = "Couldn't create session: \(error.localizedDescription)"
            }
        }
    }
}

#Preview {
    NewSessionSheet(service: GopherMindService(settings: AppSettings()), onCreated: { _ in })
}
