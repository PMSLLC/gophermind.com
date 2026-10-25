package planner

import "fmt"

// structureKeys are the keys of a component or root structure reply.
var structureKeys = []string{"rationale", "assumptions", "open_questions", "decision_ids"}

// nearKey finds the one key of x that is a mistyped spelling of want (within
// two edits, and not itself a key of the reply's shape), so a typo such as
// "ratione" is not reported as a missing key the model then repeats. It returns
// false when want is present, when no key is that close, or when two are.
func nearKey(x map[string]any, want string) (string, bool) {
	if _, have := x[want]; have {
		return "", false
	}
	known := map[string]bool{}
	for _, k := range structureKeys {
		known[k] = true
	}
	found := ""
	for k := range x {
		if known[k] || len(k) < 5 || editDistance(k, want) > 2 {
			continue
		}
		if found != "" {
			return "", false
		}
		found = k
	}
	return found, found != ""
}

// normalizeStructureKeys moves each near-miss key of x to its right name and
// returns one warning per move; label names the structure.
func normalizeStructureKeys(x map[string]any, label string) []string {
	var warns []string
	for _, want := range structureKeys {
		if from, ok := nearKey(x, want); ok {
			x[want] = x[from]
			delete(x, from)
			warns = append(warns, fmt.Sprintf("%s: %s_key_normalized", label, want))
		}
	}
	return warns
}

// editDistance is the Levenshtein distance between a and b.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}
