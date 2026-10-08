package planner

import (
	"embed"
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
	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
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
