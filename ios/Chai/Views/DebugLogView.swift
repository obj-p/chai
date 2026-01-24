import SwiftUI

struct DebugLogView: View {
    @State private var logs: [String] = []
    @State private var showCopied = false

    var body: some View {
        List(logs.reversed(), id: \.self) { log in
            Text(log)
                .font(.caption.monospaced())
        }
        .navigationTitle("Debug Logs")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button {
                    UIPasteboard.general.string = logs.joined(separator: "\n")
                    showCopied = true
                    Task {
                        try? await Task.sleep(for: .seconds(2))
                        showCopied = false
                    }
                } label: {
                    Label(showCopied ? "Copied!" : "Copy All", systemImage: showCopied ? "checkmark" : "doc.on.doc")
                }
            }
            ToolbarItem(placement: .secondaryAction) {
                Button("Clear", role: .destructive) {
                    DebugLogger.shared.clear()
                    logs = []
                }
            }
        }
        .onAppear {
            logs = DebugLogger.shared.getAll()
        }
        .refreshable {
            logs = DebugLogger.shared.getAll()
        }
    }
}
