package planner

import (
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// dropUnusedImports removes the import specs of a Go source file whose package
// name is never used as a selector qualifier. A model slip of this kind has one
// unambiguous fix, so it is made here instead of costing a repair call. Blank
// and dot imports are never touched, an import is only ever removed, and a file
// that does not parse is returned unchanged. The package name is the explicit
// alias or, for standard library paths only, the last path element; every other
// import without an alias is kept. removed lists the names dropped, in source order.
func dropUnusedImports(src string) (string, []string) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return src, nil
	}
	used := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok {
				used[id.Name] = true
			}
		}
		return true
	})
	type cut struct{ from, to int } // byte offsets, to exclusive
	var cuts []cut
	var removed []string
	off := func(p token.Pos) int { return fset.Position(p).Offset }
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		var drop []*ast.ImportSpec
		for _, s := range gd.Specs {
			is := s.(*ast.ImportSpec)
			name := importName(is)
			if name == "" || name == "_" || name == "." || used[name] {
				continue
			}
			drop = append(drop, is)
			removed = append(removed, name)
		}
		if len(drop) == 0 {
			continue
		}
		if len(drop) == len(gd.Specs) || !gd.Lparen.IsValid() {
			cuts = append(cuts, cut{off(gd.Pos()), off(gd.End())})
			continue
		}
		for _, is := range drop {
			end := is.End()
			if is.Comment != nil {
				end = is.Comment.End()
			}
			cuts = append(cuts, cut{off(is.Pos()), off(end)})
		}
	}
	if len(cuts) == 0 {
		return src, nil
	}
	sort.Slice(cuts, func(i, j int) bool { return cuts[i].from < cuts[j].from })
	var b strings.Builder
	pos := 0
	for _, c := range cuts {
		from, to := c.from, c.to
		// Take the line's indentation and newline too when nothing else is on it.
		ls := strings.LastIndexByte(src[:from], '\n') + 1
		le := to
		for le < len(src) && (src[le] == ' ' || src[le] == '\t' || src[le] == '\r') {
			le++
		}
		if strings.TrimSpace(src[ls:from]) == "" && (le == len(src) || src[le] == '\n') {
			from = ls
			if le < len(src) {
				to = le + 1
			}
		}
		if to == c.to && le < len(src) && src[le] == ';' {
			// Same-line form: take the separator with the spec.
			to = le + 1
			for to < len(src) && (src[to] == ' ' || src[to] == '\t') {
				to++
			}
		}
		if from < pos {
			continue
		}
		b.WriteString(src[pos:from])
		pos = to
	}
	b.WriteString(src[pos:])
	out, err := format.Source([]byte(b.String()))
	if err != nil {
		return src, nil
	}
	return string(out), removed
}

var plainIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var versionElem = regexp.MustCompile(`^v([2-9]|[1-9][0-9]+)$`)

// importName is the name an import is referenced by, or "" when it cannot be
// known from the path alone. Only standard library paths (a directory under
// GOROOT/src; a module path without a dot is not enough) are guessed; any other path without an alias is kept, since
// its declared package name may differ from its directory. A /vN suffix is a
// major-version element only for N >= 2 (math/rand/v2 declares rand).
func importName(is *ast.ImportSpec) string {
	if is.Name != nil {
		return is.Name.Name
	}
	p, err := strconv.Unquote(is.Path.Value)
	if err != nil {
		return ""
	}
	parts := strings.Split(p, "/")
	if !isStdlib(p) {
		return ""
	}
	last := parts[len(parts)-1]
	if versionElem.MatchString(last) && len(parts) > 1 {
		last = parts[len(parts)-2]
	}
	if !plainIdent.MatchString(last) {
		return ""
	}
	return last
}

// isStdlib reports whether p is a package directory of the standard library.
func isStdlib(p string) bool {
	root := runtime.GOROOT()
	if root == "" || strings.Contains(strings.Split(p, "/")[0], ".") {
		return false
	}
	st, err := os.Stat(filepath.Join(root, "src", filepath.FromSlash(p)))
	return err == nil && st.IsDir()
}
