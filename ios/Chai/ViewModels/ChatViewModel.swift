import Foundation
import Observation

@Observable
@MainActor
final class ChatViewModel {
    // Session data
    let session: Session

    // Message state
    var messages: [Message] = []
    var streamingMessageId: String?

    // Streaming state
    var isStreaming = false
    var pendingPermission: PermissionRequest?

    // UI state
    var isLoading = false
    var errorMessage: String?
    var inputText = ""

    // Reconnection state
    private var currentPromptId: String?
    private var lastSequence: Int64 = 0
    private var streamingTask: Task<Void, Never>?

    // Dependencies
    private let client = APIClient()
    private let sseClient = SSEClient()

    private var baseURL: URL {
        let urlString = UserDefaults.standard.string(forKey: "serverURL") ?? Config.defaultServerURL
        return URL(string: urlString) ?? URL(string: Config.defaultServerURL)!
    }

    init(session: Session) {
        self.session = session
    }

    // MARK: - Public Methods

    func loadMessages() async {
        isLoading = true
        defer { isLoading = false }

        do {
            let response = try await client.getSession(baseURL: baseURL, id: session.id)
            messages = response.messages
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    func sendMessage() async {
        let text = inputText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, !isStreaming else { return }

        inputText = ""
        isStreaming = true

        // Add user message immediately (optimistic UI)
        let userMessage = Message(
            id: UUID().uuidString,
            sessionId: session.id,
            role: .user,
            content: text
        )
        messages.append(userMessage)

        // Create placeholder for assistant response
        let assistantId = UUID().uuidString
        streamingMessageId = assistantId
        let assistantMessage = Message(
            id: assistantId,
            sessionId: session.id,
            role: .assistant,
            content: "",
            isStreaming: true
        )
        messages.append(assistantMessage)

        do {
            DebugLogger.shared.log("Starting SSE stream to \(baseURL)")
            let stream = await sseClient.streamPrompt(
                baseURL: baseURL,
                sessionId: session.id,
                prompt: text
            )
            DebugLogger.shared.log("SSE stream created, starting iteration")

            var frameCount = 0
            for try await frame in stream {
                frameCount += 1
                DebugLogger.shared.log("Received frame #\(frameCount)")
                try Task.checkCancellation()
                await handleSSEFrame(frame)
                // Yield to allow UI to update
                await Task.yield()
            }

            // Stream completed normally
            DebugLogger.shared.log("SSE stream completed, received \(frameCount) frames")
            finalizeStreaming()
        } catch is CancellationError {
            // View disappeared - clean up silently
            finalizeStreaming()
        } catch let error as SSEClient.SSEError where error == .sessionBusy {
            errorMessage = "This session is busy. Please wait for the current response to complete."
            removeStreamingMessage()
        } catch {
            errorMessage = error.localizedDescription
            removeStreamingMessage()
        }

        isStreaming = false
        streamingMessageId = nil
        streamingTask = nil
    }

    func cancelStreaming() {
        streamingTask?.cancel()
        streamingTask = nil
    }

    func approvePermission(allow: Bool) async {
        guard let permission = pendingPermission else { return }

        pendingPermission = nil

        do {
            try await client.sendApproval(
                baseURL: baseURL,
                sessionId: session.id,
                toolUseId: permission.id,
                decision: allow ? "allow" : "deny"
            )
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    func handleAppBecameActive() async {
        guard let promptId = currentPromptId, isStreaming else { return }

        do {
            var hasMore = true
            while hasMore {
                let response = try await sseClient.catchUpEvents(
                    baseURL: baseURL,
                    sessionId: session.id,
                    promptId: promptId,
                    sinceSequence: lastSequence
                )

                for event in response.events {
                    processPersistedEvent(event)
                }

                lastSequence = response.lastSequence
                hasMore = response.hasMore

                if response.streamStatus != .streaming {
                    finalizeStreaming()
                    break
                }
            }
        } catch {
            // Catch-up failed silently - stream may still be active
        }
    }

    // MARK: - Private Methods

    private func handleSSEFrame(_ frame: SSEFrame) async {
        DebugLogger.shared.log("handleSSEFrame: event=\(frame.event), dataLen=\(frame.data.count)")

        switch frame.event {
        case "connected":
            if let data = frame.data.data(using: .utf8),
               let json = try? JSONSerialization.jsonObject(with: data) as? [String: String],
               let promptId = json["prompt_id"] {
                currentPromptId = promptId
                lastSequence = 0
            }

        case "claude":
            DebugLogger.shared.log("claude event data: \(frame.data.prefix(200))")
            if let data = frame.data.data(using: .utf8) {
                handleClaudeEvent(data)
                lastSequence += 1
            } else {
                DebugLogger.shared.log("Failed to convert claude data to UTF8")
            }

        case "error":
            if let data = frame.data.data(using: .utf8),
               let json = try? JSONSerialization.jsonObject(with: data) as? [String: String],
               let error = json["error"] {
                errorMessage = error
            }

        case "done":
            finalizeStreaming()

        default:
            break
        }
    }

    private func handleClaudeEvent(_ data: Data) {
        guard let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let type = json["type"] as? String else { return }

        DebugLogger.shared.log("handleClaudeEvent: type=\(type)")

        switch type {
        case "content_block_delta":
            // Streaming text deltas
            if let delta = json["delta"] as? [String: Any],
               let text = delta["text"] as? String,
               let idx = messages.firstIndex(where: { $0.id == streamingMessageId }) {
                var updated = messages  // Copy entire array
                updated[idx].content += text
                messages = updated  // Assign new array
                DebugLogger.shared.log("content_block_delta: text='\(text.prefix(30))', total=\(messages[idx].content.count)")
            }

        case "assistant":
            // Full assistant message with content array
            DebugLogger.shared.log("handleClaudeEvent: type=assistant")
            if let message = json["message"] as? [String: Any],
               let contentArray = message["content"] as? [[String: Any]],
               let idx = messages.firstIndex(where: { $0.id == streamingMessageId }) {
                // Extract text from content blocks
                var text = ""
                for block in contentArray {
                    if let blockType = block["type"] as? String, blockType == "text",
                       let blockText = block["text"] as? String {
                        text += blockText
                    }
                }
                DebugLogger.shared.log("assistant: extracted text='\(text.prefix(50))...'")
                if !text.isEmpty {
                    DebugLogger.shared.log("Before: messages[\(idx)].content.count=\(messages[idx].content.count)")
                    var updated = messages  // Copy entire array
                    updated[idx].content = text
                    messages = updated  // Assign new array
                    DebugLogger.shared.log("After: messages[\(idx)].content.count=\(messages[idx].content.count)")
                }
            }

        case "permission_request":
            if let toolUseId = json["tool_use_id"] as? String,
               let toolName = json["tool_name"] as? String,
               let input = json["input"] as? [String: Any] {
                pendingPermission = PermissionRequest(
                    id: toolUseId,
                    toolName: toolName,
                    input: input
                )
            }

        case "result":
            // Result event received - done event follows
            break

        default:
            break
        }
    }

    private func processPersistedEvent(_ event: SessionEvent) {
        switch event.eventType {
        case "claude":
            if let data = event.data.data(using: .utf8) {
                handleClaudeEvent(data)
            }
        case "done":
            finalizeStreaming()
        default:
            break
        }
    }

    private func finalizeStreaming() {
        DebugLogger.shared.log("finalizeStreaming called")
        if let idx = messages.firstIndex(where: { $0.id == streamingMessageId }) {
            var updated = messages
            updated[idx].isStreaming = false
            messages = updated
            DebugLogger.shared.log("finalizeStreaming: set isStreaming=false for message \(idx)")
        }
        isStreaming = false
        streamingMessageId = nil
        currentPromptId = nil
    }

    private func removeStreamingMessage() {
        if let idx = messages.firstIndex(where: { $0.id == streamingMessageId }) {
            messages.remove(at: idx)
        }
        // Keep the user message - server saved it
    }
}
