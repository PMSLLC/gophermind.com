package planner

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/scanner"
	"regexp"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

// Reply text must never reach an error that can be printed or recorded. The
// helpers here turn the errors of the JSON decoder, the schema validator and
// the contract loader into text that names a kind, a location or a size,
// never the text of the reply.

// jsonErr describes a decode failure without quoting the input.
func jsonErr(err error) string {
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syn):
		return fmt.Sprintf("syntax error at byte offset %d", syn.Offset)
	case errors.As(err, &typ):
		return fmt.Sprintf("field %q has the wrong JSON type", typ.Field)
	default:
		return "not valid JSON"
	}
}

// schemaErr describes a schema failure by the location of each failing value
// and, for a missing property, the property's name (which the schema, not the
// reply, supplies).
func schemaErr(ve *jsonschema.ValidationError) string {
	var parts []string
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			loc := "/" + strings.Join(e.InstanceLocation, "/")
			if req, ok := e.ErrorKind.(*kind.Required); ok {
				loc += " is missing " + strings.Join(req.Missing, ", ")
			} else {
				loc += " is not valid"
			}
			parts = append(parts, loc)
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(ve)
	if len(parts) > 8 {
		parts = append(parts[:8], "and more")
	}
	return "the contract fails its schema: " + strings.Join(parts, "; ")
}

var quotedRE = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)

// loadErr rewrites an error from contract.Load. A quoted string is kept only
// when it is an id the document declares; any other quoted string, an unknown
// id for example, is replaced by its length.
func loadErr(err error, doc map[string]any) error {
	var ve *jsonschema.ValidationError
	if errors.As(err, &ve) {
		return errors.New(schemaErr(ve))
	}
	keep := map[string]bool{}
	for _, key := range []string{"types", "functions", "components"} {
		for _, o := range objects(doc[key]) {
			if id, ok := o["id"].(string); ok {
				keep[fmt.Sprintf("%q", id)] = true
			}
		}
	}
	msg := quotedRE.ReplaceAllStringFunc(err.Error(), func(q string) string {
		if keep[q] {
			return q
		}
		return fmt.Sprintf("<%d bytes>", len(q)-2)
	})
	return errors.New(msg)
}

// syntaxErr describes a Go parse failure by position only.
func syntaxErr(err error) string {
	var list scanner.ErrorList
	if errors.As(err, &list) && len(list) > 0 {
		return fmt.Sprintf("syntax error at byte %d", list[0].Pos.Offset)
	}
	return "syntax error"
}
