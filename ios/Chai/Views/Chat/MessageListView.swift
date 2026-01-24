import SwiftUI

struct MessageListView: View {
    var viewModel: ChatViewModel

    var body: some View {
        let _ = DebugLogger.shared.log("MessageListView body, count=\(viewModel.messages.count), lastContent=\(viewModel.messages.last?.content.count ?? 0)")
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(spacing: 12) {
                    ForEach(viewModel.messages) { message in
                        MessageBubbleView(
                            message: message,
                            isStreaming: message.id == viewModel.streamingMessageId
                        )
                        .id("\(message.id)-\(message.content.count)")
                    }
                }
                .padding()
            }
            .onChange(of: viewModel.messages.count) { _, _ in
                scrollToBottom(proxy: proxy)
            }
            .onChange(of: viewModel.messages.last?.content.count) { _, _ in
                if viewModel.streamingMessageId != nil {
                    scrollToBottom(proxy: proxy)
                }
            }
            .onAppear {
                scrollToBottom(proxy: proxy)
            }
        }
    }

    private func scrollToBottom(proxy: ScrollViewProxy) {
        if let last = viewModel.messages.last {
            withAnimation(.easeOut(duration: 0.2)) {
                proxy.scrollTo("\(last.id)-\(last.content.count)", anchor: .bottom)
            }
        }
    }
}
