import Foundation

struct ToolCall: Codable, Hashable, Identifiable {
    let id: String
    let name: String
    let input: [String: AnyCodable]

    enum CodingKeys: String, CodingKey {
        case id
        case name
        case input
    }
}

struct PermissionRequest: Identifiable {
    let id: String
    let toolName: String
    let input: [String: AnyCodable]
}
