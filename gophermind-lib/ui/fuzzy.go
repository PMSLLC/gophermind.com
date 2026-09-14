package ui

import (
	"sort"
	"strings"
)

// fuzzyScore returns (matched, score) for a subsequence match of query in
// candidate (case-insensitive). Higher is better: consecutive and early matches
// score more. Returns matched=false when query is not a subsequence.
func fuzzyScore(query, candidate string) (bool, int) {
	q := strings.ToLower(query)
	c := strings.ToLower(candidate)
	if q == "" {
		return true, 0
	}
	score, qi, prev := 0, 0, -2
	for ci := 0; ci < len(c) && qi < len(q); ci++ {
		if c[ci] == q[qi] {
			if ci == prev+1 {
				score += 3 // consecutive
			} else {
				score++
			}
			if ci == 0 {
				score += 2 // matches at start
			}
			prev = ci
			qi++
		}
	}
	if qi != len(q) {
		return false, 0
	}
	return true, score
}

// FuzzyFilter returns the candidates matching query as a subsequence, ranked
// best-first (ties broken by original order for stability).
func FuzzyFilter(query string, candidates []string) []string {
	type hit struct {
		s     string
		score int
		idx   int
	}
	var hits []hit
	for i, c := range candidates {
		if ok, sc := fuzzyScore(query, c); ok {
			hits = append(hits, hit{c, sc, i})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].idx < hits[j].idx
	})
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.s
	}
	return out
}
