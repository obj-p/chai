import Foundation

struct Session: Identifiable, Codable, Hashable {
    let id: String
    var title: String?
    var claudeSessionId: String?
    var workingDirectory: String?
    var streamStatus: StreamStatus
    let createdAt: Date
    var updatedAt: Date

    enum CodingKeys: String, CodingKey {
        case id, title
        case claudeSessionId = "claude_session_id"
        case workingDirectory = "working_directory"
        case streamStatus = "stream_status"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }
}

enum StreamStatus: String, Codable {
    case idle
    case streaming
    case completed
}
