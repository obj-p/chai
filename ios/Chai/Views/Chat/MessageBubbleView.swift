import SwiftUI

struct MessageBubbleView: View {
    let message: Message
    let isStreaming: Bool

    private var isUser: Bool { message.role == .user }

    var body: some View {
        HStack {
            if isUser { Spacer(minLength: 60) }

            VStack(alignment: isUser ? .trailing : .leading, spacing: 4) {
                // Message content
                HStack(alignment: .bottom, spacing: 4) {
                    if !isUser && isStreaming && message.content.isEmpty {
                        TypingIndicatorView()
                    } else {
                        Text(message.content)
                            .textSelection(.enabled)
                    }

                    if isStreaming && !message.content.isEmpty {
                        TypingIndicatorView()
                            .scaleEffect(0.6)
                    }
                }
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                .background(isUser ? Color.blue : Color(.systemGray5))
                .foregroundStyle(isUser ? .white : .primary)
                .clipShape(RoundedRectangle(cornerRadius: 18))

                // Tool calls (if any)
                if let toolCalls = message.toolCalls, !toolCalls.isEmpty {
                    ForEach(toolCalls) { toolCall in
                        ToolCallView(toolCall: toolCall)
                    }
                }
            }

            if !isUser { Spacer(minLength: 60) }
        }
    }
}
