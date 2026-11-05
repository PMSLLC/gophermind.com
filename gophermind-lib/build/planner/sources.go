package planner

import (
	"fmt"
	"sort"
	"strings"

	"gophermind/gophermind-lib/build/brief"
)

// sourceRef names one part of the brief a node packet can carry.
type sourceRef struct {
	ID      string `json:"id"`
	Section string `json:"section"`
	Bytes   int    `json:"bytes"`
}

// packetSections are the main sections a shrunk node packet carries, in order.
var packetSections = []string{"Architecture", "Data", "Constraints"}

// mainSectionOrder is the order the main sections are listed in; others follow
// alphabetically.
var mainSectionOrder = []string{"Overview", "Architecture", "Data", "Constraints", "Out of scope", "Acceptance"}

// briefSources lists every main section, then every feature.
func briefSources(b *brief.Brief) []sourceRef {
	var out []sourceRef
	seen := map[string]bool{"Features": true}
	add := func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		if body, ok := b.Sections[name]; ok {
			out = append(out, sourceRef{ID: "section-" + slug(name), Section: name, Bytes: len(body)})
		}
	}
	for _, n := range mainSectionOrder {
		add(n)
	}
	var rest []string
	for n := range b.Sections {
		rest = append(rest, n)
	}
	sort.Strings(rest)
	for _, n := range rest {
		add(n)
	}
	for _, f := range b.Features {
		out = append(out, sourceRef{ID: "feature-" + slug(f.Name), Section: "Features / " + f.Name, Bytes: len(f.Body)})
	}
	return out
}

// componentBrief is the brief text of a node packet. Level 0 is the whole brief;
// from level 1 it is Architecture, Data and Constraints and every feature, whole.
// The component is only recorded, not used to select text.
func componentBrief(r *run, component string, level int) string {
	if level <= 0 {
		return string(r.src)
	}
	var b strings.Builder
	for _, sec := range packetSections {
		if body := r.brief.Sections[sec]; body != "" {
			fmt.Fprintf(&b, "## %s\n\n%s\n\n", sec, body)
		}
	}
	for _, f := range r.brief.Features {
		fmt.Fprintf(&b, "### %s\n\n%s\n\n", f.Name, f.Body)
	}
	return strings.TrimRight(b.String(), "\n")
}

// componentSources is the ids of what componentBrief sends from level 1.
func componentSources(r *run, component string) []string {
	var ids []string
	for _, sec := range packetSections {
		if r.brief.Sections[sec] != "" {
			ids = append(ids, "section-"+slug(sec))
		}
	}
	for _, f := range r.brief.Features {
		ids = append(ids, "feature-"+slug(f.Name))
	}
	return ids
}

// writeSources records what every component of doc receives.
func writeSources(r *run, doc map[string]any) error {
	comps := map[string][]string{}
	for _, c := range objects(doc["components"]) {
		id := fmt.Sprint(c["id"])
		comps[id] = componentSources(r, id)
	}
	return writeJSON(r.path("sources.json"), map[string]any{"sources": briefSources(r.brief), "components": comps})
}
