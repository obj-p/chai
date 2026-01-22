import Foundation

struct APIError: Error, LocalizedError, Decodable {
    let error: String
    var errorDescription: String? { error }
}

actor APIClient {
    private let session = URLSession.shared
    private let decoder: JSONDecoder = {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .iso8601
        return d
    }()
    private let encoder = JSONEncoder()

    func listSessions(baseURL: URL) async throws -> [Session] {
        let url = baseURL.appendingPathComponent("api/sessions")
        let (data, response) = try await session.data(from: url)
        try checkResponse(response, data: data)
        return try decoder.decode([Session].self, from: data)
    }

    func createSession(baseURL: URL, title: String?) async throws -> Session {
        let url = baseURL.appendingPathComponent("api/sessions")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        if let title {
            request.httpBody = try encoder.encode(["title": title])
        } else {
            request.httpBody = Data("{}".utf8)
        }
        let (data, response) = try await session.data(for: request)
        try checkResponse(response, data: data)
        return try decoder.decode(Session.self, from: data)
    }

    func deleteSession(baseURL: URL, id: String) async throws {
        let url = baseURL.appendingPathComponent("api/sessions/\(id)")
        var request = URLRequest(url: url)
        request.httpMethod = "DELETE"
        let (data, response) = try await session.data(for: request)
        try checkResponse(response, data: data)
    }

    private func checkResponse(_ response: URLResponse, data: Data) throws {
        guard let http = response as? HTTPURLResponse else { return }
        guard (200..<300).contains(http.statusCode) else {
            if let apiError = try? decoder.decode(APIError.self, from: data) {
                throw apiError
            }
            throw URLError(.badServerResponse)
        }
    }
}
