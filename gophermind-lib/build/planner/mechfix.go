package planner

import (
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// mechanicalFix is one applied fix: Kind is "unused variable" or "missing import", Name the variable or package.
type mechanicalFix struct{ Kind, Name string }

var unusedVarRE = regexp.MustCompile(`^declared and not used: (\w+)$`)

// srcInsert is text to put at a byte offset of a source file.
type srcInsert struct {
	off  int
	text string
}

// applyMechanicalFixes rewrites src for every diagnostic that has one correct fix and returns the
// formatted result, the fixes applied in diagnostic order, and the diagnostics it did not fix.
// A src that does not parse, or a fixed src that does not format, is returned unchanged with no fixes.
// An unused variable gets "_ = name" where the name is in scope; a standard library package that is
// used but not imported gets an import. Every other diagnostic is left to the model.
func applyMechanicalFixes(src string, diags []compileDiag) (string, []mechanicalFix, []compileDiag) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return src, nil, diags
	}
	off := func(p token.Pos) int { return fset.Position(p).Offset }
	var fixes []mechanicalFix
	var unfixed []compileDiag
	var inserts []srcInsert
	seenInsert := map[srcInsert]bool{}
	var missing []string
	seenMissing := map[string]bool{}
	for _, d := range diags {
		if m := unusedVarRE.FindStringSubmatch(strings.TrimSpace(d.Msg)); m != nil {
			ins := unusedVarInserts(fset, off, f, d, m[1])
			if len(ins) == 0 {
				unfixed = append(unfixed, d)
				continue
			}
			for _, in := range ins {
				if !seenInsert[in] {
					seenInsert[in] = true
					inserts = append(inserts, in)
				}
			}
			fixes = append(fixes, mechanicalFix{"unused variable", m[1]})
			continue
		}
		if pkg := missingStdImport(d.Msg); pkg != "" && !importsName(f, pkg) {
			if !seenMissing[pkg] {
				seenMissing[pkg] = true
				missing = append(missing, pkg)
				fixes = append(fixes, mechanicalFix{"missing import", pkg})
			}
			continue
		}
		unfixed = append(unfixed, d)
	}
	if len(missing) > 0 {
		inserts = append(inserts, importInserts(f, off, missing)...)
	}
	if len(inserts) == 0 {
		return src, nil, diags
	}
	sort.SliceStable(inserts, func(i, j int) bool { return inserts[i].off < inserts[j].off })
	var b strings.Builder
	pos := 0
	for _, in := range inserts {
		b.WriteString(src[pos:in.off])
		b.WriteString(in.text)
		pos = in.off
	}
	b.WriteString(src[pos:])
	out, err := format.Source([]byte(b.String()))
	if err != nil {
		return src, nil, diags
	}
	return string(out), fixes, unfixed
}

// importsName reports whether f already has an import that is referenced as name.
func importsName(f *ast.File, name string) bool {
	for _, is := range f.Imports {
		if is.Name != nil {
			if is.Name.Name == name {
				return true
			}
			continue
		}
		if p, err := strconv.Unquote(is.Path.Value); err == nil && path.Base(p) == name {
			return true
		}
	}
	return false
}

// importInserts adds the standard library packages pkgs to the first import
// declaration of f, or as a new declaration after the package clause. The
// formatting pass sorts the block.
func importInserts(f *ast.File, off func(token.Pos) int, pkgs []string) []srcInsert {
	var list strings.Builder
	for _, p := range pkgs {
		list.WriteString("\n" + strconv.Quote(p))
	}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		if gd.Lparen.IsValid() {
			return []srcInsert{{off(gd.Lparen) + 1, list.String()}}
		}
		spec := gd.Specs[0]
		return []srcInsert{{off(spec.Pos()), "(" + list.String() + "\n"}, {off(spec.End()), "\n)"}}
	}
	return []srcInsert{{off(f.Name.End()), "\n\nimport (" + list.String() + "\n)"}}
}

// unusedVarInserts is where "_ = name" goes for the diagnostic d, or nil when
// the declaring identifier cannot be found or has no place the rules cover.
// The identifier is the one at the diagnostic's line and column; failing that,
// the first identifier of that name on the line.
func unusedVarInserts(fset *token.FileSet, off func(token.Pos) int, f *ast.File, d compileDiag, name string) []srcInsert {
	var stack, exact, fallback []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		stack = append(stack, n)
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			p := fset.Position(id.Pos())
			if p.Line == d.Line {
				if p.Column == d.Col && exact == nil {
					exact = append([]ast.Node(nil), stack...)
				}
				if fallback == nil {
					fallback = append([]ast.Node(nil), stack...)
				}
			}
		}
		return true
	})
	nodes := exact
	if nodes == nil {
		nodes = fallback
	}
	if nodes == nil {
		return nil
	}
	text := "_ = " + name
	// first puts the statement first in a body whose opening token is at open.
	first := func(list []ast.Stmt, open token.Pos) srcInsert {
		if len(list) > 0 {
			return srcInsert{off(list[0].Pos()), text + "\n"}
		}
		return srcInsert{off(open) + 1, "\n" + text}
	}
	// Walk outward from the identifier. A statement that has the identifier in
	// its header (anything but its body or else branch) is where the variable is
	// scoped, so it is checked before a listed statement is looked for. This
	// also covers an else-if and a labeled loop, whose statement is not itself
	// a member of a list.
	for i := len(nodes) - 2; i >= 0; i-- {
		child := nodes[i+1]
		switch s := nodes[i].(type) {
		case *ast.IfStmt:
			if child != ast.Node(s.Body) && child != s.Else {
				return []srcInsert{first(s.Body.List, s.Body.Lbrace)}
			}
		case *ast.ForStmt:
			if child != ast.Node(s.Body) {
				return []srcInsert{first(s.Body.List, s.Body.Lbrace)}
			}
		case *ast.RangeStmt:
			if child != ast.Node(s.Body) {
				return []srcInsert{first(s.Body.List, s.Body.Lbrace)}
			}
		case *ast.SwitchStmt:
			if child != ast.Node(s.Body) {
				return clauseInserts(s.Body, first)
			}
		case *ast.TypeSwitchStmt:
			if child != ast.Node(s.Body) {
				return clauseInserts(s.Body, first)
			}
		case *ast.CommClause:
			if !inStmtList(s, child) {
				return []srcInsert{first(s.Body, s.Colon)}
			}
		case *ast.CaseClause:
			if !inStmtList(s, child) {
				return nil
			}
		}
		if n, ok := nodes[i].(ast.Stmt); ok && i > 0 && inStmtList(nodes[i-1], n) {
			return []srcInsert{{off(n.End()), "\n" + text}}
		}
	}
	return nil
}

// clauseInserts puts the statement first in every case clause of body.
func clauseInserts(body *ast.BlockStmt, first func([]ast.Stmt, token.Pos) srcInsert) []srcInsert {
	var out []srcInsert
	for _, s := range body.List {
		if cc, ok := s.(*ast.CaseClause); ok {
			out = append(out, first(cc.Body, cc.Colon))
		}
	}
	return out
}

// inStmtList reports whether s is one of the statements of the block or clause parent.
func inStmtList(parent, s ast.Node) bool {
	var list []ast.Stmt
	switch p := parent.(type) {
	case *ast.BlockStmt:
		list = p.List
	case *ast.CaseClause:
		list = p.Body
	case *ast.CommClause:
		list = p.Body
	}
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
