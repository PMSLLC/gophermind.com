package planner

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
)

// urlHost finds the host part of an http or https URL inside a string. The
// host stops at closing punctuation so a path-less URL in markdown or HTML is
// read cleanly; a bracketed IPv6 host is kept whole.
var urlHost = regexp.MustCompile("(?i)https?://(\\[[^\\]]*\\](?::\\d+)?|[^/\\s\"'`?#)>,;\\]}]+)")

// checkNoRealHost refuses a test file with a string literal that names a real
// host. A local model wrote http://example.com/redir twice, so the test went
// out to the network and passed or failed on whatever answered. Loopback
// hosts and format verbs (a URL built from srv.URL) are fine. The host is the
// model's own text, so it may appear in the refusal, capped; the brief is
// never quoted. A file that does not parse returns nil: that is reported
// earlier.
func checkNoRealHost(src string) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	var refusal error
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if refusal != nil || !ok || lit.Kind != token.STRING {
			return refusal == nil
		}
		text, uerr := strconv.Unquote(lit.Value)
		if uerr != nil {
			text = lit.Value
		}
		for _, m := range urlHost.FindAllStringSubmatch(text, -1) {
			if !localHost(m[1]) {
				refusal = fmt.Errorf("test file reaches the network at line %d: a test never names a real host (%s); serve fixtures with httptest.NewServer and build every URL from srv.URL, or use http://127.0.0.1", fset.Position(lit.Pos()).Line, capLine(m[1], 60))
				break
			}
		}
		return refusal == nil
	})
	return refusal
}

// localHost reports whether host is loopback or carries a format verb.
func localHost(host string) bool {
	h := strings.ToLower(host)
	if strings.Contains(h, "%") {
		return true
	}
	switch h {
	case "localhost", "127.0.0.1", "::1", "[::1]", "0.0.0.0":
		return true
	}
	if strings.HasPrefix(h, "[::1]:") {
		return true
	}
	if i := strings.LastIndex(h, ":"); i > 0 && !strings.HasPrefix(h, "[") {
		switch h[:i] {
		case "localhost", "127.0.0.1", "0.0.0.0":
			return true
		}
	}
	return false
}
