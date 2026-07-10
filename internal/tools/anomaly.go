package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// defaultAnomalyThreshold is the modified z-score above which a point is an
// outlier. 3.5 is the conventional cutoff for the MAD-based score.
const defaultAnomalyThreshold = 3.5

// DetectAnomalies returns a tool that flags statistical outliers in a numeric
// series using the robust modified z-score (median + MAD), which — unlike a
// plain z-score — is not masked by the very outlier it is looking for. Pure and
// offline: useful for spotting runaway token/cost turns or latency spikes.
func DetectAnomalies() Tool {
	return Tool{
		Name:        "detect_anomalies",
		Description: "Flag statistical outliers in a numeric series using the robust modified z-score (median/MAD). Returns each anomalous index/value; threshold defaults to 3.5.",
		Schema: object(map[string]any{
			"values":    map[string]any{"type": "array", "description": "The numeric series to scan.", "items": map[string]any{"type": "number"}},
			"threshold": map[string]any{"type": "number", "description": "Modified z-score threshold for flagging an outlier (default 3.5)."},
		}, "values"),
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Values    []float64 `json:"values"`
				Threshold float64   `json:"threshold"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if len(a.Values) < 3 {
				return "", fmt.Errorf("need at least 3 values to detect anomalies (got %d)", len(a.Values))
			}
			threshold := a.Threshold
			if threshold <= 0 {
				threshold = defaultAnomalyThreshold
			}

			med := median(a.Values)
			scores := modifiedZScores(a.Values, med)

			var b strings.Builder
			fmt.Fprintf(&b, "n=%d median=%g threshold=%g\n", len(a.Values), round4(med), threshold)
			found := 0
			for i, z := range scores {
				if math.Abs(z) >= threshold {
					found++
					fmt.Fprintf(&b, "anomaly: index %d value %g (score=%.2f)\n", i, a.Values[i], z)
				}
			}
			if found == 0 {
				b.WriteString("no anomalies\n")
			}
			return b.String(), nil
		},
	}
}

// modifiedZScores returns the robust modified z-score of each value. When the
// median absolute deviation is zero (e.g. a majority of identical values), it
// falls back to the standard-deviation-based z-score so a lone spike is still
// detected.
func modifiedZScores(xs []float64, med float64) []float64 {
	devs := make([]float64, len(xs))
	for i, x := range xs {
		devs[i] = math.Abs(x - med)
	}
	mad := median(devs)

	scores := make([]float64, len(xs))
	if mad > 0 {
		for i, x := range xs {
			scores[i] = 0.6745 * (x - med) / mad
		}
		return scores
	}
	// MAD == 0: fall back to the population-stddev z-score.
	mean, std := meanStd(xs)
	if std == 0 {
		return scores // all identical: no deviation
	}
	for i, x := range xs {
		scores[i] = (x - mean) / std
	}
	return scores
}

// median returns the median of xs (does not mutate xs).
func median(xs []float64) float64 {
	c := make([]float64, len(xs))
	copy(c, xs)
	sort.Float64s(c)
	n := len(c)
	if n%2 == 1 {
		return c[n/2]
	}
	return (c[n/2-1] + c[n/2]) / 2
}

// meanStd returns the mean and population standard deviation of xs.
func meanStd(xs []float64) (mean, std float64) {
	for _, x := range xs {
		mean += x
	}
	mean /= float64(len(xs))
	var variance float64
	for _, x := range xs {
		d := x - mean
		variance += d * d
	}
	variance /= float64(len(xs))
	return mean, math.Sqrt(variance)
}

func round4(f float64) float64 {
	return math.Round(f*10000) / 10000
}
