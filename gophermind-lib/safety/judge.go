package safety

// JudgeFunc asks an external judge (e.g. a small model) whether a gated tool
// call should be allowed, given the tool name and its raw JSON arguments. It
// returns the decision, a short reason, and an error if the judge was
// unreachable.
type JudgeFunc func(tool, argsJSON string) (approve bool, reason string, err error)

// JudgeApproval builds an ApprovalFunc that routes each gated call to a judge.
// The judge's verdict is used directly; if the judge errors (or is nil), the
// decision defers to the fallback (e.g. an interactive prompt or Auto), so a
// judge outage never silently blocks or opens the gate. This is smarter than a
// static regex gate for ambiguous cases.
func JudgeApproval(judge JudgeFunc, fallback ApprovalFunc) ApprovalFunc {
	if judge == nil {
		return fallback
	}
	return func(tool, argsJSON string) bool {
		approve, _, err := judge(tool, argsJSON)
		if err != nil {
			return fallback(tool, argsJSON)
		}
		return approve
	}
}
