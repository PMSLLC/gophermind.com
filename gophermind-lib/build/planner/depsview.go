package planner

import (
	"go/ast"
	"go/parser"
	"go/token"
)

// withoutUnexportedDeps returns view without the unexported functions in
// context.dependency_signatures. An external test package cannot call an
// unexported helper and the prompt forbids testing it, so its signature is
// noise that invites a test that does not compile. A test in the same package
// (an unexported target, or a main package) may still need it, so those views
// pass through unchanged, as does anything that is not a function declaration
// or does not parse. view and the node it came from are left unchanged.
func withoutUnexportedDeps(view map[string]any, sig, pkgDir string) map[string]any {
	if _, _, why := internalTarget(sig); why != whyNone || PackageClause(pkgDir) == "main" {
		return view
	}
	nctx, _ := view["context"].(map[string]any)
	var sigs []any
	switch v := nctx["dependency_signatures"].(type) {
	case []any:
		sigs = v
	case []string:
		for _, s := range v {
			sigs = append(sigs, s)
		}
	}
	if len(sigs) == 0 {
		return view
	}
	kept := make([]any, 0, len(sigs))
	for _, s := range sigs {
		if text, ok := s.(string); ok && isUnexportedFunc(text) {
			continue
		}
		kept = append(kept, s)
	}
	out := map[string]any{}
	for k, v := range view {
		out[k] = v
	}
	cp := map[string]any{}
	for k, v := range nctx {
		cp[k] = v
	}
	cp["dependency_signatures"] = kept
	out["context"] = cp
	return out
}

// isUnexportedFunc reports whether entry is a function declaration with an
// unexported name.
func isUnexportedFunc(entry string) bool {
	f, err := parser.ParseFile(token.NewFileSet(), "", "package p\n"+entry, parser.SkipObjectResolution)
	if err != nil {
		return false
	}
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && !ast.IsExported(fd.Name.Name) {
			return true
		}
	}
	return false
}
