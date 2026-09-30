package executor

import (
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/settings"
)

// ladderEntry is one model of a leaf's ladder.
type ladderEntry struct {
	Name          string // "provider/model", the router's Only value
	ContextTokens int
	Tier          router.Tier // the chain the entry comes from: the router finds Name in that tier
	Pos           int         // 1-based position in this leaf's entry list (Attempt.Order)
}

// ladderEntries is the chain of the leaf's tier (standard when unset) in
// order, then the entries of the strong chain not already present. An entry
// whose provider or model is not configured is dropped. A model appears once,
// so it is never tried twice in a revision. The result depends only on the
// settings and the leaf.
func (rc *runCtx) ladderEntries(l *Leaf) []ladderEntry {
	tier := router.Tier(l.Tier)
	if tier == "" {
		tier = router.TierStandard
	}
	var out []ladderEntry
	seen := map[string]bool{}
	add := func(chain []string, t router.Tier) {
		for _, name := range chain {
			if seen[name] {
				continue
			}
			mi, ok := rc.cfg.ModelInfo(name)
			if !ok {
				continue
			}
			if _, _, ok := settings.SplitEntry(name); !ok {
				continue
			}
			seen[name] = true
			out = append(out, ladderEntry{Name: name, ContextTokens: mi.ContextTokens, Tier: t, Pos: len(out) + 1})
		}
	}
	add(rc.cfg.Models[string(tier)], tier)
	add(rc.cfg.Models[string(router.TierStrong)], router.TierStrong)
	return out
}

// fixTemperature is the sampling temperature of attempt k of an entry: the
// first try and the first fix at 0, every later fix at 0.3 (ruling S7).
func fixTemperature(k int) float64 {
	if k >= 2 {
		return 0.3
	}
	return 0
}
