package brief

import (
	"fmt"
	"regexp"
	"strings"
)

// maxLintLines is where a brief stops being "a few hundred lines at most".
const maxLintLines = 400

var (
	backtickRE   = regexp.MustCompile("`([^`]+)`")
	goRunRE      = regexp.MustCompile(`\bgo\s+(run|install)\b`)
	goBuildOutRE = regexp.MustCompile(`\bgo\s+build\b[^|;&]*\s-o\b`)
	dotSlashRE   = regexp.MustCompile(`(^|[|;&]\s*)\./\S`)
	envAssignRE  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=\S*\s+`)
	programRE    = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
)

// notProgram are commands an acceptance bullet may start with that are not
// the program under test.
var notProgram = map[string]bool{
	"go": true, "curl": true, "printf": true, "echo": true, "cat": true, "grep": true, "diff": true,
	"test": true, "sleep": true, "jq": true, "sort": true, "wc": true, "head": true, "tail": true,
}

type numbered struct {
	n    int
	text string
}

// Lint reports practices from section 5 of the brief specification that a
// brief can break and still be valid, and that make it plan or build badly.
// They are warnings: Check and Parse do not refuse a brief for them. It
// returns nil for text Check already rejects as having no frontmatter.
func Lint(src []byte) []Problem {
	_, body, bodyLine, p := splitFrontmatter(src)
	if p != nil {
		return nil
	}
	sections, headings := sectionLines(body, bodyLine)
	var out []Problem

	// An acceptance command must run the built binary by name.
	hasBinary := false
	for _, l := range sections["Acceptance"] {
		bullet, ok := BulletText(l.text)
		if !ok || !strings.HasPrefix(bullet, "`") {
			continue
		}
		for _, m := range backtickRE.FindAllStringSubmatch(bullet, -1) {
			if goRunRE.MatchString(m[1]) || goBuildOutRE.MatchString(m[1]) || dotSlashRE.MatchString(m[1]) {
				out = append(out, Problem{Line: l.n, Message: "acceptance runs the program with go run, go install, go build -o or a ./path; run the built binary by name, because only binaries built from cmd/ are put on PATH (spec section 5)"})
				break
			}
		}
		if first := backtickRE.FindStringSubmatch(bullet); first != nil && isProgramCommand(first[1]) {
			hasBinary = true
		}
	}

	var acceptance strings.Builder
	for _, l := range sections["Acceptance"] {
		acceptance.WriteString(l.text + "\n")
	}
	for _, want := range []string{"go build ./...", "go vet ./...", "go test"} {
		if !strings.Contains(acceptance.String(), want) {
			out = append(out, Problem{Line: headings["Acceptance"], Message: fmt.Sprintf("Acceptance does not include %q; the spec asks for go build, go vet and go test (section 5)", want)})
		}
	}

	arch := joinLines(sections["Architecture"])
	if hasBinary && !strings.Contains(arch, "cmd/") {
		out = append(out, Problem{Line: headings["Architecture"], Message: "Acceptance runs a program but Architecture does not put its main package in cmd/<name>/; without a binary built from cmd/ the acceptance tests are refused as vacuous (spec section 5)"})
	}
	prose := strings.ToLower(joinLines(sections["Features"]) + "\n" + arch)
	if (strings.Contains(prose, "standard input") || strings.Contains(prose, "stdin")) && !strings.Contains(arch, "io.Reader") {
		out = append(out, Problem{Line: headings["Architecture"], Message: "the brief reads standard input but Architecture does not say the code takes an io.Reader; the planner refuses a function that reads standard input without one (spec section 5)"})
	}

	if n := strings.Count(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n"); n > maxLintLines {
		out = append(out, Problem{Message: fmt.Sprintf("the brief is %d lines long; the spec asks for a few hundred at most, and a long brief plans into large functions", n)})
	}
	sortByLine(out)
	return out
}

// isProgramCommand reports whether cmd, the first code span of an acceptance
// bullet, runs the program under test: the last stage of its pipeline starts
// with a name that is not one of the usual shell tools.
func isProgramCommand(cmd string) bool {
	stage := cmd
	if i := strings.LastIndex(stage, "|"); i >= 0 {
		stage = stage[i+1:]
	}
	stage = strings.TrimSpace(stage)
	for envAssignRE.MatchString(stage) {
		stage = envAssignRE.ReplaceAllString(stage, "")
	}
	word := strings.Fields(stage + " ")[0]
	return programRE.MatchString(word) && !notProgram[word]
}

func joinLines(ls []numbered) string {
	var b strings.Builder
	for _, l := range ls {
		b.WriteString(l.text + "\n")
	}
	return b.String()
}

// sectionLines groups the body's lines by "## " section, keeping each line's
// file line, and returns the file line of each section's heading.
func sectionLines(body string, firstLine int) (map[string][]numbered, map[string]int) {
	sections := map[string][]numbered{}
	headings := map[string]int{}
	section := ""
	inFence := false
	for i, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
		}
		if !inFence && strings.HasPrefix(line, "## ") {
			section = strings.TrimSpace(line[3:])
			if _, seen := headings[section]; !seen {
				headings[section] = firstLine + i
			}
			continue
		}
		if section != "" {
			sections[section] = append(sections[section], numbered{firstLine + i, line})
		}
	}
	return sections, headings
}
