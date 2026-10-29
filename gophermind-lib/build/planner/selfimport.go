package planner

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"sort"
	"strconv"
	"strings"
)

// dropSelfImport removes from a test file of package P an import of P itself,
// and the "P." qualifier on every name that import supplied. A package cannot
// import itself, so Go refused the file as an import cycle and the build
// stopped before any function was written. A file in the external test package
// (P_test), without such an import, or that does not parse is returned as it
// was, and so is any result that no longer parses.
func dropSelfImport(src, importPath string) string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil || strings.HasSuffix(f.Name.Name, "_test") {
		return src
	}
	var spec *ast.ImportSpec
	for _, s := range f.Imports {
		if p, err := strconv.Unquote(s.Path.Value); err == nil && p == importPath {
			spec = s
		}
	}
	if spec == nil || (spec.Name != nil && (spec.Name.Name == "_" || spec.Name.Name == ".")) {
		return src
	}
	alias := path.Base(importPath)
	if spec.Name != nil {
		alias = spec.Name.Name
	}
	type edit struct{ from, to int }
	var edits []edit
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == alias {
				edits = append(edits, edit{fset.Position(id.Pos()).Offset, fset.Position(sel.Sel.Pos()).Offset})
			}
		}
		return true
	})
	// The import's own line goes too (the whole declaration when it stands alone).
	from, to := fset.Position(spec.Pos()).Offset, fset.Position(spec.End()).Offset
	for _, d := range f.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.IMPORT && !g.Lparen.IsValid() && len(g.Specs) == 1 && g.Specs[0] == spec {
			from, to = fset.Position(g.Pos()).Offset, fset.Position(g.End()).Offset
		}
	}
	for from > 0 && (src[from-1] == '\t' || src[from-1] == ' ') {
		from--
	}
	if to < len(src) && src[to] == '\n' {
		to++
	}
	edits = append(edits, edit{from, to})
	sort.Slice(edits, func(i, j int) bool { return edits[i].from > edits[j].from })
	out := src
	for _, e := range edits {
		out = out[:e.from] + out[e.to:]
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "", out, parser.ParseComments|parser.SkipObjectResolution); err != nil {
		return src
	}
	return out
}
