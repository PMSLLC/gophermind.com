package planner

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
)

var errorMethodSigRE = regexp.MustCompile(`^\s*func\s*\(\s*\w*\s*\*?\s*(\w+)\s*\)\s*Error\s*\(\s*\)\s*string`)

// addErrorMethods gives an Error method to each contract struct type named as
// an error (cliError, ParseError) that has a string field and no Error method:
// the functions return it as an error and the tests compare it as one, but
// without the method it is not an error and no test compiled. The method
// returns the first string field. It returns the ids of the types it changed.
func addErrorMethods(doc map[string]any) []string {
	// An Error method may also be a contract function of its own.
	fnError := map[string]bool{}
	for _, fn := range objects(doc["functions"]) {
		sig, _ := fn["signature"].(string)
		if m := errorMethodSigRE.FindStringSubmatch(sig); m != nil {
			fnError[m[1]] = true
		}
	}
	var changed []string
	for _, t := range objects(doc["types"]) {
		decl, _ := t["decl"].(string)
		f, err := parser.ParseFile(token.NewFileSet(), "decl.go", "package p\n"+decl, 0)
		if err != nil {
			continue
		}
		hasError := map[string]bool{}
		for k := range fnError {
			hasError[k] = true
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv != nil && fd.Name.Name == "Error" && len(fd.Recv.List) == 1 {
				hasError[recvTypeName(fd.Recv.List[0].Type)] = true
			}
		}
		var add []string
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, s := range gd.Specs {
				ts := s.(*ast.TypeSpec)
				st, ok := ts.Type.(*ast.StructType)
				name := ts.Name.Name
				if !ok || hasError[name] || !strings.HasSuffix(strings.ToLower(name), "error") {
					continue
				}
				if field := firstStringField(st); field != "" {
					add = append(add, "func (e *"+name+") Error() string { return e."+field+" }")
				}
			}
		}
		if len(add) > 0 {
			t["decl"] = strings.TrimRight(decl, "\n") + "\n\n" + strings.Join(add, "\n\n") + "\n"
			id, _ := t["id"].(string)
			changed = append(changed, id)
		}
	}
	return changed
}

func firstStringField(st *ast.StructType) string {
	for _, f := range st.Fields.List {
		if id, ok := f.Type.(*ast.Ident); ok && id.Name == "string" && len(f.Names) > 0 {
			return f.Names[0].Name
		}
	}
	return ""
}
