package embed

import (
	"context"
	"fmt"
	"strings"
)

// EvalCase is one retrieval fixture: a query and the id (or id prefix, e.g. a
// file path) expected among the top-k results.
type EvalCase struct {
	Query  string `json:"query"`
	Expect string `json:"expect"`
}

// HitAtK returns the fraction of fixtures whose expected id appears (as a
// substring of a result id) within the top-k retrieved for the query — the
// standard hit@k retrieval-quality metric.
func HitAtK(ctx context.Context, p Provider, idx *Index, cases []EvalCase, k int) (float64, error) {
	if len(cases) == 0 {
		return 0, fmt.Errorf("no fixtures")
	}
	hits := 0
	for _, c := range cases {
		qv, err := p.Embed(ctx, []string{c.Query})
		if err != nil || len(qv) == 0 {
			return 0, fmt.Errorf("embed query %q: %w", c.Query, err)
		}
		for _, h := range TopK(qv[0], idx.Vectors, k) {
			if strings.Contains(h.ID, c.Expect) {
				hits++
				break
			}
		}
	}
	return float64(hits) / float64(len(cases)), nil
}
