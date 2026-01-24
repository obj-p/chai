import Foundation

actor SSEClient {
    enum SSEError: Error, LocalizedError, Equatable {
        case invalidURL
        case sessionBusy
        case connectionLost
        case invalidResponse(statusCode: Int)

        var errorDescription: String? {
            switch self {
            case .invalidURL:
                return "Invalid server URL"
            case .sessionBusy:
                return "Session is already streaming"
            case .connectionLost:
                return "Connection lost"
            case .invalidResponse(let statusCode):
                return "Server error: \(statusCode)"
            }
        }
    }

    /// Stream prompt responses via SSE
    func streamPrompt(
        baseURL: URL,
        sessionId: String,
        prompt: String
    ) -> AsyncThrowingStream<SSEFrame, Error> {
        AsyncThrowingStream { continuation in
            let task = Task {
                do {
                    let url = baseURL.appendingPathComponent("api/sessions/\(sessionId)/prompt")
                    var request = URLRequest(url: url)
                    request.httpMethod = "POST"
                    request.setValue("application/json", forHTTPHeaderField: "Content-Type")
                    request.setValue("text/event-stream", forHTTPHeaderField: "Accept")
                    request.httpBody = try JSONEncoder().encode(["prompt": prompt])

                    let (bytes, response) = try await URLSession.shared.bytes(for: request)

                    guard let httpResponse = response as? HTTPURLResponse else {
                        throw SSEError.invalidResponse(statusCode: 0)
                    }

                    if httpResponse.statusCode == 409 {
                        throw SSEError.sessionBusy
                    }

                    guard (200..<300).contains(httpResponse.statusCode) else {
                        throw SSEError.invalidResponse(statusCode: httpResponse.statusCode)
                    }

                    // Parse SSE stream line by line
                    // Each SSE event is: event: <name>\ndata: <json>\n\n
                    // Since bytes.lines skips empty lines, we yield immediately after
                    // receiving the data: line (each event has exactly one data line)
                    var currentEvent = ""

                    for try await line in bytes.lines {
                        if line.hasPrefix("event:") {
                            currentEvent = String(line.dropFirst(6)).trimmingCharacters(in: .whitespaces)
                        } else if line.hasPrefix("data:") {
                            let data = String(line.dropFirst(5)).trimmingCharacters(in: .whitespaces)
                            // Yield frame immediately - don't wait for next event
                            let frame = SSEFrame(
                                event: currentEvent.isEmpty ? "message" : currentEvent,
                                data: data
                            )
                            continuation.yield(frame)
                            currentEvent = ""  // Reset for next event
                        }
                        // Ignore comments (lines starting with :) and other fields
                    }
                    continuation.finish()
                } catch {
                    continuation.finish(throwing: error)
                }
            }

            continuation.onTermination = { @Sendable _ in
                task.cancel()
            }
        }
    }

    /// Fetch missed events after reconnection
    func catchUpEvents(
        baseURL: URL,
        sessionId: String,
        promptId: String,
        sinceSequence: Int64
    ) async throws -> GetEventsResponse {
        guard var components = URLComponents(
            url: baseURL.appendingPathComponent("api/sessions/\(sessionId)/events"),
            resolvingAgainstBaseURL: false
        ) else {
            throw SSEError.invalidURL
        }
        components.queryItems = [
            URLQueryItem(name: "prompt_id", value: promptId),
            URLQueryItem(name: "since_sequence", value: String(sinceSequence)),
            URLQueryItem(name: "limit", value: "100")
        ]

        guard let url = components.url else {
            throw SSEError.invalidURL
        }

        let (data, response) = try await URLSession.shared.data(from: url)

        guard let httpResponse = response as? HTTPURLResponse,
              (200..<300).contains(httpResponse.statusCode) else {
            throw URLError(.badServerResponse)
        }

        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return try decoder.decode(GetEventsResponse.self, from: data)
    }
}
