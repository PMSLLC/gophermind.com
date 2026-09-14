package project

// bytesPerToken is the coarse heuristic used to bound injected context by an
// approximate token budget (matches the llm trimmer's ~4-bytes-per-token rule).
const bytesPerToken = 4

// CapContext bounds an injected-context string to roughly maxTokens, so the
// system-prompt additions (persona + repo instructions + repo map) can't crowd
// out the room the task itself needs. A non-positive maxTokens disables capping.
// When truncation happens, a short note is appended so the model knows content
// was dropped.
func CapContext(text string, maxTokens int) string {
	if maxTokens <= 0 || text == "" {
		return text
	}
	maxBytes := maxTokens * bytesPerToken
	if len(text) <= maxBytes {
		return text
	}
	const note = "\n… [context truncated to fit the token budget]"
	keep := maxBytes - len(note)
	if keep < 0 {
		keep = 0
	}
	return text[:keep] + note
}
