import Foundation

/// `GET /session` list entry. Raw Go struct field names — no JSON tags on the
/// server — so the keys are capitalized exactly as shown in
/// `docs/mobile-serve.md` (`session.Info`).
struct SessionInfo: Decodable, Equatable, Identifiable {
    let id: String
    let path: String
    let size: Int
    let modTime: Date
    let messages: Int
    let title: String

    enum CodingKeys: String, CodingKey {
        case id = "ID"
        case path = "Path"
        case size = "Size"
        case modTime = "ModTime"
        case messages = "Messages"
        case title = "Title"
    }
}
