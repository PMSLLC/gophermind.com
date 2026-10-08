package planner

import (
	"embed"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"gophermind/gophermind-lib/briefv2/provider"
)

//go:embed prompts/*.md
var promptFS embed.FS

// render fills prompts/<name>.md. A field the template names and data lacks
// is an error, never an empty string in a prompt.
func render(name string, data map[string]string) (string, error) {
	raw, err := promptFS.ReadFile("prompts/" + name + ".md")
	if err != nil {
		return "", err
	}
	t, err := template.New(name).Option("missingkey=error").Parse(string(raw))
	if err != nil {
		return "", err
	}
	clean := make(map[string]string, len(data))
	for k, v := range data {
		if trustedPromptKeys[k] {
			clean[k] = v
		} else {
			clean[k] = neutralizePrompt(v)
		}
	}
	var b strings.Builder
	if err := t.Execute(&b, clean); err != nil {
		return "", err
	}
	return b.String(), nil
}

// closingTagRE finds the closing tags the prompt files use. Only closing tags
// name a data section: placeholders such as <name>, <type> or <id> in the
// prompt text never have one and must reach the model unchanged.
var closingTagRE = regexp.MustCompile(`</([a-z_]+)>`)

// packerSectionTags are the section tags of the executor's node prompts
// (packer.sectionTagRE). Planner text can reach those prompts through the
// plan, so they are broken here too.
var packerSectionTags = []string{"file", "signature", "contract", "dependency_signatures", "constraints",
	"guidance", "revision_notes", "tests", "previous_failure", "attempts"}

// promptDataTags lists, sorted, every tag name closed in a prompt file.
func promptDataTags() []string {
	seen := map[string]bool{}
	entries, _ := promptFS.ReadDir("prompts")
	for _, e := range entries {
		raw, err := promptFS.ReadFile("prompts/" + e.Name())
		if err != nil {
			continue
		}
		for _, m := range closingTagRE.FindAllStringSubmatch(string(raw), -1) {
			seen[m[1]] = true
		}
	}
	for _, t := range packerSectionTags {
		seen[t] = true
	}
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

var dataTagRE = regexp.MustCompile(`(?i)<\s*(/?)\s*(` + strings.Join(promptDataTags(), "|") + `)\s*>`)

// neutralizePrompt breaks every data-section tag in untrusted text so that a brief,
// an answer or a model-written node cannot close the section it sits in and
// speak as the prompt. A backslash is put after the bracket, which a model
// still reads as the same words. Text without such a tag is returned as is.
func neutralizePrompt(s string) string { return dataTagRE.ReplaceAllString(s, `<\$1$2>`) }

// trustedPromptKeys are the data keys the planner itself builds: fixed text,
// numbers and lists of names, never the brief, an answer, a fact, a defect
// message or a model reply.
var trustedPromptKeys = map[string]bool{
	"Harness": true, "ItemSchemas": true, "Fields": true, "FactKeys": true,
	"MaxQuestions": true, "NextID": true, "Kind": true, "Count": true, "UnresolvedCount": true,
}

// harnessContract is the shared description of how root test commands are run
// (prompts/harness.md), put into every prompt that asks for one.
func harnessContract() string {
	raw, err := promptFS.ReadFile("prompts/harness.md")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// request is the shape of every planner call: a system line naming the stage
// (the offline fixture provider keys its canned replies on it) and the prompt.
func request(stage, prompt string, maxTokens int) provider.Request {
	return provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: stagePrefixSystem + stage},
			{Role: provider.RoleUser, Content: prompt},
		},
		MaxTokens:   maxTokens,
		Temperature: 0.2,
	}
}

// Output budgets per stage, in tokens. They bound one reply, not the plan:
// a large brief makes more calls, never longer ones.
const (
	maxTokensClarify  = 4096
	maxTokensContract = 8000
	// The outline lists every component and type of a large brief in one reply,
	// so it gets a larger budget, and a larger cap when the router grows it.
	maxTokensOutline   = 16000
	maxGrownOutline    = 32768
	maxTokensDecompose = 8000
	maxTokensCoverage  = 6000
	maxTokensFill      = 8000
	maxTokensTestwrite = 6000

	maxTokensEnrich          = 8000
	maxTokensEnrichStructure = 1500
)
