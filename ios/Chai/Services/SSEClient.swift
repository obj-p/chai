import Foundation

actor SSEClient {
    private var activeTask: Task<Void, Never>?

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
                    var currentEvent = ""
                    var currentData = ""

                    for try await line in bytes.lines {
                        if line.isEmpty {
                            // Empty line marks end of event
                            if !currentEvent.isEmpty || !currentData.isEmpty {
                                let frame = SSEFrame(
                                    event: currentEvent.isEmpty ? "message" : currentEvent,
                                    data: currentData
                                )
                                continuation.yield(frame)
                                currentEvent = ""
                                currentData = ""
                            }
                        } else if line.hasPrefix("event:") {
                            currentEvent = String(line.dropFirst(6)).trimmingCharacters(in: .whitespaces)
                        } else if line.hasPrefix("data:") {
                            let data = String(line.dropFirst(5)).trimmingCharacters(in: .whitespaces)
                            if !currentData.isEmpty {
                                currentData += "\n"
                            }
                            currentData += data
                        }
                        // Ignore comments (lines starting with :) and other fields
                    }

                    continuation.finish()
                } catch {
                    continuation.finish(throwing: error)
                }
            }

            self.activeTask = task

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

    func cancel() {
        activeTask?.cancel()
        activeTask = nil
    }
}
