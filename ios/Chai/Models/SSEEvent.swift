import Foundation

// Raw SSE frame from server
struct SSEFrame {
    let event: String
    let data: String
}

// Response from GET /api/sessions/{id}
struct SessionResponse: Codable {
    let session: Session
    let messages: [Message]

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        session = try container.decode(Session.self, forKey: .session)
        messages = try container.decodeIfPresent([Message].self, forKey: .messages) ?? []
    }

    enum CodingKeys: String, CodingKey {
        case session, messages
    }
}

// Response from GET /api/sessions/{id}/events
struct GetEventsResponse: Codable {
    let events: [SessionEvent]
    let lastSequence: Int64
    let hasMore: Bool
    let streamStatus: StreamStatus

    enum CodingKeys: String, CodingKey {
        case events
        case lastSequence = "last_sequence"
        case hasMore = "has_more"
        case streamStatus = "stream_status"
    }
}

// Persisted SSE event for catch-up
struct SessionEvent: Codable, Identifiable {
    let id: Int64
    let sessionId: String
    let promptId: String
    let sequence: Int64
    let eventType: String
    let data: String
    let createdAt: Date

    enum CodingKeys: String, CodingKey {
        case id
        case sessionId = "session_id"
        case promptId = "prompt_id"
        case sequence
        case eventType = "event_type"
        case data
        case createdAt = "created_at"
    }
}
