// Package planner turns a validated brief into a task tree: it parses the
// brief's requirements, runs the planning stages against the model router, and
// refuses to let a plan reach approval while a requirement is uncovered.
package planner

import (
	"fmt"
	"strings"

	"gophermind/gophermind-lib/build/brief"
)

// ReqKind says which part of the brief a requirement came from.
type ReqKind string

const (
	ReqConstraint ReqKind = "constraint"
	ReqAcceptance ReqKind = "acceptance"
	ReqFeature    ReqKind = "feature"
	// ReqTest is a bullet of the brief's Tests section: a test obligation the
	// test writer meets, never a gap coverage looks for a node for.
	ReqTest ReqKind = "test"
)

// Requirement is one obligation the brief states. IDs are C1.., A1.., F1.., T1..,
// numbered per kind in file order, so they are stable for a given brief text.
type Requirement struct {
	ID   string  `json:"id"`
	Kind ReqKind `json:"kind"`
	Name string  `json:"name,omitempty"` // feature heading; features only
	Text string  `json:"text"`           // verbatim; bullet marker removed
	Line int     `json:"line"`           // 1-based line in the brief file
}

// IsTestRule is true for a Constraint that governs how tests are written
// rather than what the code does. The test writer applies it; coverage
// does not look for a node or a root test.
func (r Requirement) IsTestRule() bool {
	if r.Kind != ReqConstraint {
		return false
	}
	t := strings.ToLower(strings.TrimSpace(r.Text))
	for _, p := range []string{"test", "tests", "unit test", "unit tests", "benchmark", "benchmarks"} {
		if t == p || strings.HasPrefix(t, p+" ") {
			return true
		}
	}
	return false
}

// ParseRequirements scans a brief file: every top-level bullet under
// "## Constraints", "## Acceptance" and "## Tests", and every "### " heading under
// "## Features", is one requirement. It is code, not a model, so the list a
// plan is checked against cannot drift from the brief. Returned in file order.
func ParseRequirements(src []byte) []Requirement {
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	text = strings.TrimPrefix(text, string([]byte{0xef, 0xbb, 0xbf}))
	lines := strings.Split(text, "\n")
	start := 0
	if lines[0] == "---" {
		for i := 1; i < len(lines); i++ {
			if lines[i] == "---" {
				start = i + 1
				break
			}
		}
	}

	out := []Requirement{}
	counts := map[ReqKind]int{}
	prefix := map[ReqKind]string{ReqConstraint: "C", ReqAcceptance: "A", ReqFeature: "F", ReqTest: "T"}
	var cur *Requirement
	var body []string
	flush := func() {
		if cur == nil {
			return
		}
		cur.Text = strings.TrimSpace(strings.Join(body, "\n"))
		out = append(out, *cur)
		cur, body = nil, nil
	}
	open := func(kind ReqKind, line int) {
		flush()
		counts[kind]++
		cur = &Requirement{ID: fmt.Sprintf("%s%d", prefix[kind], counts[kind]), Kind: kind, Line: line}
	}

	section := ""
	inFence := false
	for i := start; i < len(lines); i++ {
		line := lines[i]
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
		}
		if !inFence && strings.HasPrefix(line, "## ") {
			flush()
			section = strings.TrimSpace(line[3:])
			continue
		}
		switch section {
		case "Features":
			if !inFence && strings.HasPrefix(line, "### ") {
				open(ReqFeature, i+1)
				cur.Name = strings.TrimSpace(line[4:])
				continue
			}
			if cur != nil {
				body = append(body, line)
			}
		case "Constraints", "Acceptance", "Tests":
			kind := ReqConstraint
			switch section {
			case "Acceptance":
				kind = ReqAcceptance
			case "Tests":
				kind = ReqTest
			}
			switch {
			case !inFence && isBullet(line):
				open(kind, i+1)
				txt, _ := brief.BulletText(line)
				body = append(body, txt)
			case t == "":
				// blank lines are dropped from a bullet's text
			case line[0] == ' ' || line[0] == '\t':
				if cur != nil {
					body = append(body, strings.TrimRight(line, " \t"))
				}
			default:
				// A non-blank line at column 0 that is not a bullet ends the
				// current bullet and is not a requirement itself.
				flush()
			}
		}
	}
	flush()
	return out
}

// CoverageRequirements is reqs without the kind-test ones: the requirements a
// plan's coverage accounts for, one Covered entry or gap each. A Tests bullet
// is the test writer's obligation and never appears in coverage.json.
func CoverageRequirements(reqs []Requirement) []Requirement {
	out := make([]Requirement, 0, len(reqs))
	for _, q := range reqs {
		if q.Kind != ReqTest {
			out = append(out, q)
		}
	}
	return out
}

func isBullet(line string) bool {
	_, ok := brief.BulletText(line)
	return ok
}
