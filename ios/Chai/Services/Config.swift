import Foundation

enum Config {
    static var defaultServerURL: String {
        // Check for non-nil AND non-empty (env var unset -> empty string)
        if let value = Bundle.main.infoDictionary?["ChaiServerURL"] as? String,
           !value.isEmpty {
            return value
        }
        return "http://localhost:8080"
    }
}
