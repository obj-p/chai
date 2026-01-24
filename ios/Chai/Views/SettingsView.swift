import SwiftUI

struct SettingsView: View {
    @AppStorage("serverURL") private var serverURL = Config.defaultServerURL
    @Environment(\.dismiss) private var dismiss

    private var isValidURL: Bool {
        URL(string: serverURL) != nil && !serverURL.isEmpty
    }

    private var isInsecureURL: Bool {
        guard let url = URL(string: serverURL),
              let scheme = url.scheme,
              let host = url.host() else { return false }
        return scheme == "http" && host != "localhost" && host != "127.0.0.1"
    }

    var body: some View {
        NavigationStack {
            Form {
                Section("Server") {
                    TextField("URL", text: $serverURL)
                        .textContentType(.URL)
                        .textInputAutocapitalization(.never)
                        .keyboardType(.URL)

                    if !isValidURL {
                        Text("Enter a valid URL")
                            .font(.caption)
                            .foregroundStyle(.red)
                    } else if isInsecureURL {
                        Text("HTTP connections are insecure. Use HTTPS for production.")
                            .font(.caption)
                            .foregroundStyle(.orange)
                    }
                }

                Section("Debug") {
                    NavigationLink("View Logs") {
                        DebugLogView()
                    }
                }
            }
            .navigationTitle("Settings")
            .toolbar {
                Button("Done") { dismiss() }
            }
        }
    }
}
