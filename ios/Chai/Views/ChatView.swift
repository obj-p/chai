import SwiftUI

struct ChatView: View {
    let session: Session
    @AppStorage("serverURL") private var serverURL = Config.defaultServerURL
    @State private var viewModel: ChatViewModel?
    @Environment(\.scenePhase) private var scenePhase

    var body: some View {
        Group {
            if let viewModel {
                ChatContentView(viewModel: viewModel)
            } else {
                ProgressView()
            }
        }
        .navigationTitle(session.title ?? "Chat")
        .navigationBarTitleDisplayMode(.inline)
        .task {
            guard let baseURL = URL(string: serverURL) else { return }
            let vm = ChatViewModel(session: session, baseURL: baseURL)
            viewModel = vm
            await vm.loadMessages()
        }
        .onChange(of: scenePhase) { _, newPhase in
            if newPhase == .active {
                Task { await viewModel?.handleAppBecameActive() }
            }
        }
    }
}
