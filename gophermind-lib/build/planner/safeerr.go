package planner

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/scanner"
	"go/token"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

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
		if strings.Contains(syn.Error(), "escape") {
			return fmt.Sprintf("syntax error at byte offset %d; a backslash in a string must be written as two backslashes", syn.Offset)
		}
		return fmt.Sprintf("syntax error at byte offset %d", syn.Offset)
	case errors.As(err, &typ):
		return fmt.Sprintf("field %q has the wrong JSON type", typ.Field)
	default:
		if t, ok := unknownFieldText(err); ok {
			return "the reply holds " + t
		}
		return "not valid JSON"
	}
}

// jsonErrIn is jsonErr for a failure whose input is at hand. A syntax error
// is located by line and column and described in plain words, so a repair
// prompt can say where the reply broke. It reads only positions and the
// decoder's error kind from text, and never copies any of it into the result.
func jsonErrIn(err error, text string) string {
	var syn *json.SyntaxError
	switch {
	case errors.As(err, &syn):
		// Offset counts the bytes read, the offending byte is the last one.
		line, col := lineCol(text, int(syn.Offset)-1)
		return fmt.Sprintf("syntax error at line %d, column %d (byte offset %d); %s",
			line, col, syn.Offset, syntaxKind(syn.Error()))
	case errors.Is(err, io.ErrUnexpectedEOF):
		line, col := lineCol(text, len(text))
		return fmt.Sprintf("syntax error at line %d, column %d; the reply ends before the JSON is complete: a string, array or object is still open (was the reply cut short?)", line, col)
	}
	return jsonErr(err)
}

// syntaxKind turns the decoder's message into advice. It matches on the
// decoder's wording only and never returns any of it.
func syntaxKind(msg string) string {
	switch {
	case strings.Contains(msg, "escape"):
		return "a backslash in a string must be written as two backslashes"
	case strings.Contains(msg, "in string"):
		return `a string opened on this line is not closed; a quote inside a string must be written \" and the closing quote must be present before the line ends`
	case strings.Contains(msg, "unexpected end"):
		return "the reply ends before the JSON is complete"
	case strings.Contains(msg, "after object key:value pair"), strings.Contains(msg, "after array element"):
		return "a comma is missing between two values, or a closing bracket is wrong"
	case strings.Contains(msg, "after object key"):
		return "a colon is missing after a key"
	case strings.Contains(msg, "looking for beginning of object key string"):
		return "a key must be a quoted string (check for a trailing comma or a missing quote)"
	case strings.Contains(msg, "looking for beginning of value"):
		return "a value is expected here (check for a trailing comma or a missing value)"
	case strings.Contains(msg, "after top-level value"):
		return "extra text follows the complete JSON value"
	}
	return "the text here is not valid JSON"
}

// lineCol is the 1-based line and column (in characters) of byte index i.
func lineCol(text string, i int) (int, int) {
	if i < 0 {
		i = 0
	}
	if i > len(text) {
		i = len(text)
	}
	line := 1 + strings.Count(text[:i], "\n")
	start := strings.LastIndex(text[:i], "\n") + 1
	return line, utf8.RuneCountInString(text[start:i]) + 1
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

// syntaxErr describes a Go parse failure by line and column of the first
// error, relative to the source that was parsed. When the parser says
// "expected X, found Y" and both are fixed tokens (punctuation, a keyword, or
// the scanner's newline and EOF), the tokens are appended; the fixed
// "missing ',' in <construct>" messages are kept too. Identifiers, literals and
// any other source text are never copied.
func syntaxErr(err error) string {
	var list scanner.ErrorList
	if errors.As(err, &list) && len(list) > 0 {
		pos := list[0].Pos
		msg := fmt.Sprintf("syntax error at line %d, column %d", pos.Line, pos.Column)
		if detail := syntaxDetail(list[0].Msg); detail != "" {
			msg += ": " + detail
		}
		return msg
	}
	return "syntax error"
}

var (
	expectedFoundRE = regexp.MustCompile(`^expected (\S+?), found (\S+)$`)
	missingCommaRE  = regexp.MustCompile(`^missing ',' in (composite literal|argument list|parameter list|type argument list|type parameter list)$`)
)

// syntaxDetail returns the safe part of a parser message, or "".
func syntaxDetail(msg string) string {
	if missingCommaRE.MatchString(msg) {
		return msg
	}
	if m := expectedFoundRE.FindStringSubmatch(msg); m != nil && fixedToken(m[1]) && fixedToken(m[2]) {
		return "expected " + m[1] + ", found " + m[2]
	}
	return ""
}

// fixedToken reports whether t is a quoted single punctuation token or Go
// keyword, or the scanner's newline or EOF. Anything else (identifiers,
// literals, words such as operand) is source or parser prose and is refused.
func fixedToken(t string) bool {
	switch t {
	case "newline", "'EOF'":
		return true
	}
	if len(t) < 3 || t[0] != '\'' || t[len(t)-1] != '\'' {
		return false
	}
	inner := t[1 : len(t)-1]
	if token.Lookup(inner).IsKeyword() {
		return true
	}
	for _, r := range inner {
		if r > 127 || !strings.ContainsRune("+-*/%&|^<>=!:;,.()[]{}~", r) {
			return false
		}
	}
	return len(inner) <= 3
}
