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

                    DebugLogger.shared.log("SSEClient: making request to \(url)")
                    let (bytes, response) = try await URLSession.shared.bytes(for: request)

                    guard let httpResponse = response as? HTTPURLResponse else {
                        throw SSEError.invalidResponse(statusCode: 0)
                    }

                    DebugLogger.shared.log("SSEClient: response status=\(httpResponse.statusCode)")

                    if httpResponse.statusCode == 409 {
                        throw SSEError.sessionBusy
                    }

                    guard (200..<300).contains(httpResponse.statusCode) else {
                        throw SSEError.invalidResponse(statusCode: httpResponse.statusCode)
                    }

                    // Parse SSE stream line by line
                    // Note: bytes.lines skips empty lines, so we yield frames when we see
                    // a new event: line (indicating previous event is complete)
                    var currentEvent = ""
                    var currentData = ""
                    var lineCount = 0

                    for try await line in bytes.lines {
                        lineCount += 1
                        if lineCount <= 5 || lineCount % 10 == 0 {
                            DebugLogger.shared.log("SSEClient: line #\(lineCount): '\(line.prefix(80))'")
                        }

                        if line.hasPrefix("event:") {
                            // New event starting - yield previous if exists
                            if !currentEvent.isEmpty || !currentData.isEmpty {
                                let frame = SSEFrame(
                                    event: currentEvent.isEmpty ? "message" : currentEvent,
                                    data: currentData
                                )
                                continuation.yield(frame)
                                DebugLogger.shared.log("SSE: event=\(frame.event), data=\(frame.data.prefix(100))")
                            }
                            currentEvent = String(line.dropFirst(6)).trimmingCharacters(in: .whitespaces)
                            currentData = ""
                        } else if line.hasPrefix("data:") {
                            let data = String(line.dropFirst(5)).trimmingCharacters(in: .whitespaces)
                            if !currentData.isEmpty {
                                currentData += "\n"
                            }
                            currentData += data
                        }
                        // Ignore comments (lines starting with :) and other fields
                    }

                    // Yield final event if any
                    if !currentEvent.isEmpty || !currentData.isEmpty {
                        let frame = SSEFrame(
                            event: currentEvent.isEmpty ? "message" : currentEvent,
                            data: currentData
                        )
                        continuation.yield(frame)
                        DebugLogger.shared.log("SSE: final event=\(frame.event), data=\(frame.data.prefix(100))")
                    }

                    DebugLogger.shared.log("SSEClient: stream ended after \(lineCount) lines")
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
