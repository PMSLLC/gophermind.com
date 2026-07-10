// Package fewshot provides a small bank of curated input/output examples and
// selects the most relevant ones for a task by term overlap, so they can be
// injected into the prompt for better in-context steering.
package fewshot

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Example is one curated few-shot demonstration.
type Example struct {
	Input  string `json:"input"`
	Output string `json:"output"`
}

// Load reads a JSON array of examples from path.
func Load(path string) ([]Example, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read examples: %w", err)
	}
	var bank []Example
	if err := json.Unmarshal(data, &bank); err != nil {
		return nil, fmt.Errorf("parse examples: %w", err)
	}
	return bank, nil
}

var wordRe = regexp.MustCompile(`[a-z0-9]+`)

// tokens lowercases and splits text into a set of word tokens.
func tokens(s string) map[string]bool {
	set := map[string]bool{}
	for _, w := range wordRe.FindAllString(strings.ToLower(s), -1) {
		set[w] = true
	}
	return set
}

// jaccard returns the Jaccard similarity of two token sets.
func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for w := range a {
		if b[w] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// Select returns up to k examples from bank most similar to task (by Jaccard
// term overlap), dropping any with zero overlap. Results are ranked best-first.
func Select(bank []Example, task string, k int) []Example {
	taskTok := tokens(task)
	type scored struct {
		ex    Example
		score float64
	}
	var ranked []scored
	for _, ex := range bank {
		s := jaccard(taskTok, tokens(ex.Input))
		if s > 0 {
			ranked = append(ranked, scored{ex, s})
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	if k < len(ranked) {
		ranked = ranked[:k]
	}
	out := make([]Example, len(ranked))
	for i, r := range ranked {
		out[i] = r.ex
	}
	return out
}

// Format renders examples as a prompt section the model can learn from.
func Format(examples []Example) string {
	if len(examples) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Here are relevant examples:\n")
	for i, ex := range examples {
		fmt.Fprintf(&b, "\nExample %d:\nInput: %s\nOutput: %s\n", i+1, ex.Input, ex.Output)
	}
	return b.String()
}
