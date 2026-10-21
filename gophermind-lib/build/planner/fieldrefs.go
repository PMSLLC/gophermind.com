package planner

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strings"

	"gophermind/gophermind-lib/build/contract"
)

// A node's prose can describe behavior that needs state its types do not have
// (s.Mean = s.Sum / s.Count where the declared struct has no Sum). The
// implementer then cannot build the function and the test writer cannot test
// it. undeclaredFieldRefs finds those references by code, so Decompose can ask
// for the prose again before the plan carries them forward.

// fieldRef is one selector in prose that names a field the declared struct lacks.
type fieldRef struct {
	Name, Type, Field string
	Declared          []string
}

// detail is the fixed-form line that names the defect in a repair prompt. It
// quotes only identifiers: the parameter, the type, the unknown field and the
// type's declared fields.
func (f fieldRef) detail() string {
	return fmt.Sprintf("undeclared_field: parameter %s of type %s has no field %s; declared fields: %s",
		f.Name, f.Type, f.Field, strings.Join(f.Declared, ", "))
}

var selectorInProse = regexp.MustCompile(`(^|[^A-Za-z0-9_.])([A-Za-z_][A-Za-z0-9_]*)\.([A-Za-z_][A-Za-z0-9_]*)`)

// proseKeysSkipped are contract keys whose strings are code or names, not prose.
var proseKeysSkipped = map[string]bool{"package": true, "file": true, "signature": true, "name": true, "type": true, "go_type": true, "kind": true}

// nodeProse is the free text of a decomposed node: its description and every
// string in its contract that is not a name, type or path.
func nodeProse(d map[string]any) []string {
	var out []string
	if s, _ := d["description"].(string); s != "" {
		out = append(out, s)
	}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			out = append(out, x)
		case []any:
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				if !proseKeysSkipped[k] {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(x[k])
			}
		}
	}
	if ct, ok := d["contract"].(map[string]any); ok {
		walk(ct)
	}
	return out
}

// undeclaredFieldRefs lists, for function f and its node d, the selectors in
// the node's prose of the form <parameter or receiver>.<Field> where the name's
// type (after stripping *, [] and ...) is a struct the contract declares and
// Field is neither one of its fields nor a method the contract declares on it.
// A struct with embedded fields is skipped: it promotes fields this check
// cannot list. At most one report per name and field.
func undeclaredFieldRefs(f contract.Function, d map[string]any, c *contract.Contracts) []fieldRef {
	fd, err := parseSignature(f.Signature)
	if err != nil {
		return nil
	}
	names := map[string]ast.Expr{}
	add := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, fld := range fl.List {
			for _, n := range fld.Names {
				if n.Name != "_" {
					names[n.Name] = fld.Type
				}
			}
		}
	}
	add(fd.Recv)
	add(fd.Type.Params)
	if len(names) == 0 {
		return nil
	}
	structs := map[string]structInfo{} // by parameter name
	for name, expr := range names {
		if si, ok := lookupStruct(baseTypeName(expr), f.Package, c); ok {
			structs[name] = si
		}
	}
	if len(structs) == 0 {
		return nil
	}
	var out []fieldRef
	seen := map[string]bool{}
	for _, text := range nodeProse(d) {
		for _, m := range selectorInProse.FindAllStringSubmatch(text, -1) {
			name, field := m[2], m[3]
			si, ok := structs[name]
			if !ok || si.embedded || contains(si.fields, field) || contains(si.methods, field) {
				continue
			}
			if len(field) == 1 && field[0] >= 'a' && field[0] <= 'z' {
				continue // e.g., i.e.
			}
			if key := name + "." + field; !seen[key] {
				seen[key] = true
				out = append(out, fieldRef{Name: name, Type: si.name, Field: field, Declared: si.fields})
			}
		}
	}
	return out
}

// baseTypeName strips pointers, slices, arrays, ellipses and parentheses from
// a type expression and returns the name of the type left (the selected name of
// a package-qualified one), or "".
func baseTypeName(e ast.Expr) string {
	for {
		switch x := e.(type) {
		case *ast.StarExpr:
			e = x.X
		case *ast.ArrayType:
			e = x.Elt
		case *ast.Ellipsis:
			e = x.Elt
		case *ast.ParenExpr:
			e = x.X
		case *ast.SelectorExpr:
			return x.Sel.Name
		case *ast.Ident:
			return x.Name
		default:
			return ""
		}
	}
}

// structInfo is a declared struct type: its field names, the methods the
// contract declares on it, and whether it embeds another type.
type structInfo struct {
	name     string
	fields   []string
	methods  []string
	embedded bool
}

// lookupStruct finds the struct type called name in the contract, preferring
// the one in pkg when several packages declare the name. It reports false for
// a name the contract does not declare as a struct, or an ambiguous one.
func lookupStruct(name, pkg string, c *contract.Contracts) (structInfo, bool) {
	if name == "" || c == nil {
		return structInfo{}, false
	}
	var found []structInfo
	var pkgs []string
	for _, t := range c.Types {
		file, err := parser.ParseFile(token.NewFileSet(), "", "package p\n"+t.Decl+"\n", parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, s := range gd.Specs {
				ts, ok := s.(*ast.TypeSpec)
				if !ok || ts.Name.Name != name {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				si := structInfo{name: name}
				for _, fld := range st.Fields.List {
					if len(fld.Names) == 0 {
						si.embedded = true
					}
					for _, n := range fld.Names {
						si.fields = append(si.fields, n.Name)
					}
				}
				found = append(found, si)
				pkgs = append(pkgs, t.Package)
			}
		}
	}
	pick := -1
	switch {
	case len(found) == 1:
		pick = 0
	case len(found) > 1:
		for i, p := range pkgs {
			if p == pkg {
				if pick >= 0 {
					return structInfo{}, false
				}
				pick = i
			}
		}
	}
	if pick < 0 {
		return structInfo{}, false
	}
	si := found[pick]
	for _, fn := range c.Functions {
		if fn.Package != pkgs[pick] {
			continue
		}
		if fdecl, err := parseSignature(fn.Signature); err == nil && fdecl.Recv != nil && len(fdecl.Recv.List) == 1 &&
			baseTypeName(fdecl.Recv.List[0].Type) == name {
			si.methods = append(si.methods, fdecl.Name.Name)
		}
	}
	return si, true
}
