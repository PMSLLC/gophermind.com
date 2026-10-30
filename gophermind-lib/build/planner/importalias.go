package planner

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strconv"
)

// dropUnusedImportAlias removes an import alias the file never uses when the
// file uses the package's own name instead: a model imported a package as
// example and called it as roman.IsCanonical (run gm-2026-10-30-051). Source
// that does not parse, or has no such alias, is returned as it was.
func dropUnusedImportAlias(src string) string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if err != nil {
		return src
	}
	used := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if s, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := s.X.(*ast.Ident); ok {
				used[id.Name] = true
			}
		}
		return true
	})
	// Cut from the end so earlier offsets stay right.
	out := src
	for i := len(f.Imports) - 1; i >= 0; i-- {
		im := f.Imports[i]
		if im.Name == nil || im.Name.Name == "_" || im.Name.Name == "." || used[im.Name.Name] {
			continue
		}
		p, err := strconv.Unquote(im.Path.Value)
		if err != nil || !used[path.Base(p)] {
			continue
		}
		from, to := fset.Position(im.Name.Pos()).Offset, fset.Position(im.Path.Pos()).Offset
		out = out[:from] + out[to:]
	}
	return out
}
