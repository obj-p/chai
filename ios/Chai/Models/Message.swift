import Foundation

struct Message: Identifiable, Codable, Hashable {
    let id: String
    let sessionId: String
    let role: MessageRole
    var content: String
    var toolCalls: [ToolCall]?
    let createdAt: Date

    // Local-only state for streaming (not from API)
    var isStreaming: Bool = false

    enum CodingKeys: String, CodingKey {
        case id
        case sessionId = "session_id"
        case role, content
        case toolCalls = "tool_calls"
        case createdAt = "created_at"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        sessionId = try container.decode(String.self, forKey: .sessionId)
        role = try container.decode(MessageRole.self, forKey: .role)
        content = try container.decode(String.self, forKey: .content)
        toolCalls = try container.decodeIfPresent([ToolCall].self, forKey: .toolCalls)
        createdAt = try container.decode(Date.self, forKey: .createdAt)
        isStreaming = false
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(id, forKey: .id)
        try container.encode(sessionId, forKey: .sessionId)
        try container.encode(role, forKey: .role)
        try container.encode(content, forKey: .content)
        try container.encodeIfPresent(toolCalls, forKey: .toolCalls)
        try container.encode(createdAt, forKey: .createdAt)
    }

    // Local initializer for optimistic/streaming messages
    init(id: String, sessionId: String, role: MessageRole, content: String = "", isStreaming: Bool = false) {
        self.id = id
        self.sessionId = sessionId
        self.role = role
        self.content = content
        self.toolCalls = nil
        self.createdAt = Date()
        self.isStreaming = isStreaming
    }
}

enum MessageRole: String, Codable {
    case user
    case assistant
    case system
}
