import SwiftUI

struct ChatView: View {
    let session: Session

    var body: some View {
        Text("Chat: \(session.title ?? session.id)")
            .navigationTitle(session.title ?? "Chat")
    }
}
