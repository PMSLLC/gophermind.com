package planner

import (
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// dropUnusedImports removes the import specs of a Go source file whose package
// name is never used as a selector qualifier. A model slip of this kind has one
// unambiguous fix, so it is made here instead of costing a repair call. Blank
// and dot imports are never touched, an import is only ever removed, and a file
// that does not parse is returned unchanged. The package name is the explicit
// alias or the last path element (the one before a /vN suffix); a path whose
// last element is not a plain identifier is kept, since its declared name is
// unknown. removed lists the names dropped, in source order.
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
	type cut struct{ from, to int } // 1-based inclusive line range
	var cuts []cut
	var removed []string
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
			cuts = append(cuts, cut{fset.Position(gd.Pos()).Line, fset.Position(gd.End()).Line})
			continue
		}
		for _, is := range drop {
			cuts = append(cuts, cut{fset.Position(is.Pos()).Line, fset.Position(is.End()).Line})
		}
	}
	if len(cuts) == 0 {
		return src, nil
	}
	sort.Slice(cuts, func(i, j int) bool { return cuts[i].from < cuts[j].from })
	lines := strings.SplitAfter(src, "\n")
	var b strings.Builder
	for i, l := range lines {
		n := i + 1
		skip := false
		for _, c := range cuts {
			if n >= c.from && n <= c.to {
				skip = true
				break
			}
		}
		if !skip {
			b.WriteString(l)
		}
	}
	out, err := format.Source([]byte(b.String()))
	if err != nil {
		return src, nil
	}
	return string(out), removed
}

var plainIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var versionElem = regexp.MustCompile(`^v[0-9]+$`)

// importName is the name an import is referenced by, or "" when it cannot be
// known from the path alone.
func importName(is *ast.ImportSpec) string {
	if is.Name != nil {
		return is.Name.Name
	}
	p, err := strconv.Unquote(is.Path.Value)
	if err != nil {
		return ""
	}
	parts := strings.Split(p, "/")
	last := parts[len(parts)-1]
	if versionElem.MatchString(last) && len(parts) > 1 {
		last = parts[len(parts)-2]
	}
	if !plainIdent.MatchString(last) {
		return ""
	}
	return last
}
