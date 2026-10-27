package packer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"sort"
	"strconv"
	"strings"
)

// stdByName maps the package name of a standard library package to its import
// path when exactly one package has that name (rand, template and a few more
// have two, and are left to the model).
var stdByName = func() map[string]string {
	paths := map[string][]string{}
	for p := range stdPackages {
		n := path.Base(p)
		if versionDir(n) {
			continue
		}
		paths[n] = append(paths[n], p)
	}
	out := map[string]string{}
	for n, ps := range paths {
		if len(ps) == 1 {
			out[n] = ps[0]
		}
	}
	return out
}()

// versionDir reports whether a path element is a /vN major-version suffix.
func versionDir(e string) bool {
	if len(e) < 2 || e[0] != 'v' {
		return false
	}
	_, err := strconv.Atoi(e[1:])
	return err == nil
}

// addMissingStdImports adds an import for every standard library package that
// the source uses as a qualifier (pkg.Name) without importing it or declaring
// the name in the file. A model that writes utf8.RuneLen and forgets
// "unicode/utf8" has one unambiguous fix, made here instead of costing an
// attempt. Only unambiguous standard library names are added, each as its own
// import line after the package clause (the caller formats the file). A name
// declared in another file of the package is not visible here; adding an
// import over it would not compile, which is the failure the attempt would
// have had anyway. added lists the paths, sorted. A file that does not parse
// is returned unchanged.
func addMissingStdImports(src string) (string, []string) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return src, nil
	}
	have := map[string]bool{}
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return src, nil
		}
		name := path.Base(p)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		have[name] = true
	}
	need := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || id.Obj != nil || have[id.Name] {
			return true
		}
		if p, ok := stdByName[id.Name]; ok {
			need[p] = true
		}
		return true
	})
	if len(need) == 0 {
		return src, nil
	}
	var added []string
	for p := range need {
		added = append(added, p)
	}
	sort.Strings(added)
	var b strings.Builder
	for _, p := range added {
		b.WriteString("\nimport " + strconv.Quote(p))
	}
	at := fset.Position(f.Name.End()).Offset
	return src[:at] + b.String() + src[at:], added
}
