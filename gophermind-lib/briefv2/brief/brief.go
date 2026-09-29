// Package brief parses a v2 brief: YAML frontmatter validated against the
// embedded schema, fixed H2 sections, and a warn-only scan for secret-looking
// names the frontmatter did not declare.
package brief

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"gophermind/gophermind-lib/briefv2/schema"
)

// InvalidError means the brief itself is wrong (the CLI exits 2), as opposed
// to an I/O failure.
type InvalidError struct{ Reason string }

func (e *InvalidError) Error() string { return e.Reason }

func invalid(format string, a ...any) error {
	return &InvalidError{Reason: fmt.Sprintf(format, a...)}
}

type Secret struct {
	Name    string `json:"name"`
	Purpose string `json:"purpose"`
}

type EnvVar struct {
	Name    string  `json:"name"`
	Purpose string  `json:"purpose"`
	Default *string `json:"default"`
}

type Network struct {
	Host     string `json:"host"`
	Purpose  string `json:"purpose"`
	Critical bool   `json:"critical"`
}

type Budget struct {
	MaxContextTokens int  `json:"max_context_tokens"`
	MaxRevisions     *int `json:"max_revisions"`
}

type Frontmatter struct {
	SpecVersion        string    `json:"spec_version"`
	ID                 string    `json:"id"`
	Title              string    `json:"title"`
	Language           string    `json:"language"`
	Repo               string    `json:"repo"`
	BaseBranch         string    `json:"base_branch"`
	WorkBranch         string    `json:"work_branch"`
	Landing            string    `json:"landing"`
	OnAmbiguity        string    `json:"on_ambiguity"`
	MilestoneApprovals bool      `json:"milestone_approvals"`
	Secrets            []Secret  `json:"secrets"`
	Network            []Network `json:"network"`
	Env                []EnvVar  `json:"env"`
	Budget             *Budget   `json:"budget"`
}

// WorkBranchName is work_branch, or gm/<id> when unset.
func (f Frontmatter) WorkBranchName() string {
	if f.WorkBranch != "" {
		return f.WorkBranch
	}
	return "gm/" + f.ID
}

type Feature struct {
	Name string
	Body string
}

type Brief struct {
	Front    Frontmatter
	Sections map[string]string // H2 text -> body
	Features []Feature         // H3 blocks under ## Features, in order

	body     string
	bodyLine int // 1-based file line of the first body line
}

var requiredSections = []string{"Overview", "Features", "Architecture", "Data", "Constraints", "Out of scope", "Acceptance"}

// Parse splits, validates, and sections a brief. Failures caused by the
// brief's content are *InvalidError.
func Parse(src []byte) (*Brief, error) {
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	text = strings.TrimPrefix(text, string([]byte{0xef, 0xbb, 0xbf}))
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return nil, invalid("brief must start with a --- frontmatter block")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, invalid("frontmatter block is not closed with ---")
	}
	front := strings.Join(lines[1:end], "\n")
	body := strings.Join(lines[end+1:], "\n")

	var doc map[string]any
	if err := yaml.Unmarshal([]byte(front), &doc); err != nil {
		return nil, invalid("frontmatter is not valid YAML: %v", err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	js, err := json.Marshal(doc)
	if err != nil {
		return nil, invalid("frontmatter cannot be converted to JSON: %v", err)
	}
	if err := schema.Validate(schema.KindBrief, js); err != nil {
		return nil, invalid("frontmatter: %v", err)
	}
	b := &Brief{body: body, bodyLine: end + 2}
	if err := json.Unmarshal(js, &b.Front); err != nil {
		return nil, invalid("frontmatter: %v", err)
	}
	secretNames := map[string]bool{}
	for _, s := range b.Front.Secrets {
		secretNames[s.Name] = true
	}
	for _, e := range b.Front.Env {
		if secretNames[e.Name] {
			return nil, invalid("ENV_SECRET_OVERLAP: %s", e.Name)
		}
	}

	sections, features, err := parseSections(body)
	if err != nil {
		return nil, err
	}
	for _, name := range requiredSections {
		if strings.TrimSpace(sections[name]) == "" {
			return nil, invalid("missing or empty section \"## %s\"", name)
		}
	}
	if len(features) == 0 {
		return nil, invalid("\"## Features\" must contain at least one \"### Feature\" block")
	}
	b.Sections, b.Features = sections, features
	return b, nil
}

func parseSections(body string) (map[string]string, []Feature, error) {
	sections := map[string]string{}
	var features []Feature
	var secName, featName string
	var secLines, featLines []string
	var dupErr error

	flushFeat := func() {
		if featName != "" {
			features = append(features, Feature{Name: featName, Body: strings.TrimSpace(strings.Join(featLines, "\n"))})
		}
		featName, featLines = "", nil
	}
	flushSec := func() {
		flushFeat()
		if secName != "" {
			if _, dup := sections[secName]; dup && dupErr == nil {
				dupErr = invalid("duplicate section \"## %s\"", secName)
			}
			sections[secName] = strings.TrimSpace(strings.Join(secLines, "\n"))
		}
		secName, secLines = "", nil
	}

	inFence := false
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
		}
		if !inFence {
			if strings.HasPrefix(line, "## ") {
				flushSec()
				secName = strings.TrimSpace(line[3:])
				continue
			}
			if secName == "Features" && strings.HasPrefix(line, "### ") {
				flushFeat()
				featName = strings.TrimSpace(line[4:])
				secLines = append(secLines, line)
				continue
			}
		}
		secLines = append(secLines, line)
		if featName != "" {
			featLines = append(featLines, line)
		}
	}
	flushSec()
	return sections, features, dupErr
}

// Warning is an undeclared secret-looking token and its 1-based file line.
type Warning struct {
	Token string
	Line  int
}

var (
	tokenRE        = regexp.MustCompile(`\b[A-Z][A-Z0-9_]{2,}\b`)
	secretSuffixes = []string{"_KEY", "_SECRET", "_TOKEN", "_URL", "_DSN", "_PASSWORD", "_PASSPHRASE"}
	secretWords    = []string{"secret", "credential", "environment variable"}
)

func hasSecretSuffix(tok string) bool {
	for _, s := range secretSuffixes {
		if strings.HasSuffix(tok, s) {
			return true
		}
	}
	return false
}

// UndeclaredSecrets scans the body for tokens that look like secret names but
// are declared under neither secrets nor env. A token is flagged when it ends
// in a secret-ish suffix, or when it sits on a line that mentions "secret",
// "credential" or "environment variable" (case-insensitive). Warn-only: it
// never edits the brief and never changes an exit code.
func (b *Brief) UndeclaredSecrets() []Warning {
	declared := map[string]bool{}
	for _, s := range b.Front.Secrets {
		declared[s.Name] = true
	}
	for _, e := range b.Front.Env {
		declared[e.Name] = true
	}
	var out []Warning
	for i, line := range strings.Split(b.body, "\n") {
		lower := strings.ToLower(line)
		wording := false
		for _, w := range secretWords {
			if strings.Contains(lower, w) {
				wording = true
				break
			}
		}
		seen := map[string]bool{}
		for _, tok := range tokenRE.FindAllString(line, -1) {
			if declared[tok] || seen[tok] || !(wording || hasSecretSuffix(tok)) {
				continue
			}
			seen[tok] = true
			out = append(out, Warning{Token: tok, Line: b.bodyLine + i})
		}
	}
	return out
}
