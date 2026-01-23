import SwiftUI

struct PermissionSheet: View {
    let permission: PermissionRequest
    let onDecision: (Bool) -> Void
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            VStack(spacing: 20) {
                Image(systemName: iconName)
                    .font(.system(size: 48))
                    .foregroundStyle(.orange)

                Text("Permission Request")
                    .font(.headline)

                Text("Claude wants to use: **\(permission.toolName)**")

                // Show input details
                ScrollView {
                    Text(formatInput())
                        .font(.caption)
                        .fontDesign(.monospaced)
                        .padding()
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(Color(.systemGray6))
                        .clipShape(RoundedRectangle(cornerRadius: 8))
                }
                .frame(maxHeight: 200)

                Spacer()

                HStack(spacing: 16) {
                    Button("Deny") {
                        onDecision(false)
                        dismiss()
                    }
                    .buttonStyle(.bordered)

                    Button("Allow") {
                        onDecision(true)
                        dismiss()
                    }
                    .buttonStyle(.borderedProminent)
                }
            }
            .padding()
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") {
                        onDecision(false)
                        dismiss()
                    }
                }
            }
        }
        .presentationDetents([.medium])
    }

    private var iconName: String {
        switch permission.toolName {
        case "Bash": return "terminal"
        case "Read", "Write", "Edit": return "doc.text"
        default: return "exclamationmark.shield"
        }
    }

    private func formatInput() -> String {
        if let command = permission.input["command"] as? String {
            return command
        }
        if let filePath = permission.input["file_path"] as? String {
            return filePath
        }
        if let data = try? JSONSerialization.data(withJSONObject: permission.input, options: .prettyPrinted),
           let string = String(data: data, encoding: .utf8) {
            return string
        }
        return ""
    }
}
