package planner

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"sort"

	"gophermind/gophermind-lib/build/contract"
)

// errSharedName is the refusal for a test file whose top-level name is already
// declared by another file of the same package. It names the defect kind only:
// the identifier is never printed.
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
// others declares in the same package clause. Sources that do not parse are
// skipped: they cannot be compared.
func checkSharedNames(src string, others []string) error {
	clause, mine, ok := topLevelNames(src)
	if !ok {
		return nil
	}
	for _, o := range others {
		oc, theirs, ok := topLevelNames(o)
		if !ok || oc != clause {
			continue
		}
		for n := range mine {
			if theirs[n] {
				return errSharedName
			}
		}
	}
	return nil
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
