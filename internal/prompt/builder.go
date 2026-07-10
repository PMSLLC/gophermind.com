package prompt

import (
	_ "embed"
	"os"
)

// defaultTemplate is the built-in enhanced system prompt (role/workflow/tools/
// safety/rules + project_context/skills injection points).
//
//go:embed default.md
var defaultTemplate string

// Builder composes a system prompt from a template plus injected context values.
type Builder struct {
	template *Template
	context  map[string]string
}

// NewBuilder returns a Builder over the embedded default template.
func NewBuilder() (*Builder, error) {
	t, err := ParseTemplate(defaultTemplate)
	if err != nil {
		return nil, err
	}
	return &Builder{template: t, context: map[string]string{}}, nil
}

// LoadBuilder returns a Builder over a custom template file.
func LoadBuilder(path string) (*Builder, error) {
	t, err := Load(path)
	if err != nil {
		return nil, err
	}
	return &Builder{template: t, context: map[string]string{}}, nil
}

// WithContext sets a context variable for {{.Key}} injection and returns the
// builder for chaining.
func (b *Builder) WithContext(key, value string) *Builder {
	b.context[key] = value
	return b
}

// Build renders the final system prompt string.
func (b *Builder) Build() string {
	return Build(b.template, b.context)
}

// writeFile is a tiny helper used by tests (and callers writing a template out).
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
