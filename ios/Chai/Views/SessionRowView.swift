import SwiftUI

struct SessionRowView: View {
    let session: Session

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(session.title ?? "Untitled")
                    .font(.headline)

                if session.streamStatus == .streaming {
                    ProgressView()
                        .scaleEffect(0.7)
                }
            }

            Text(session.updatedAt, style: .relative)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .padding(.vertical, 4)
    }
}
