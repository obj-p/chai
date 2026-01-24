import SwiftUI

struct ChatContentView: View {
    @Bindable var viewModel: ChatViewModel

    var body: some View {
        VStack(spacing: 0) {
            MessageListView(viewModel: viewModel)

            Divider()

            ChatInputBar(
                text: $viewModel.inputText,
                isStreaming: viewModel.isStreaming,
                onSend: { Task { await viewModel.sendMessage() } }
            )
        }
        .alert("Error", isPresented: .init(
            get: { viewModel.errorMessage != nil },
            set: { if !$0 { viewModel.errorMessage = nil } }
        )) {
            Button("OK") { viewModel.errorMessage = nil }
        } message: {
            Text(viewModel.errorMessage ?? "")
        }
        .sheet(item: Binding(
            get: { viewModel.pendingPermission },
            set: { viewModel.pendingPermission = $0 }
        )) { permission in
            PermissionSheet(
                permission: permission,
                onDecision: { allow in
                    Task { await viewModel.approvePermission(allow: allow, permission: permission) }
                }
            )
        }
        .onChange(of: viewModel.pendingPermission?.id) { oldValue, newValue in
            DebugLogger.shared.log("pendingPermission changed: \(oldValue ?? "nil") -> \(newValue ?? "nil")")
        }
    }
}
