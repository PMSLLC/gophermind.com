package safety

// PolicyApproval wraps a fallback ApprovalFunc with per-tool policy from a
// loaded Policy: a tool mapped to "always" is auto-approved, "never" is denied,
// and "ask" (or any unlisted tool) defers to the fallback (e.g. an interactive
// prompt or Auto). A nil policy passes straight through to the fallback. This is
// what lets a repo declare "always allow read, ask on write, never auto shell".
func PolicyApproval(p *Policy, fallback ApprovalFunc) ApprovalFunc {
	if p == nil {
		return fallback
	}
	return func(tool, argsJSON string) bool {
		switch p.ToolApprovalPolicy(tool) {
		case "always":
			return true
		case "never":
			return false
		default: // "ask"
			return fallback(tool, argsJSON)
		}
	}
}
