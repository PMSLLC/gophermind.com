package planner

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
)

// typeIdentOffsets lists the offsets, in sig, of the bare identifiers that name
// a type in the parameters and results of the function signature in file. Names
// of type parameters (of the function or its receiver) are not included.
func typeIdentOffsets(file *ast.File, head int) map[string][]int {
	out := map[string][]int{}
	var fn *ast.FuncDecl
	for _, d := range file.Decls {
		if f, ok := d.(*ast.FuncDecl); ok {
			fn = f
			break
		}
	}
	if fn == nil {
		return out
	}
	skip := map[string]bool{}
	if fn.Type.TypeParams != nil {
		for _, f := range fn.Type.TypeParams.List {
			for _, n := range f.Names {
				skip[n.Name] = true
			}
		}
	}
	if fn.Recv != nil {
		// A receiver type parameter is a name, not a type of another package.
		for _, f := range fn.Recv.List {
			ast.Inspect(f.Type, func(n ast.Node) bool {
				if ix, ok := n.(*ast.IndexExpr); ok {
					ast.Inspect(ix.Index, func(m ast.Node) bool {
						if id, ok := m.(*ast.Ident); ok {
							skip[id.Name] = true
						}
						return true
					})
				}
				if ix, ok := n.(*ast.IndexListExpr); ok {
					for _, e := range ix.Indices {
						ast.Inspect(e, func(m ast.Node) bool {
							if id, ok := m.(*ast.Ident); ok {
								skip[id.Name] = true
							}
							return true
						})
					}
				}
				return true
			})
		}
	}
	var visit func(e ast.Expr)
	visit = func(e ast.Expr) {
		switch t := e.(type) {
		case *ast.Ident:
			if !skip[t.Name] {
				out[t.Name] = append(out[t.Name], int(t.Pos())-1-head)
			}
		case *ast.StarExpr:
			visit(t.X)
		case *ast.ParenExpr:
			visit(t.X)
		case *ast.Ellipsis:
			visit(t.Elt)
		case *ast.ArrayType:
			visit(t.Elt)
		case *ast.MapType:
			visit(t.Key)
			visit(t.Value)
		case *ast.ChanType:
			visit(t.Value)
		case *ast.IndexExpr:
			visit(t.X)
			visit(t.Index)
		case *ast.IndexListExpr:
			visit(t.X)
			for _, i := range t.Indices {
				visit(i)
			}
		case *ast.FuncType:
			for _, l := range []*ast.FieldList{t.Params, t.Results} {
				if l != nil {
					for _, f := range l.List {
						visit(f.Type)
					}
				}
			}
		case *ast.StructType:
			for _, f := range t.Fields.List {
				visit(f.Type)
			}
		}
		// SelectorExpr is already qualified; interfaces are left as written.
	}
	for _, l := range []*ast.FieldList{fn.Type.Params, fn.Type.Results} {
		if l != nil {
			for _, f := range l.List {
				visit(f.Type)
			}
		}
	}
	return out
}

// qualifyForeignTypes rewrites, in every function signature of doc, a bare type
// identifier to Pkg.Name when the identifier is not declared in the function's
// own package, is not a Go predeclared name, and names an exported contract type
// declared in exactly one other package (Pkg is that package's clause name, the
// same name the stub generator resolves to an import). The stub generator then
// imports that package. A name declared in several other packages, or one whose
// package clause is shared by two directories, is left as written and its
// function id is returned in ambiguous. It returns the number of identifiers
// rewritten and at most maxDefaultedIDsShown function ids.
func qualifyForeignTypes(doc map[string]any) (int, []string, []string) {
	module, _ := doc["module"].(string)
	declared := map[string]map[string]bool{} // type name -> stripped packages declaring it
	own := map[string]map[string]bool{}      // stripped package -> type names
	for _, t := range objects(doc["types"]) {
		pkg, _ := t["package"].(string)
		decl, _ := t["decl"].(string)
		key := stripModule(module, pkg)
		for _, n := range declNames(decl) {
			if declared[n] == nil {
				declared[n] = map[string]bool{}
			}
			declared[n][key] = true
			if own[key] == nil {
				own[key] = map[string]bool{}
			}
			own[key][n] = true
		}
	}
	siblings := docImports(doc)
	importOf := func(key string) string {
		if key == "." {
			return module
		}
		return module + "/" + key
	}
	total := 0
	var ex, ambiguous []string
	for _, f := range objects(doc["functions"]) {
		sig, _ := f["signature"].(string)
		pkg, _ := f["package"].(string)
		id, _ := f["id"].(string)
		key := stripModule(module, pkg)
		const head = "package p\n"
		file, err := parser.ParseFile(token.NewFileSet(), "", head+sig, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		type cut struct {
			at   int
			qual string
		}
		var cuts []cut
		isAmbiguous := false
		for name, offs := range typeIdentOffsets(file, len(head)) {
			if own[key][name] || types.Universe.Lookup(name) != nil || !ast.IsExported(name) {
				continue
			}
			others := []string{}
			for k := range declared[name] {
				if k != key {
					others = append(others, k)
				}
			}
			if len(others) == 0 {
				continue
			}
			sort.Strings(others)
			clause := PackageClause(others[0])
			if len(others) > 1 || siblings[clause] != importOf(others[0]) || clause == PackageClause(key) {
				isAmbiguous = true
				continue
			}
			for _, at := range offs {
				cuts = append(cuts, cut{at, clause})
			}
		}
		if isAmbiguous {
			ambiguous = append(ambiguous, boundedID(id))
		}
		if len(cuts) == 0 {
			continue
		}
		sort.Slice(cuts, func(i, j int) bool { return cuts[i].at > cuts[j].at })
		out := sig
		for _, c := range cuts {
			out = out[:c.at] + c.qual + "." + out[c.at:]
		}
		f["signature"] = out
		total += len(cuts)
		if len(ex) < maxDefaultedIDsShown {
			ex = append(ex, boundedID(id))
		}
	}
	return total, ex, ambiguous
}
