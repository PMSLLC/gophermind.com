package planner

import (
	"errors"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
)

// unescapeQuotedSource repairs Go source whose string quotes arrived escaped a
// second time ({\"happy\", \"42\"}): the JSON decoder leaves a backslash before
// every quote and the file does not parse. Source that parses is returned as it
// was; the repair is kept only if it parses.
func unescapeQuotedSource(src string) string {
	if !strings.Contains(src, `\"`) || parsesAsGo(src) {
		return src
	}
	fixed := strings.ReplaceAll(src, `\"`, `"`)
	if !parsesAsGo(fixed) {
		return src
	}
	return fixed
}

func parsesAsGo(src string) bool {
	_, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution)
	return err == nil
}

// checkNotPlaceholder refuses a test file that is the example placeholder from
// the prompt instead of source, with a message that says what to send instead.
func checkNotPlaceholder(src string) error {
	s := strings.TrimSpace(src)
	if strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">") && !strings.Contains(s, "\n") {
		return errors.New("test_file holds the placeholder from the example reply, not Go source; put the complete Go test file, starting with its package clause, in test_file")
	}
	return nil
}

var (
	importBlockRE  = regexp.MustCompile(`(?s)import\s*\((.*?)\)`)
	bareImportRE   = regexp.MustCompile(`(?m)^(\s*(?:[A-Za-z_][A-Za-z0-9_]*\s+|\.\s+)?)([A-Za-z0-9][A-Za-z0-9._\-]*(?:/[A-Za-z0-9._\-]+)*)\s*$`)
	singleImportRE = regexp.MustCompile(`(?m)^import\s+((?:[A-Za-z_][A-Za-z0-9_]*\s+)?)([A-Za-z0-9][A-Za-z0-9._\-]*(?:/[A-Za-z0-9._\-]+)+)\s*$`)
)

// quoteBareImports quotes import paths a model wrote without quotes
// (example.com/slugify/internal/slug inside an import block), which never
// parse. Source that parses is returned as it was; the repair is kept only if
// it parses.
func quoteBareImports(src string) string {
	if parsesAsGo(src) {
		return src
	}
	fixed := importBlockRE.ReplaceAllStringFunc(src, func(block string) string {
		m := importBlockRE.FindStringSubmatchIndex(block)
		body := block[m[2]:m[3]]
		return block[:m[2]] + bareImportRE.ReplaceAllString(body, "${1}\"${2}\"") + block[m[3]:]
	})
	fixed = singleImportRE.ReplaceAllString(fixed, "import ${1}\"${2}\"")
	if !parsesAsGo(fixed) {
		return src
	}
	return fixed
}
