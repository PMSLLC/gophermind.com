package planner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"gophermind/gophermind-lib/briefv2/router"
)

// Contract prompts are sized to the smallest context window in the tier's
// chain. The optional context (the brief, the ids already declared, the content
// of existing nodes) shrinks in a fixed order when a prompt would not fit; what
// remains at the last level is the minimum a pass needs.
//
//	level 0  everything (the whole brief for an outline pass)
//	level 1  the brief becomes an excerpt: the main sections and only the
//	         features the pass concerns, each capped
//	level 2  the declared-ids list is cut down and the excerpt caps tighten
//	level 3  existing nodes are trimmed to signature and doc, caps tighten again
const (
	// KindPromptSize is the event kind of a prompt size report (sizes only).
	KindPromptSize = "prompt_size"
	// promptHeadroomTokens is the margin kept beyond the estimate and the
	// reserved reply, because the estimate is rough.
	promptHeadroomTokens = 1000
	maxPromptLevel       = 3
)

// estimateTokens is the router's own estimate (bytes/4 plus a tenth), so the
// planner and the router agree on what fits.
func estimateTokens(bytes int) int { return bytes/4 + bytes/40 }

// contextWindow is the smallest context among the models of the strong tier
// that the settings know; 0 when none is known (nothing is then trimmed ahead
// of time, the router's answer decides).
func (p *Planner) contextWindow() int {
	min := 0
	for _, entry := range p.d.Settings.Models[string(router.TierStrong)] {
		if mi, ok := p.d.Settings.ModelInfo(entry); ok && mi.ContextTokens > 0 && (min == 0 || mi.ContextTokens < min) {
			min = mi.ContextTokens
		}
	}
	return min
}

// callSized makes a contract call whose prompt is built for a level of optional
// context. It starts at level 0 and moves down a level when the estimate does not
// fit the window, or when every model refuses the prompt as too long. It fails
// with a fixed message when even the last level does not fit.
func (p *Planner) callSized(ctx context.Context, r *run, cs callSpec, build func(level int) (string, error), parse func(text string) error) error {
	window := p.contextWindow()
	for level := 0; ; level++ {
		prompt, err := build(level)
		if err != nil {
			return err
		}
		est := estimateTokens(len(prompt) + len(stagePrefixSystem) + len(cs.stage) + 2)
		fits := window == 0 || est+cs.maxTokens+promptHeadroomTokens <= window
		if !fits {
			if level < maxPromptLevel {
				continue
			}
			return tooLongErr(cs.stage, est, cs.maxTokens, window)
		}
		p.emit(KindPromptSize, cs.stage, "", fmt.Sprintf("prompt_tokens_estimate=%d reserved=%d window=%d level=%d", est, cs.maxTokens, window, level))
		err = p.call(ctx, r, cs, prompt, parse)
		var ce *router.ChainExhausted
		if err != nil && errors.As(err, &ce) && ce.OnlyTooLong() {
			if level < maxPromptLevel {
				continue
			}
			return tooLongErr(cs.stage, est, cs.maxTokens, window)
		}
		return err
	}
}

func tooLongErr(stage string, est, reserved, window int) error {
	return fmt.Errorf("stage %s: the prompt (about %d tokens plus %d reserved, window %d) is too long for the model even with the optional context removed",
		stage, est, reserved, window)
}

// excerptCaps are the byte caps of a brief excerpt at a level.
type excerptCaps struct {
	sections       []string
	section        int // per main section
	feature, total int // per feature section, and all feature sections together
}

func excerptCapsAt(level int) excerptCaps {
	switch {
	case level <= 1:
		return excerptCaps{[]string{"Overview", "Data", "Architecture", "Constraints"}, 3000, 4000, 24000}
	case level == 2:
		return excerptCaps{[]string{"Overview", "Constraints"}, 1500, 1500, 12000}
	}
	return excerptCaps{[]string{"Overview", "Constraints"}, 600, 800, 4000}
}

// capText cuts s to at most n bytes (on a character boundary) and says so.
func capText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "\n(cut)"
}

// briefExcerpt is the part of the brief a pass needs: the main sections and the
// named features (in brief order), each capped so the excerpt is bounded however
// large the brief is.
func briefExcerpt(r *run, names []string, level int) string {
	caps := excerptCapsAt(level)
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s (an excerpt of the brief: only the parts this pass needs)\n", r.brief.Front.Title)
	for _, sec := range caps.sections {
		if body := r.brief.Sections[sec]; body != "" {
			fmt.Fprintf(&b, "\n## %s\n\n%s\n", sec, capText(body, caps.section))
		}
	}
	var feats []int
	for i, f := range r.brief.Features {
		if want[f.Name] {
			feats = append(feats, i)
		}
	}
	per := caps.feature
	if len(feats) > 0 && caps.total/len(feats) < per {
		if per = caps.total / len(feats); per < 200 {
			per = 200
		}
	}
	for _, i := range feats {
		f := r.brief.Features[i]
		fmt.Fprintf(&b, "\n### %s\n\n%s\n", f.Name, capText(f.Body, per))
	}
	return strings.TrimRight(b.String(), "\n")
}

func allFeatureNames(r *run) []string {
	var out []string
	for _, f := range r.brief.Features {
		out = append(out, f.Name)
	}
	return out
}

// featuresOfComponents maps component ids to the features they came from.
func featuresOfComponents(r *run, comps []string) []string {
	var out []string
	for _, f := range r.brief.Features {
		for _, c := range comps {
			if slug(f.Name) == c {
				out = append(out, f.Name)
				break
			}
		}
	}
	return out
}

// repairExcerptLevel is the excerpt level of a repair pass: repair prompts never
// carry the whole brief, so level 0 already uses an excerpt.
func repairExcerptLevel(level int) int {
	if level+1 > maxPromptLevel {
		return maxPromptLevel
	}
	return level + 1
}

// outlineJSONAt is the outline (module, conventions, components) for a prompt:
// complete up to level 1, component ids only after that.
func outlineJSONAt(doc map[string]any, level int) string {
	comps := doc["components"]
	if level >= 2 {
		var ids []string
		for _, c := range objects(doc["components"]) {
			ids = append(ids, fmt.Sprint(c["id"]))
		}
		comps = ids
	}
	return mustJSON(map[string]any{"module": doc["module"], "conventions": doc["conventions"], "components": comps})
}

// declaredTextAt lists what is declared: in full up to level 1; from level 2
// type ids and function signatures; at level 3 ids only.
func declaredTextAt(doc map[string]any, level int) string {
	if level <= 1 {
		return declaredText(doc)
	}
	var b strings.Builder
	for _, t := range objects(doc["types"]) {
		fmt.Fprintf(&b, "%v\n", t["id"])
	}
	for _, f := range objects(doc["functions"]) {
		if level == 2 {
			fmt.Fprintf(&b, "%v: %v\n", f["id"], f["signature"])
		} else {
			fmt.Fprintf(&b, "%v\n", f["id"])
		}
	}
	if b.Len() == 0 {
		return "(nothing yet)"
	}
	return strings.TrimRight(b.String(), "\n")
}

// briefSectionAt is a component's feature section, capped from level 1.
func briefSectionAt(r *run, component string, level int) string {
	s := briefSection(r, component)
	if level <= 0 {
		return s
	}
	return capText(s, excerptCapsAt(level).feature)
}
