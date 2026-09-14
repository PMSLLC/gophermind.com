package embed

import (
	"context"
	"testing"
)

func TestHitAtK(t *testing.T) {
	idx := &Index{Vectors: []Vector{
		{ID: "alpha.txt#0", Text: "alpha alpha", Values: []float32{2, 0, 0.01}},
		{ID: "beta.txt#0", Text: "beta beta", Values: []float32{0, 2, 0.01}},
	}}
	p := fakeProvider{}
	fixtures := []EvalCase{
		{Query: "alpha", Expect: "alpha.txt"},
		{Query: "beta", Expect: "beta.txt"},
	}
	score, err := HitAtK(context.Background(), p, idx, fixtures, 1)
	if err != nil {
		t.Fatal(err)
	}
	if score != 1.0 {
		t.Errorf("both fixtures should hit@1, score=%f", score)
	}

	// A fixture expecting the wrong doc lowers the score.
	fixtures = append(fixtures, EvalCase{Query: "alpha", Expect: "beta.txt"})
	score, _ = HitAtK(context.Background(), p, idx, fixtures, 1)
	if score >= 1.0 {
		t.Errorf("a wrong expectation should lower hit@k, score=%f", score)
	}
}
