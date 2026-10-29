package brief

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"gophermind/gophermind-lib/build/schema"
)

// Problem is one thing wrong with a brief. Messages never carry the brief's
// own text, only names and rules, so they are safe to show or log.
type Problem struct {
	// Line is the 1-based file line the problem is on, or 0 when it is not
	// tied to one line (a section that is missing altogether).
	Line    int    `json:"line"`
	Message string `json:"message"`
}

// splitFrontmatter cuts a brief into its frontmatter text and body. bodyLine
// is the file line of the first body line. The problem is non-nil when the
// brief has no usable frontmatter block.
func splitFrontmatter(src []byte) (front, body string, bodyLine int, p *Problem) {
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	text = strings.TrimPrefix(text, string([]byte{0xef, 0xbb, 0xbf}))
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return "", "", 0, &Problem{Line: 1, Message: "brief must start with a --- frontmatter block"}
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return "", "", 0, &Problem{Line: 1, Message: "frontmatter block is not closed with ---"}
	}
	return strings.Join(lines[1:end], "\n"), strings.Join(lines[end+1:], "\n"), end + 2, nil
}

var quotedRE = regexp.MustCompile(`'([^']+)'`)

// Check lists every problem in a brief, in file order, where Parse stops at
// the first. It returns nil exactly when Parse accepts the brief.
func Check(src []byte) []Problem {
	front, body, bodyLine, p := splitFrontmatter(src)
	if p != nil {
		return []Problem{*p}
	}
	frontLines := strings.Split(front, "\n") // frontLines[0] is file line 2

	var doc map[string]any
	if err := yaml.Unmarshal([]byte(front), &doc); err != nil {
		return []Problem{{Line: 2, Message: fmt.Sprintf("frontmatter is not valid YAML: %v", err)}}
	}
	if doc == nil {
		doc = map[string]any{}
	}
	var out []Problem
	js, err := json.Marshal(doc)
	if err != nil {
		return []Problem{{Line: 2, Message: fmt.Sprintf("frontmatter cannot be converted to JSON: %v", err)}}
	}
	violations, err := schema.Violations(schema.KindBrief, js)
	if err != nil {
		return []Problem{{Line: 2, Message: fmt.Sprintf("frontmatter: %v", err)}}
	}
	for _, v := range violations {
		msg := "frontmatter: " + v.Message
		if v.Path != "" {
			msg = "frontmatter " + v.Path + ": " + v.Message
		}
		out = append(out, Problem{Line: violationLine(frontLines, doc, v), Message: msg})
	}
	if len(violations) == 0 {
		var f Frontmatter
		if err := json.Unmarshal(js, &f); err != nil {
			out = append(out, Problem{Line: 2, Message: fmt.Sprintf("frontmatter: %v", err)})
		} else {
			out = append(out, frontmatterProblems(f, frontLines)...)
		}
	}

	out = append(out, bodyProblems(body, bodyLine)...)
	out = append(out, sectionProblems(body, bodyLine)...)
	sortByLine(out)
	return out
}

// violationLine finds the frontmatter line a schema violation points at: the
// top-level key in its path, or a key the message names (an unknown key).
func violationLine(frontLines []string, doc map[string]any, v schema.Violation) int {
	if v.Path != "" {
		key := strings.SplitN(strings.TrimPrefix(v.Path, "/"), "/", 2)[0]
		if n := keyLine(frontLines, key); n > 0 {
			return n
		}
	}
	for _, m := range quotedRE.FindAllStringSubmatch(v.Message, -1) {
		if _, ok := doc[m[1]]; ok {
			if n := keyLine(frontLines, m[1]); n > 0 {
				return n
			}
		}
	}
	return 2
}

// keyLine is the file line of a top-level `key:` in the frontmatter, or 0.
func keyLine(frontLines []string, key string) int {
	for i, l := range frontLines {
		if strings.HasPrefix(l, key+":") {
			return i + 2
		}
	}
	return 0
}

// nameLine is the file line of the nth (1-based) `name: NAME` entry, or 2.
func nameLine(frontLines []string, name string, nth int) int {
	seen := 0
	for i, l := range frontLines {
		t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "- "))
		if t == "name: "+name {
			seen++
			if seen == nth {
				return i + 2
			}
		}
	}
	return 2
}

// frontmatterProblems are the rules the schema cannot express: names that
// overlap, repeat, or belong to the harness.
func frontmatterProblems(f Frontmatter, frontLines []string) []Problem {
	var out []Problem
	secret := map[string]bool{}
	for _, s := range f.Secrets {
		secret[s.Name] = true
	}
	for _, e := range f.Env {
		if secret[e.Name] {
			out = append(out, Problem{Line: nameLine(frontLines, e.Name, 1), Message: fmt.Sprintf("ENV_SECRET_OVERLAP: %s is declared as both an env setting and a secret", e.Name)})
		}
	}
	seen := map[string]int{}
	for _, s := range f.Secrets {
		seen[s.Name]++
		if seen[s.Name] == 2 {
			out = append(out, Problem{Line: nameLine(frontLines, s.Name, 2), Message: fmt.Sprintf("secret %s is declared twice", s.Name)})
		}
	}
	seen = map[string]int{}
	for _, e := range f.Env {
		seen[e.Name]++
		if seen[e.Name] == 2 {
			out = append(out, Problem{Line: nameLine(frontLines, e.Name, 2), Message: fmt.Sprintf("env %s is declared twice", e.Name)})
		}
	}
	for _, s := range f.Secrets {
		if IsReservedName(s.Name) {
			out = append(out, Problem{Line: nameLine(frontLines, s.Name, 1), Message: fmt.Sprintf("%s is reserved for the harness", s.Name)})
		}
	}
	for _, e := range f.Env {
		if IsReservedName(e.Name) {
			out = append(out, Problem{Line: nameLine(frontLines, e.Name, 1), Message: fmt.Sprintf("%s is reserved for the harness", e.Name)})
		}
	}
	return out
}

// sectionProblems are repeated sections, required sections that are missing
// or empty, and a Features section with no feature in it.
func sectionProblems(body string, firstLine int) []Problem {
	var out []Problem
	headingLine := map[string]int{}
	inFence := false
	for i, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.HasPrefix(line, "## ") {
			continue
		}
		name := strings.TrimSpace(line[3:])
		if _, dup := headingLine[name]; dup {
			out = append(out, Problem{Line: firstLine + i, Message: fmt.Sprintf("duplicate section \"## %s\"", name)})
			continue
		}
		headingLine[name] = firstLine + i
	}
	sections, features, _ := parseSections(body)
	for _, name := range requiredSections {
		if strings.TrimSpace(sections[name]) == "" {
			out = append(out, Problem{Line: headingLine[name], Message: fmt.Sprintf("missing or empty section \"## %s\"", name)})
		}
	}
	if len(features) == 0 && strings.TrimSpace(sections["Features"]) != "" {
		out = append(out, Problem{Line: headingLine["Features"], Message: "\"## Features\" must contain at least one \"### Feature\" block"})
	}
	return out
}

// sortByLine orders problems by line, keeping the original order for equal
// lines. Problems with no line (0) come last.
func sortByLine(ps []Problem) {
	key := func(p Problem) int {
		if p.Line == 0 {
			return int(^uint(0) >> 1)
		}
		return p.Line
	}
	for i := 1; i < len(ps); i++ {
		for j := i; j > 0 && key(ps[j]) < key(ps[j-1]); j-- {
			ps[j], ps[j-1] = ps[j-1], ps[j]
		}
	}
}
