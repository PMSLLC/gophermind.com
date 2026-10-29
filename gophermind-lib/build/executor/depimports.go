package executor

import (
	"fmt"
	"path"
	"regexp"
	"unicode"

	"gophermind/gophermind-lib/build/contract"
	"gophermind/gophermind-lib/build/planner"
)

var (
	funcNameRE = regexp.MustCompile(`^func\s+([A-Za-z_][A-Za-z0-9_]*)`) // not a method: no receiver
	typeNameRE = regexp.MustCompile(`(?m)^\s*type\s+([A-Za-z_][A-Za-z0-9_]*)`)
)

// dependencyImports names, for each function and type in ids that is declared in
// a package other than leafPkg, its qualifier and import path:
//
//	slug.Make: import "example.com/m/internal/slug"
//
// The dependency signatures alone say only `func Make(...)`, and a model called
// it bare three times (undefined: Make). Only exported names are listed, since
// no other name can be used from another package, and methods are not (they are
// reached through a value). The order follows ids.
func dependencyImports(c *contract.Contracts, ids []string, leafPkg string) []string {
	if c == nil {
		return nil
	}
	fns := map[string]contract.Function{}
	for _, f := range c.Functions {
		fns[f.ID] = f
	}
	types := map[string]contract.Type{}
	for _, t := range c.Types {
		types[t.ID] = t
	}
	line := func(pkg, name string) string {
		imp := c.Module
		if p := path.Clean(pkg); p != "." {
			imp = path.Join(c.Module, p)
		}
		return fmt.Sprintf("%s.%s: import %q", planner.PackageClause(pkg), name, imp)
	}
	exported := func(n string) bool { return n != "" && unicode.IsUpper([]rune(n)[0]) }
	var out []string
	for _, id := range ids {
		if f, ok := fns[id]; ok && f.Package != leafPkg {
			if m := funcNameRE.FindStringSubmatch(f.Signature); m != nil && exported(m[1]) {
				out = append(out, line(f.Package, m[1]))
			}
		}
		if t, ok := types[id]; ok && t.Package != leafPkg {
			for _, m := range typeNameRE.FindAllStringSubmatch(t.Decl, -1) {
				if exported(m[1]) {
					out = append(out, line(t.Package, m[1]))
				}
			}
		}
	}
	return out
}
