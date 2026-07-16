package ngram

import "testing"

// buildBackoffCorpus trains a model where trigram, bigram, and unigram
// predictions diverge, so tests can exercise each backoff level in
// isolation.
//
// Trained lines (each repeated the given number of times):
//
//	"x a b c" x3  -> trigram (a,b)->c=3   bigram b->c=3   unigram c=3
//	"y a b d" x1  -> trigram (a,b)->d=1   bigram b->d=1   unigram d=1
//	"z q b e" x5  -> trigram (q,b)->e=5   bigram b->e=5   unigram e=5
//
// Global unigram counts: b=9 (highest), q=5, z=5, e=5, a=4, x=3, c=3, y=1, d=1.
func buildBackoffCorpus() *Model {
	m := New()
	for i := 0; i < 3; i++ {
		m.Train("x a b c")
	}
	m.Train("y a b d")
	for i := 0; i < 5; i++ {
		m.Train("z q b e")
	}
	return m
}

func TestPredict_TrigramWins(t *testing.T) {
	m := buildBackoffCorpus()

	// Trigram context (a,b) has c=3 vs d=1 -> "c" wins on count, and it
	// differs from the pure-bigram answer for context "b" (which would be
	// "e", see TestPredict_BackoffToBigram). This proves the trigram level
	// is actually consulted first.
	got, ok := m.Predict([]string{"x", "a", "b"})
	if !ok || got != "c" {
		t.Fatalf("Predict(x a b) = %q, %v; want c, true", got, ok)
	}
}

func TestPredict_BackoffToBigram(t *testing.T) {
	m := buildBackoffCorpus()

	// "unseen b" is not a trained trigram context, so this must fall back
	// to the bigram context "b", where e=5 is the top candidate.
	got, ok := m.Predict([]string{"unseen", "b"})
	if !ok || got != "e" {
		t.Fatalf("Predict(unseen b) = %q, %v; want e, true", got, ok)
	}
}

func TestPredict_BackoffToUnigram(t *testing.T) {
	m := buildBackoffCorpus()

	// No trigram or bigram context matches at all, so this must fall back
	// to the globally most frequent word: "b" (count 9).
	got, ok := m.Predict([]string{"totally", "unknown", "words"})
	if !ok || got != "b" {
		t.Fatalf("Predict(totally unknown words) = %q, %v; want b, true", got, ok)
	}
}

func TestPredict_SilentBelowMinCount(t *testing.T) {
	m := New() // default MinCount = 2
	m.Train("alpha beta gamma")

	// Every n-gram in this corpus was observed exactly once, so nothing
	// meets the default MinCount of 2 at any backoff level.
	got, ok := m.Predict([]string{"alpha", "beta"})
	if ok {
		t.Fatalf("Predict(alpha beta) = %q, %v; want \"\", false", got, ok)
	}
}

func TestPredict_MinCountOneAllowsSingleObservation(t *testing.T) {
	m := New()
	m.MinCount = 1
	m.Train("alpha beta gamma")

	got, ok := m.Predict([]string{"alpha", "beta"})
	if !ok || got != "gamma" {
		t.Fatalf("Predict(alpha beta) = %q, %v; want gamma, true", got, ok)
	}
}

func TestPredict_TieBreaksLexicallyAndIsDeterministic(t *testing.T) {
	m := New()
	// Trigram context (p,q) ties between "r" and "s" at count 2 each.
	for i := 0; i < 2; i++ {
		m.Train("p q r")
		m.Train("p q s")
	}

	for i := 0; i < 20; i++ {
		got, ok := m.Predict([]string{"p", "q"})
		if !ok || got != "r" {
			t.Fatalf("call %d: Predict(p q) = %q, %v; want r, true (lexically smaller of tied r/s)", i, got, ok)
		}
	}
}

func TestTrainAll(t *testing.T) {
	m := New()
	m.TrainAll([]string{"x a b c", "x a b c", "x a b c", "y a b d"})

	got, ok := m.Predict([]string{"x", "a", "b"})
	if !ok || got != "c" {
		t.Fatalf("Predict(x a b) after TrainAll = %q, %v; want c, true", got, ok)
	}
}

func TestPredict_ShortPrefixSkipsUnavailableLevels(t *testing.T) {
	m := buildBackoffCorpus()

	// A single-word prefix can't supply trigram context, so it should skip
	// straight to the bigram check.
	got, ok := m.Predict([]string{"b"})
	if !ok || got != "e" {
		t.Fatalf("Predict(b) = %q, %v; want e, true", got, ok)
	}

	// An empty prefix skips both trigram and bigram checks and falls back
	// straight to the unigram level.
	got, ok = m.Predict(nil)
	if !ok || got != "b" {
		t.Fatalf("Predict(nil) = %q, %v; want b, true", got, ok)
	}
}
