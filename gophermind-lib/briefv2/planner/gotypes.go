package planner

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path"
	"strings"

	"gophermind/gophermind-lib/briefv2/contract"
	"gophermind/gophermind-lib/briefv2/packer"
)

// goTypes is what a signature says about its parameter and result types, read
// by code: Params and Results are the types as written, in order (one entry
// per name in a grouped parameter), and Unresolved lists the identifiers that
// are neither a Go builtin, a type parameter of the function, a standard
// library package, a package of this plan, nor a type the contract declares.
type goTypes struct {
	Params, Results, Unresolved []string
	ParamByName                 map[string]string // parameter name to its type, for the named parameters
}

// deriveGoTypes reads sig. It is the source of every input's and output's
// go_type, so a model never writes it.
func deriveGoTypes(sig string, c *contract.Contracts) (goTypes, error) {
	fd, err := parseSignature(sig)
	if err != nil {
		return goTypes{}, err
	}
	declared := contractTypeNames(c)
	pkgs := planPackageNames(c)
	if fd.Type.TypeParams != nil {
		for _, f := range fd.Type.TypeParams.List {
			for _, n := range f.Names {
				declared[n.Name] = true
			}
		}
	}
	g := goTypes{ParamByName: map[string]string{}}
	seen := map[string]bool{}
	collect := func(fl *ast.FieldList, into *[]string) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			expr := types.ExprString(f.Type)
			n := max(1, len(f.Names))
			for i := 0; i < n; i++ {
				*into = append(*into, expr)
			}
			if into == &g.Params {
				for _, name := range f.Names {
					g.ParamByName[name.Name] = expr
				}
			}
			for _, name := range unresolvedTypeNames(f.Type, declared, pkgs) {
				if !seen[name] {
					seen[name] = true
					g.Unresolved = append(g.Unresolved, name)
				}
			}
		}
	}
	collect(fd.Type.Params, &g.Params)
	collect(fd.Type.Results, &g.Results)
	return g, nil
}

// unresolvedTypeNames walks a type expression and returns the names that
// resolve to nothing.
func unresolvedTypeNames(e ast.Expr, declared, pkgs map[string]bool) []string {
	var bad []string
	ast.Inspect(e, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok && !packer.IsStdPackageName(id.Name) && !pkgs[id.Name] {
				bad = append(bad, id.Name)
			}
			return false // the selected name belongs to the package
		case *ast.Ident:
			if types.Universe.Lookup(x.Name) == nil && !declared[x.Name] {
				bad = append(bad, x.Name)
			}
		case *ast.Field:
			// A field's name is not a type: only its type is walked.
			if x.Type != nil {
				bad = append(bad, unresolvedTypeNames(x.Type, declared, pkgs)...)
			}
			return false
		}
		return true
	})
	return bad
}

// contractTypeNames is the set of Go type names the contract declares.
func contractTypeNames(c *contract.Contracts) map[string]bool {
	out := map[string]bool{}
	if c == nil {
		return out
	}
	for _, t := range c.Types {
		f, err := parser.ParseFile(token.NewFileSet(), "", "package p\n"+t.Decl+"\n", parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, d := range f.Decls {
			if gd, ok := d.(*ast.GenDecl); ok {
				for _, s := range gd.Specs {
					if ts, ok := s.(*ast.TypeSpec); ok {
						out[ts.Name.Name] = true
					}
				}
			}
		}
	}
	return out
}

// planPackageNames is the set of package names the plan itself defines: a
// qualified identifier such as store.User is resolved when store is one.
func planPackageNames(c *contract.Contracts) map[string]bool {
	out := map[string]bool{}
	if c == nil {
		return out
	}
	add := func(p string) {
		if p != "" {
			out[path.Base(strings.TrimSuffix(p, "/"))] = true
		}
	}
	for _, comp := range c.Components {
		add(comp.Package)
	}
	for _, t := range c.Types {
		add(t.Package)
	}
	for _, f := range c.Functions {
		add(f.Package)
	}
	return out
}
