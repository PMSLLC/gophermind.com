package executor

import (
	"fmt"
	"sort"
	"strings"

	"gophermind/gophermind-lib/briefv2/runner"
)

// maxAttributionLines caps the lines one node is handed for its next prompt.
const maxAttributionLines = 30

// Attribution is what an integration failure is pinned on. It is computed by
// code from file paths and test names only, never by a model (spec 8.3, R10).
type Attribution struct {
	Nodes        []string            // leaf ids, sorted, unique
	Unattributed []string            // "file:line" for a record no node owns, "<kind>" or "<dir> <kind>" when there is no location
	Lines        map[string][]string // leaf id -> failure lines that mention its own files or tests, for its next prompt
}

// Attribute maps every failure of res to the leaves that own the files and
// tests it names:
//
//   - a build or vet record maps by the file of its location to the leaf whose
//     contract file, stub file or test file it is;
//   - a test record maps by (package dir, top-level test name) to the leaf that
//     owns that test, and only when no owner is found by the file of its
//     locations;
//   - anything no rule resolves is unattributable, including a failure that
//     names neither a test nor a file: guessing would burn the repair budget on
//     the wrong leaf.
//
// A leaf's lines hold only its own files and tests (need to know). The result
// says nothing about a leaf's status: re-opening a verified leaf is the repair
// loop's explicit act, never a side effect of attribution.
func (p *Plan) Attribute(res checkResult) Attribution {
	byPath := map[string]string{}
	byTest := map[[2]string]string{}
	for _, l := range p.Leaves {
		for _, f := range []string{l.File, l.StubFile, l.TestFile} {
			if f != "" {
				byPath[f] = l.ID
			}
		}
		byTest[[2]string{l.Dir, l.TestFunc}] = l.ID
	}

	nodes := map[string]bool{}
	lines := map[string][]string{}
	seenLine := map[string]map[string]bool{}
	var unatt []string
	seenUnatt := map[string]bool{}
	addLine := func(id, line string) {
		if seenLine[id] == nil {
			seenLine[id] = map[string]bool{}
		}
		if seenLine[id][line] || len(lines[id]) >= maxAttributionLines {
			return
		}
		seenLine[id][line] = true
		lines[id] = append(lines[id], line)
	}
	addUnatt := func(s string) {
		if !seenUnatt[s] {
			seenUnatt[s] = true
			unatt = append(unatt, s)
		}
	}
	locText := func(l runner.Location) string { return fmt.Sprintf("%s:%d", l.File, l.Line) }
	// byLocations resolves every location; it reports whether there were any.
	byLocations := func(locs []runner.Location) {
		for _, l := range locs {
			if id, ok := byPath[l.File]; ok {
				nodes[id] = true
				addLine(id, locText(l))
			} else {
				addUnatt(locText(l))
			}
		}
	}

	for _, f := range res.Failures {
		isTest := f.Kind != runner.ClassBuild && f.Kind != runner.ClassVet
		if !isTest || len(f.Names) == 0 {
			if len(f.Locations) == 0 {
				addUnatt(noLocation(f))
				continue
			}
			byLocations(f.Locations)
			continue
		}
		var unowned []string
		for _, n := range f.Names {
			top, _, _ := strings.Cut(n, "/")
			if id, ok := byTest[[2]string{f.Dir, top}]; ok {
				nodes[id] = true
				addLine(id, n)
			} else {
				unowned = append(unowned, n)
			}
		}
		if len(unowned) == 0 {
			continue
		}
		if len(f.Locations) == 0 {
			addUnatt(noLocation(f))
			continue
		}
		byLocations(f.Locations)
	}

	out := Attribution{Unattributed: unatt, Lines: lines}
	for id := range nodes {
		out.Nodes = append(out.Nodes, id)
	}
	sort.Strings(out.Nodes)
	return out
}

func noLocation(f checkFailure) string {
	if f.Dir == "" {
		return f.Kind
	}
	return f.Dir + " " + f.Kind
}
