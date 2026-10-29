// Package schema embeds the three JSON Schemas and validates documents
// against them. Every node write, contract write, and brief load goes through
// Validate: an invalid document is a planner bug, not a runtime condition.
package schema

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

//go:embed task-node.schema.json brief-frontmatter.schema.json contract.schema.json
var files embed.FS

// Kind selects which embedded schema Validate checks against.
type Kind string

const (
	KindBrief    Kind = "brief"
	KindNode     Kind = "node"
	KindContract Kind = "contract"
)

var sources = map[Kind]struct{ file, id string }{
	KindBrief:    {"brief-frontmatter.schema.json", "https://gophermind.local/schema/brief-frontmatter/2.0"},
	KindNode:     {"task-node.schema.json", "https://gophermind.local/schema/task-node/2.0"},
	KindContract: {"contract.schema.json", "https://gophermind.local/schema/contract/2.0"},
}

var (
	once     sync.Once
	compiled map[Kind]*jsonschema.Schema
	initErr  error
)

func compile() {
	c := jsonschema.NewCompiler()
	for _, s := range sources {
		raw, err := files.ReadFile(s.file)
		if err != nil {
			initErr = err
			return
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			initErr = fmt.Errorf("schema %s: %w", s.file, err)
			return
		}
		if err := c.AddResource(s.id, doc); err != nil {
			initErr = fmt.Errorf("schema %s: %w", s.file, err)
			return
		}
	}
	compiled = map[Kind]*jsonschema.Schema{}
	for k, s := range sources {
		sch, err := c.Compile(s.id)
		if err != nil {
			initErr = fmt.Errorf("schema %s: %w", s.file, err)
			return
		}
		compiled[k] = sch
	}
}

// Validate checks the JSON document doc against the schema for kind. The
// returned error's text names the failing property path (for example
// "missing property 'spec_version'").
func Validate(kind Kind, doc []byte) error {
	once.Do(compile)
	if initErr != nil {
		return initErr
	}
	sch, ok := compiled[kind]
	if !ok {
		return fmt.Errorf("schema: unknown kind %q", kind)
	}
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(doc))
	if err != nil {
		return fmt.Errorf("schema: not valid JSON: %w", err)
	}
	return sch.Validate(v)
}

// Violation is one place where a document breaks its schema.
type Violation struct {
	// Path is a JSON pointer such as "/landing"; empty for the document itself.
	Path    string
	Message string
}

// Violations lists every way doc breaks the schema for kind, not just the
// first. It returns nil for a valid document. An error means the check itself
// could not run (unknown kind, doc is not JSON), not that doc is invalid.
func Violations(kind Kind, doc []byte) ([]Violation, error) {
	err := Validate(kind, doc)
	if err == nil {
		return nil, nil
	}
	ve, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return nil, err
	}
	p := message.NewPrinter(language.English)
	var out []Violation
	seen := map[Violation]bool{}
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			v := Violation{Message: e.ErrorKind.LocalizedString(p)}
			if len(e.InstanceLocation) > 0 {
				v.Path = "/" + strings.Join(e.InstanceLocation, "/")
			}
			if !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(ve)
	return out, nil
}

// Raw returns the embedded schema file for kind, byte for byte. The Contract
// prompt shows it to the model so the reply can be validated against the same
// text.
func Raw(kind Kind) ([]byte, error) {
	s, ok := sources[kind]
	if !ok {
		return nil, fmt.Errorf("schema: unknown kind %q", kind)
	}
	return files.ReadFile(s.file)
}
