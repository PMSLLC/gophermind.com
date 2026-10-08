package planner

import (
	"fmt"
	"sort"
	"strings"

	"gophermind/gophermind-lib/briefv2/contract"
)

var trustBoundaries = []string{"none", "internal", "external_input", "external_output", "both"}

// summaryMax bounds every model-written value the approval summary prints.
const summaryMax = 300

// writeGroupSummary adds the "Node groups" section to the plan a person
// approves: how many function nodes answered "not applicable" for each group
// (so a person can sample the reasons), the trust boundaries per component,
// the nodes no requirement names, the warnings Enrich raised, the component
// and root assumptions, and the open questions. Every value that came from a
// model is put on one line and cut.
func writeGroupSummary(s *strings.Builder, c *contract.Contracts, dec decomposed, est enrichedState, cov CoverageFile) {
	s.WriteString("## Node groups\n\n")
	total := 0
	na := map[string]int{}
	perComp := map[string]map[string]int{}
	var noReq []string
	for _, comp := range c.Components {
		perComp[comp.ID] = map[string]int{}
		for _, d := range dec.Components[comp.ID] {
			total++
			id, _ := d["id"].(string)
			for _, g := range naGroups {
				if _, isNA := notApplicable(d[g]); isNA {
					na[g]++
				}
			}
			if sec, ok := d["security"].(map[string]any); ok {
				if _, isNA := notApplicable(sec); !isNA {
					b := strField(sec, "trust_boundary")
					if !isTrustBoundary(b) {
						b = "other"
					}
					perComp[comp.ID][b]++
				}
			}
			if len(requirementIDsFor(cov, id, comp.ID)) == 0 {
				noReq = append(noReq, oneLine(id, 80))
			}
		}
	}
	fmt.Fprintf(s, "Function nodes: %d.\n\n| Group | Answered not applicable |\n|---|---|\n", total)
	for _, g := range naGroups {
		fmt.Fprintf(s, "| %s | %d |\n", g, na[g])
	}
	s.WriteString("\nTrust boundaries per component (nodes whose security was answered):\n\n")
	if len(c.Components) == 0 {
		s.WriteString("None.\n")
	}
	for _, comp := range c.Components {
		var parts []string
		for _, b := range append(append([]string(nil), trustBoundaries...), "other") {
			parts = append(parts, fmt.Sprintf("%s %d", b, perComp[comp.ID][b]))
		}
		fmt.Fprintf(s, "- %s: %s\n", oneLine(comp.ID, 80), strings.Join(parts, ", "))
	}
	sort.Strings(noReq)
	fmt.Fprintf(s, "\nNodes that no requirement names: %s.\n", orNone(firstN(noReq, 10)))

	warns := make([]string, 0, len(est.Warnings))
	for _, w := range est.Warnings {
		warns = append(warns, oneLine(w, summaryMax))
	}
	sort.Strings(warns)
	s.WriteString("\nEnrich warnings:\n\n")
	if len(warns) == 0 {
		s.WriteString("None.\n")
	}
	for _, w := range firstN(warns, 10) {
		fmt.Fprintf(s, "- %s\n", w)
	}

	s.WriteString("\nComponent assumptions:\n\n")
	n := 0
	for _, comp := range c.Components {
		for _, a := range strList(est.Components[comp.ID]["assumptions"]) {
			fmt.Fprintf(s, "- %s: %s\n", oneLine(comp.ID, 80), oneLine(a, summaryMax))
			n++
		}
	}
	if n == 0 {
		s.WriteString("None.\n")
	}
	s.WriteString("\nRoot assumptions:\n\n")
	root := strList(est.Root["assumptions"])
	if len(root) == 0 {
		s.WriteString("None.\n")
	}
	for _, a := range root {
		fmt.Fprintf(s, "- %s\n", oneLine(a, summaryMax))
	}

	open := append(openQuestionNodes(dec), openQuestionStructures(est)...)
	sort.Strings(open)
	for i := range open {
		open[i] = oneLine(open[i], 80)
	}
	fmt.Fprintf(s, "\nOpen questions: %s.\n\n", orNone(firstN(open, 10)))
}

// checksSkipped names the group checks that could not run because the
// repository has no go.mod (or none that states a Go version).
func checksSkipped(f factsFile) string {
	if f.GoVersion == "" {
		return "Checks skipped: go_min against the Go version in go.mod (the repository has no go.mod with a go line)."
	}
	return "Checks skipped: none."
}

func isTrustBoundary(b string) bool {
	for _, t := range trustBoundaries {
		if t == b {
			return true
		}
	}
	return false
}

func firstN(list []string, n int) []string {
	if len(list) > n {
		return list[:n]
	}
	return list
}
