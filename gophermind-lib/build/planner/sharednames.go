package planner

import (
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"gophermind/gophermind-lib/build/contract"
)

// errSharedName is the refusal for a test file whose top-level name is already
// declared by another file of the same package. It names the defect kind only:
// the identifier is never printed.
// errDeclaredName is the refusal for a test file that defines a name the
// contract already declares in its package (the function under test, another
// contract function or a contract type). It never names the identifier.
var errDeclaredName = errors.New("test file defines code the package already declares (the function under test or a contract type); do not define or stub it, call it as the package provides it and write only tests and test doubles")

var errSharedName = errors.New("test file declares a top-level name that another file of this package already declares; prefix every helper type, function and variable with this function's name (for example <FuncName>Reader) or declare it inside the test function")

// topLevelNames is the package clause of src and the names it declares at top
// level: types, variables, constants and plain functions (methods, init and
// the blank identifier excluded). A file that does not parse yields ok=false.
func topLevelNames(src string) (clause string, names map[string]bool, ok bool) {
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution)
	if err != nil || f.Name == nil {
		return "", nil, false
	}
	names = map[string]bool{}
	add := func(id *ast.Ident) {
		if id != nil && id.Name != "_" && id.Name != "init" {
			names[id.Name] = true
		}
	}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				add(d.Name)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					add(s.Name)
				case *ast.ValueSpec:
					for _, id := range s.Names {
						add(id)
					}
				}
			}
		}
	}
	return f.Name.Name, names, true
}

// checkSharedNames refuses src when it declares a top-level name that any of
// tests or stubs declares in the same package clause. A collision with a
// contract declaration (stubs) wins over one with another test file. Sources
// that do not parse are skipped: they cannot be compared.
func checkSharedNames(src string, tests, stubs []string) error {
	clause, mine, ok := topLevelNames(src)
	if !ok {
		return nil
	}
	collides := func(others []string) bool {
		for _, o := range others {
			oc, theirs, ok := topLevelNames(o)
			if !ok || oc != clause {
				continue
			}
			for n := range mine {
				if theirs[n] {
					return true
				}
			}
		}
		return false
	}
	if collides(stubs) {
		return errDeclaredName
	}
	if collides(tests) {
		return errSharedName
	}
	return nil
}

// isEntryPointName reports a name the go tool treats as a test entry point.
func isEntryPointName(n string) bool {
	for _, p := range []string{"Test", "Benchmark", "Example", "Fuzz"} {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

// resolveSharedNames is checkSharedNames with a repair for the one collision a
// model keeps repeating: a helper that another accepted test file already
// declares. Such a helper is renamed to <FuncName><Helper> everywhere in src
// (the first letter keeps the case of the original, so exported status does not
// change) and the rewritten source is returned with renamed=true. A collision
// with a contract declaration, a collided test entry point, a name that would
// still collide, or a file that cannot be parsed or printed gives the same
// refusal checkSharedNames gives. funcName is the node's function name (for
// example renderText or RenderText). Identifiers are matched through the
// parser's object resolution, so a local variable of the same name is left
// alone.
func resolveSharedNames(src, funcName string, tests, stubs []string) (out string, renamed bool, err error) {
	err = checkSharedNames(src, tests, stubs)
	if err != errSharedName {
		return src, false, err
	}
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, "", src, parser.ParseComments)
	if perr != nil || f.Name == nil || f.Scope == nil || funcName == "" {
		return src, false, errSharedName
	}
	clause, mine, _ := topLevelNames(src)
	taken := map[string]bool{}
	theirs := map[string]bool{}
	for _, o := range append(append([]string{}, tests...), stubs...) {
		oc, names, ok := topLevelNames(o)
		if !ok || oc != clause {
			continue
		}
		for n := range names {
			taken[n] = true
			theirs[n] = true
		}
	}
	for n := range mine {
		taken[n] = true
	}
	rename := map[string]string{}
	for n := range mine {
		if !theirs[n] {
			continue
		}
		if isEntryPointName(n) {
			return src, false, errSharedName
		}
		stem := funcName[:1]
		if n[0] >= 'a' && n[0] <= 'z' {
			stem = strings.ToLower(stem)
		} else {
			stem = strings.ToUpper(stem)
		}
		nn := stem + funcName[1:] + strings.ToUpper(n[:1]) + n[1:]
		if taken[nn] || !token.IsIdentifier(nn) {
			return src, false, errSharedName
		}
		taken[nn] = true
		rename[n] = nn
	}
	if len(rename) == 0 {
		return src, false, errSharedName
	}
	objs := map[*ast.Object]string{}
	for n, nn := range rename {
		if o := f.Scope.Lookup(n); o != nil {
			objs[o] = nn
		}
	}
	if len(objs) != len(rename) {
		return src, false, errSharedName
	}
	ast.Inspect(f, func(node ast.Node) bool {
		if id, ok := node.(*ast.Ident); ok && id.Obj != nil {
			if nn, hit := objs[id.Obj]; hit {
				id.Name = nn
			}
		}
		return true
	})
	var buf strings.Builder
	if format.Node(&buf, fset, f) != nil {
		return src, false, errSharedName
	}
	res := buf.String()
	// The result must still parse and no longer collide.
	if checkSharedNames(res, tests, stubs) != nil {
		return src, false, errSharedName
	}
	return res, true, nil
}

// stubSources is the contract's types and function signatures that live in
// dir, as Go source in the package clause of the package under test, so the
// test files of that package can be compared with them.
func stubSources(c *contract.Contracts, dir string) []string {
	var out []string
	for _, t := range c.Types {
		if path.Dir(t.File) == dir {
			out = append(out, "package "+PackageClause(t.Package)+"\n"+t.Decl+"\n")
		}
	}
	for _, f := range c.Functions {
		if path.Dir(f.File) == dir {
			out = append(out, "package "+PackageClause(f.Package)+"\n"+f.Signature+"\n")
		}
	}
	return out
}

// otherTestSources is the source of every test file already accepted for a
// node other than self in dir (the node being rewritten is never compared with
// its own earlier file). A file that cannot be read is skipped.
func otherTestSources(repo, dir, self string, st *testwriterState) []string {
	ids := make([]string, 0, len(st.Nodes))
	for id := range st.Nodes {
		if id != self {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	var out []string
	for _, id := range ids {
		rel := st.Nodes[id].TestFile
		if path.Dir(rel) != dir {
			continue
		}
		if raw, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(rel))); err == nil {
			out = append(out, string(raw))
		}
	}
	return out
}

// richDeclaredName is errDeclaredName made specific: the first top-level
// declaration of src (in source order) that the contract declares in dir, with
// its line, whether it is a contract type or function and, for a struct, the
// declared fields. The identifier is printed only when the contract declares
// it; anything else gives err back unchanged.
func richDeclaredName(src string, c *contract.Contracts, dir string, err error) error {
	if c == nil {
		return err
	}
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if perr != nil {
		return err
	}
	safe := newSafeNames(c, dir)
	const tail = "; do not define or stub it, "
	const end = " and write only tests and test doubles"
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv != nil || !safe.sameDir[d.Name.Name] {
				continue
			}
			if _, isType := contractStruct(c, dir, d.Name.Name); isType || declaredType(c, d.Name.Name) {
				continue
			}
			return fmt.Errorf("test file declares %s at line %d, a function the package already declares%scall it as the package provides it%s",
				d.Name.Name, fset.Position(d.Pos()).Line, tail, end)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || !safe.sameDir[ts.Name.Name] || !declaredType(c, ts.Name.Name) {
					continue
				}
				line := fset.Position(ts.Pos()).Line
				if si, ok := contractStruct(c, dir, ts.Name.Name); ok && len(si.fields) > 0 {
					return fmt.Errorf("test file declares %s at line %d, which the package already declares (fields: %s)%sbuild it with a composite literal of those fields%s",
						ts.Name.Name, line, strings.Join(si.fields, ", "), tail, end)
				}
				return fmt.Errorf("test file declares %s at line %d, which the package already declares%suse it as the package provides it%s",
					ts.Name.Name, line, tail, end)
			}
		}
	}
	return err
}

// declaredType reports whether the contract declares a type called name.
func declaredType(c *contract.Contracts, name string) bool {
	for _, t := range c.Types {
		if contains(declNames(t.Decl), name) {
			return true
		}
	}
	return false
}
