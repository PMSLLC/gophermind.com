package embed

import "time"

// Supersede closes the validity window of every live vector whose cosine
// similarity to vec reaches threshold, marking them as no longer true from at,
// and returns the texts it retired.
//
// This is how a memory store stays honest without a migration or a pruning
// pass: a newly remembered fact retires the older phrasings of the same fact,
// which then stop surfacing in TopK while their history remains on disk.
// Vectors already retired keep their original ValidUntil.
func (idx *Index) Supersede(vec []float32, at time.Time, threshold float32) []string {
	var retired []string
	stamp := at.UTC().Format(time.RFC3339)
	for i := range idx.Vectors {
		v := &idx.Vectors[i]
		if v.ValidUntil != "" {
			continue
		}
		if cosine(vec, v.Values) < threshold {
			continue
		}
		v.ValidUntil = stamp
		retired = append(retired, v.Text)
	}
	return retired
}
