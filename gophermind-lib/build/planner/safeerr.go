package planner

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/scanner"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"

	"gophermind/gophermind-lib/build/schema"
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
// maxSchemaPointers is how many pointers a schema failure names; the rest are counted.
const maxSchemaPointers = 5

func schemaErr(ve *jsonschema.ValidationError) string {
	var parts []string
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			loc := "/" + strings.Join(e.InstanceLocation, "/")
			if req, ok := e.ErrorKind.(*kind.Required); ok {
				loc += " is missing " + strings.Join(req.Missing, ", ")
			} else if _, ok := e.ErrorKind.(*kind.AdditionalProperties); ok {
				loc += unknownPropText(e)
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
	if len(parts) > maxSchemaPointers {
		parts = append(parts[:maxSchemaPointers], fmt.Sprintf("and %d more", len(parts)-maxSchemaPointers))
	}
	return "the contract fails its schema: " + strings.Join(parts, "; ")
}

// maxAllowedProps is how many allowed property names an unknown-property failure lists.
const maxAllowedProps = 12

// allowedProps lists the properties the schema allows for the object that e
// failed, read from the embedded schema and never from the reply. It is nil
// when the object cannot be found.
func allowedProps(e *jsonschema.ValidationError) []string {
	base, ptr, ok := strings.Cut(e.SchemaURL, "#")
	if !ok {
		return nil
	}
	for _, k := range []schema.Kind{schema.KindContract, schema.KindNode, schema.KindBrief} {
		raw, err := schema.Raw(k)
		if err != nil {
			continue
		}
		var node any
		if json.Unmarshal(raw, &node) != nil {
			continue
		}
		if m, _ := node.(map[string]any); m["$id"] != base {
			continue
		}
		for _, seg := range strings.Split(ptr, "/") {
			if seg == "" {
				continue
			}
			seg = strings.NewReplacer("~1", "/", "~0", "~").Replace(seg)
			switch n := node.(type) {
			case map[string]any:
				node = n[seg]
			case []any:
				i, err := strconv.Atoi(seg)
				if err != nil || i < 0 || i >= len(n) {
					return nil
				}
				node = n[i]
			default:
				return nil
			}
		}
		m, _ := node.(map[string]any)
		props, _ := m["properties"].(map[string]any)
		if len(props) == 0 {
			return nil
		}
		names := make([]string, 0, len(props))
		for n := range props {
			names = append(names, n)
		}
		return orderByFile(names, raw)
	}
	return nil
}

// orderByFile sorts names by where their key first appears in raw, which is
// the order the schema declares them in (a decoded map has lost it).
func orderByFile(names []string, raw []byte) []string {
	pos := map[string]int{}
	for _, n := range names {
		pos[n] = bytes.Index(raw, []byte(`"`+n+`":`))
	}
	sort.SliceStable(names, func(i, j int) bool { return pos[names[i]] < pos[names[j]] })
	return names
}

// unknownPropText is the text for a failed additionalProperties: it says the
// object has a property the schema does not allow and lists the allowed ones.
// It never names the property the reply supplied.
func unknownPropText(e *jsonschema.ValidationError) string {
	names := allowedProps(e)
	if len(names) == 0 {
		return " has a property the schema does not allow"
	}
	extra := ""
	if len(names) > maxAllowedProps {
		extra = fmt.Sprintf(", and %d more", len(names)-maxAllowedProps)
		names = names[:maxAllowedProps]
	}
	return " has a property the schema does not allow (allowed: " + strings.Join(names, ", ") + extra + ")"
}

var quotedRE = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)

// loadErr rewrites an error from contract.Load. A quoted string is kept only
// when it is an id the document declares; any other quoted string, an unknown
// id for example, is replaced by its length.
func loadErr(err error, doc map[string]any) error {
	var ve *jsonschema.ValidationError
	if errors.As(err, &ve) {
		return errors.New(nodeSchemaErr(ve, doc))
	}
	keep := map[string]string{}
	for _, key := range []string{"types", "functions", "components"} {
		for _, o := range objects(doc[key]) {
			if id, ok := o["id"].(string); ok {
				keep[fmt.Sprintf("%q", id)] = boundedID(id)
			}
		}
	}
	msg := quotedRE.ReplaceAllStringFunc(err.Error(), func(q string) string {
		if b, ok := keep[q]; ok {
			return b
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
