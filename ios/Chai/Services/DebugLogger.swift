import Foundation
import os

final class DebugLogger: Sendable {
    static let shared = DebugLogger()

    private let logs: OSAllocatedUnfairLock<[String]> = OSAllocatedUnfairLock(initialState: [])
    private let maxLines = 500

    private init() {}

    func log(_ message: String) {
        let timestamp = ISO8601DateFormatter().string(from: Date())
        let entry = "[\(timestamp)] \(message)"
        logs.withLock { logs in
            logs.append(entry)
            if logs.count > maxLines {
                logs.removeFirst()
            }
        }
        print(entry) // Also print for Xcode console
    }

    func getAll() -> [String] {
        logs.withLock { $0 }
    }

    func clear() {
        logs.withLock { $0.removeAll() }
    }
}
