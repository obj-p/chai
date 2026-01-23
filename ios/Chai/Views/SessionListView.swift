import SwiftUI

struct SessionListView: View {
    @AppStorage("serverURL") private var serverURL = Config.defaultServerURL
    @State private var sessions: [Session] = []
    @State private var isLoading = false
    @State private var errorMessage: String?
    @State private var showSettings = false

    private let client = APIClient()

    private var baseURL: URL? { URL(string: serverURL) }

    var body: some View {
        NavigationStack {
            Group {
                if isLoading && sessions.isEmpty {
                    ProgressView()
                } else if sessions.isEmpty {
                    ContentUnavailableView(
                        "No Sessions",
                        systemImage: "bubble.left.and.bubble.right",
                        description: Text("Start a new conversation")
                    )
                } else {
                    List {
                        ForEach(sessions) { session in
                            NavigationLink(value: session) {
                                SessionRowView(session: session)
                            }
                            .swipeActions(edge: .trailing, allowsFullSwipe: true) {
                                Button(role: .destructive) {
                                    deleteSession(session)
                                } label: {
                                    Image(systemName: "trash")
                                }
                            }
                        }
                    }
                    .listStyle(.plain)
                }
            }
            .navigationTitle("")
            .navigationDestination(for: Session.self) { session in
                ChatView(session: session)
            }
            .toolbar {
                ToolbarItem(placement: .primaryAction) {
                    Button("New", systemImage: "plus") {
                        Task { await createSession() }
                    }
                }
                ToolbarItem(placement: .navigationBarLeading) {
                    Button("Settings", systemImage: "gear") {
                        showSettings = true
                    }
                }
            }
            .sheet(isPresented: $showSettings) {
                SettingsView()
            }
            .alert("Error", isPresented: .init(
                get: { errorMessage != nil },
                set: { if !$0 { errorMessage = nil } }
            )) {
                Button("OK") { errorMessage = nil }
            } message: {
                Text(errorMessage ?? "")
            }
            .task { await loadSessions() }
            .refreshable { await loadSessions() }
        }
    }

    private func loadSessions() async {
        guard let baseURL else {
            errorMessage = "Invalid server URL"
            return
        }
        isLoading = true
        defer { isLoading = false }
        do {
            sessions = try await client.listSessions(baseURL: baseURL)
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    private func createSession() async {
        guard let baseURL else { return }
        do {
            let session = try await client.createSession(baseURL: baseURL, title: nil)
            sessions.insert(session, at: 0)
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    private func deleteSession(_ session: Session) {
        guard let index = sessions.firstIndex(where: { $0.id == session.id }) else { return }

        // Remove with animation
        let _ = withAnimation {
            sessions.remove(at: index)
        }

        Task {
            guard let baseURL else { return }
            do {
                try await client.deleteSession(baseURL: baseURL, id: session.id)
            } catch {
                // Restore row on API failure
                await MainActor.run {
                    if !sessions.contains(where: { $0.id == session.id }) {
                        withAnimation {
                            sessions.insert(session, at: min(index, sessions.count))
                        }
                    }
                    errorMessage = error.localizedDescription
                }
            }
        }
    }
}
