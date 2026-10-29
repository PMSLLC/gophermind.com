package planner

import (
	"go/parser"
	"go/token"
	"slices"
)

// fixTestPackageClause rewrites the package clause of a test file to the plain
// package name when it is not one the contract package allows. A model derives
// the name from the import path ("internal_cli_test") or invents one, the
// harness knows the right one, and refusing the reply cost three whole calls.
// Source that does not parse, or has an allowed clause, is returned as it was.
func fixTestPackageClause(src, pkg string) string {
	names := packageClauseNames(pkg)
	if len(names) == 0 {
		return src
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.PackageClauseOnly)
	if err != nil || f.Name == nil || slices.Contains(names, f.Name.Name) {
		return src
	}
	start := fset.Position(f.Name.Pos()).Offset
	end := fset.Position(f.Name.End()).Offset
	if start < 0 || end > len(src) || start >= end {
		return src
	}
	return src[:start] + names[0] + src[end:]
}
