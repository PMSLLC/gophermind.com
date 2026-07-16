// Package ngram implements a small, self-contained Markov (n-gram)
// next-word predictor. It is stdlib-only and independent of the rest of
// bubblecomplete: train it on a corpus of lines, then ask it to predict the
// next word given the preceding words.
package ngram

import (
	"sort"
	"strings"
)

// trigramSep separates the two context words used as a trigram map key.
// A NUL byte is used because it cannot appear in strings.Fields tokens of
// normal text, avoiding collisions between e.g. ("a b", "c") and ("a", "b
// c").
const trigramSep = "\x00"

// Model is a trigram-backoff next-word predictor built from raw counts.
//
// MinCount is the minimum observation count a candidate must have before
// Predict will return it; below that threshold Predict stays silent
// (returns ok == false) rather than guessing from too little evidence.
// New sets it to 2; set it to 1 to allow predictions from a single
// observation.
type Model struct {
	MinCount int

	unigram map[string]int
	bigram  map[string]map[string]int
	trigram map[string]map[string]int
}

// New returns an empty Model with MinCount set to 2.
func New() *Model {
	return &Model{
		MinCount: 2,
		unigram:  make(map[string]int),
		bigram:   make(map[string]map[string]int),
		trigram:  make(map[string]map[string]int),
	}
}

// Train updates the model's unigram, bigram, and trigram next-word counts
// from a single line of text, tokenized on whitespace via strings.Fields.
// Tokenization is case-sensitive; words retain their original casing.
func (m *Model) Train(line string) {
	tokens := strings.Fields(line)
	for i, w := range tokens {
		m.unigram[w]++
		if i >= 1 {
			addCount(m.bigram, tokens[i-1], w)
		}
		if i >= 2 {
			addCount(m.trigram, trigramKey(tokens[i-2], tokens[i-1]), w)
		}
	}
}

// TrainAll calls Train on each line in order.
func (m *Model) TrainAll(lines []string) {
	for _, line := range lines {
		m.Train(line)
	}
}

// Predict returns the most likely next word following prefixWords, using
// trigram -> bigram -> unigram backoff. At each level, the candidate with
// the highest observed count is chosen; ties are broken by lexical order
// (smallest string wins) so the result is deterministic regardless of Go's
// randomized map iteration order. A level is used only if its top
// candidate's count is >= MinCount; otherwise Predict falls back to the
// next level. If no level yields a qualifying candidate, Predict returns
// ("", false).
func (m *Model) Predict(prefixWords []string) (word string, ok bool) {
	if len(prefixWords) >= 2 {
		ctx := trigramKey(prefixWords[len(prefixWords)-2], prefixWords[len(prefixWords)-1])
		if cands, found := m.trigram[ctx]; found {
			if w, ok := bestCandidate(cands, m.MinCount); ok {
				return w, true
			}
		}
	}

	if len(prefixWords) >= 1 {
		ctx := prefixWords[len(prefixWords)-1]
		if cands, found := m.bigram[ctx]; found {
			if w, ok := bestCandidate(cands, m.MinCount); ok {
				return w, true
			}
		}
	}

	if w, ok := bestCandidate(m.unigram, m.MinCount); ok {
		return w, true
	}

	return "", false
}

// bestCandidate picks the highest-count key in counts, breaking ties by
// lexical order (smallest wins). It returns ok == false if counts is empty
// or the winning count is below minCount.
func bestCandidate(counts map[string]int, minCount int) (word string, ok bool) {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	bestCount := 0
	for _, k := range keys {
		if c := counts[k]; c > bestCount {
			bestCount = c
			word = k
			ok = true
		}
	}

	if !ok || bestCount < minCount {
		return "", false
	}
	return word, true
}

// addCount increments contexts[ctx][word], initializing the inner map if
// needed.
func addCount(contexts map[string]map[string]int, ctx, word string) {
	inner, found := contexts[ctx]
	if !found {
		inner = make(map[string]int)
		contexts[ctx] = inner
	}
	inner[word]++
}

// trigramKey builds the map key for a two-word trigram context.
func trigramKey(w1, w2 string) string {
	return w1 + trigramSep + w2
}
