import SwiftUI

struct ChatView: View {
    @State private var viewModel: ChatViewModel
    @Environment(\.scenePhase) private var scenePhase

    init(session: Session) {
        _viewModel = State(wrappedValue: ChatViewModel(session: session))
    }

    var body: some View {
        ChatContentView(viewModel: viewModel)
            .navigationTitle(viewModel.session.title ?? "Chat")
            .navigationBarTitleDisplayMode(.inline)
            .task {
                await viewModel.loadMessages()
            }
            .onChange(of: scenePhase) { _, newPhase in
                if newPhase == .active {
                    Task { await viewModel.handleAppBecameActive() }
                }
            }
            .onDisappear {
                viewModel.cancelStreaming()
            }
    }
}
