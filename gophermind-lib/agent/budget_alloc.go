package agent

// AllocateBudget splits a total cost/token budget across n subtasks (e.g.
// spawn_agent children) as evenly as possible, giving any remainder to the
// earliest children so no single child can consume the whole budget. Returns nil
// for n <= 0.
func AllocateBudget(total, n int) []int {
	if n <= 0 {
		return nil
	}
	if total < 0 {
		total = 0
	}
	base := total / n
	rem := total % n
	out := make([]int, n)
	for i := range out {
		out[i] = base
		if i < rem {
			out[i]++
		}
	}
	return out
}
