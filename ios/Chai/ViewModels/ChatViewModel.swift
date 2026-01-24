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
            let stream = await sseClient.streamPrompt(
                baseURL: baseURL,
                sessionId: session.id,
                prompt: text
            )

            for try await frame in stream {
                try Task.checkCancellation()
                await handleSSEFrame(frame)
                // Yield to allow UI to update
                await Task.yield()
            }

            // Stream completed normally
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

    func approvePermission(allow: Bool, permission: PermissionRequest) async {
        DebugLogger.shared.log("approvePermission called: allow=\(allow), permission.id=\(permission.id)")

        // Clear pending permission if it matches
        if pendingPermission?.id == permission.id {
            pendingPermission = nil
        }

        do {
            DebugLogger.shared.log("Sending approval: toolUseId=\(permission.id), decision=\(allow ? "allow" : "deny")")
            try await client.sendApproval(
                baseURL: baseURL,
                sessionId: session.id,
                toolUseId: permission.id,
                decision: allow ? "allow" : "deny"
            )
            DebugLogger.shared.log("Approval sent successfully")
        } catch {
            DebugLogger.shared.log("Approval failed: \(error.localizedDescription)")
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
        DebugLogger.shared.log("RAW SSE frame: event='\(frame.event)' data='\(frame.data.prefix(300))'")

        switch frame.event {
        case "connected":
            if let data = frame.data.data(using: .utf8),
               let json = try? JSONSerialization.jsonObject(with: data) as? [String: String],
               let promptId = json["prompt_id"] {
                currentPromptId = promptId
                lastSequence = 0
            }

        case "claude":
            if let data = frame.data.data(using: .utf8) {
                handleClaudeEvent(data)
                lastSequence += 1
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
              let type = json["type"] as? String else {
            DebugLogger.shared.log("handleClaudeEvent: failed to parse JSON or no type field")
            return
        }

        DebugLogger.shared.log("claude event type: \(type)")

        switch type {
        case "content_block_delta":
            // Streaming text deltas
            if let delta = json["delta"] as? [String: Any],
               let text = delta["text"] as? String,
               let idx = messages.firstIndex(where: { $0.id == streamingMessageId }) {
                var updated = messages  // Copy entire array
                updated[idx].content += text
                messages = updated  // Assign new array
            }

        case "assistant":
            // Full assistant message with content array
            if let message = json["message"] as? [String: Any],
               let contentArray = message["content"] as? [[String: Any]],
               let idx = messages.firstIndex(where: { $0.id == streamingMessageId }) {
                // Extract text from content blocks
                var text = ""
                for block in contentArray {
                    if let blockType = block["type"] as? String {
                        if blockType == "text", let blockText = block["text"] as? String {
                            text += blockText
                        }
                        // Note: tool_use blocks are shown here but permission is requested via control_request
                    }
                }
                if !text.isEmpty {
                    var updated = messages  // Copy entire array
                    updated[idx].content = text
                    messages = updated  // Assign new array
                }
            }

        case "control_request":
            // Claude CLI's permission request format
            DebugLogger.shared.log("control_request received: \(String(data: data, encoding: .utf8)?.prefix(500) ?? "nil")")
            if let requestId = json["request_id"] as? String,
               let request = json["request"] as? [String: Any],
               let subtype = request["subtype"] as? String,
               subtype == "can_use_tool",
               let toolName = request["tool_name"] as? String,
               let input = request["input"] as? [String: Any] {
                DebugLogger.shared.log("Creating PermissionRequest from control_request: id=\(requestId), tool=\(toolName)")
                pendingPermission = PermissionRequest(
                    id: requestId,
                    toolName: toolName,
                    input: input
                )
            }

        case "permission_request", "tool_use":
            DebugLogger.shared.log("\(type) received: \(String(data: data, encoding: .utf8) ?? "nil")")
            if let toolUseId = json["tool_use_id"] as? String,
               let toolName = json["tool_name"] as? String,
               let input = json["input"] as? [String: Any] {
                DebugLogger.shared.log("Creating PermissionRequest: id=\(toolUseId), tool=\(toolName)")
                pendingPermission = PermissionRequest(
                    id: toolUseId,
                    toolName: toolName,
                    input: input
                )
                DebugLogger.shared.log("pendingPermission set: \(pendingPermission != nil)")
            } else {
                DebugLogger.shared.log("\(type) parsing failed - keys: \(Array(json.keys))")
            }

        case "result":
            // Result event received - done event follows
            break

        default:
            // Log unhandled event types to see what we're missing
            DebugLogger.shared.log("unhandled event type '\(type)': \(String(data: data, encoding: .utf8)?.prefix(200) ?? "nil")")
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
        if let idx = messages.firstIndex(where: { $0.id == streamingMessageId }) {
            var updated = messages
            updated[idx].isStreaming = false
            messages = updated
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
