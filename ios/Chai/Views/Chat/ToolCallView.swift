import SwiftUI

struct ToolCallView: View {
    let toolCall: ToolCall
    @State private var isExpanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Button {
                withAnimation(.easeInOut(duration: 0.2)) {
                    isExpanded.toggle()
                }
            } label: {
                HStack {
                    Image(systemName: iconName)
                        .foregroundStyle(.secondary)
                    Text(toolCall.name)
                        .font(.caption)
                        .fontWeight(.medium)
                    Spacer()
                    Image(systemName: "chevron.right")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                        .rotationEffect(.degrees(isExpanded ? 90 : 0))
                }
            }
            .buttonStyle(.plain)

            if isExpanded {
                Text(formatInput())
                    .font(.caption)
                    .fontDesign(.monospaced)
                    .foregroundStyle(.secondary)
                    .padding(8)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Color(.systemGray6))
                    .clipShape(RoundedRectangle(cornerRadius: 8))
            }
        }
        .padding(8)
        .background(Color(.systemGray5).opacity(0.5))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }

    private var iconName: String {
        switch toolCall.name {
        case "Bash": return "terminal"
        case "Read", "Write", "Edit": return "doc.text"
        case "Glob", "Grep": return "magnifyingglass"
        default: return "wrench"
        }
    }

    private func formatInput() -> String {
        // Extract common tool parameters for display
        if let command = toolCall.input["command"]?.value as? String {
            return command
        }
        if let filePath = toolCall.input["file_path"]?.value as? String {
            return filePath
        }
        if let pattern = toolCall.input["pattern"]?.value as? String {
            return pattern
        }
        // Fallback to JSON
        if let data = try? JSONEncoder().encode(toolCall.input),
           let string = String(data: data, encoding: .utf8) {
            return string
        }
        return ""
    }
}
