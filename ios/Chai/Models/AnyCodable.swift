import Foundation

struct AnyCodable: Codable, Hashable {
    let value: Any

    init(_ value: Any) {
        self.value = value
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()

        if container.decodeNil() {
            value = ()
        } else if let bool = try? container.decode(Bool.self) {
            value = bool
        } else if let int = try? container.decode(Int.self) {
            value = int
        } else if let double = try? container.decode(Double.self) {
            value = double
        } else if let string = try? container.decode(String.self) {
            value = string
        } else if let array = try? container.decode([AnyCodable].self) {
            value = array.map { $0.value }
        } else if let dict = try? container.decode([String: AnyCodable].self) {
            value = dict.mapValues { $0.value }
        } else {
            throw DecodingError.dataCorruptedError(
                in: container,
                debugDescription: "Unable to decode value"
            )
        }
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()

        switch value {
        case is Void:
            try container.encodeNil()
        case let bool as Bool:
            try container.encode(bool)
        case let int as Int:
            try container.encode(int)
        case let double as Double:
            try container.encode(double)
        case let string as String:
            try container.encode(string)
        case let array as [Any]:
            try container.encode(array.map { AnyCodable($0) })
        case let dict as [String: Any]:
            try container.encode(dict.mapValues { AnyCodable($0) })
        default:
            throw EncodingError.invalidValue(
                value,
                EncodingError.Context(
                    codingPath: container.codingPath,
                    debugDescription: "Unable to encode value"
                )
            )
        }
    }

    static func == (lhs: AnyCodable, rhs: AnyCodable) -> Bool {
        switch (lhs.value, rhs.value) {
        case is (Void, Void):
            return true
        case let (l as Bool, r as Bool):
            return l == r
        case let (l as Int, r as Int):
            return l == r
        case let (l as Double, r as Double):
            return l == r
        case let (l as String, r as String):
            return l == r
        case let (l as [Any], r as [Any]):
            return l.count == r.count &&
                zip(l, r).allSatisfy { AnyCodable($0) == AnyCodable($1) }
        case let (l as [String: Any], r as [String: Any]):
            return l.count == r.count &&
                l.allSatisfy { key, val in
                    r[key].map { AnyCodable(val) == AnyCodable($0) } ?? false
                }
        default:
            return false
        }
    }

    func hash(into hasher: inout Hasher) {
        // Use type discriminators to avoid collisions between types
        switch value {
        case is Void:
            hasher.combine("nil")
        case let bool as Bool:
            hasher.combine("bool")
            hasher.combine(bool)
        case let int as Int:
            hasher.combine("int")
            hasher.combine(int)
        case let double as Double:
            hasher.combine("double")
            hasher.combine(double)
        case let string as String:
            hasher.combine("string")
            hasher.combine(string)
        case let array as [Any]:
            hasher.combine("array")
            hasher.combine(array.count)
            for element in array {
                AnyCodable(element).hash(into: &hasher)
            }
        case let dict as [String: Any]:
            hasher.combine("dict")
            hasher.combine(dict.count)
            // Sort keys for deterministic hashing
            for key in dict.keys.sorted() {
                hasher.combine(key)
                AnyCodable(dict[key]!).hash(into: &hasher)
            }
        default:
            hasher.combine("unknown")
        }
    }
}
