package packer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// stripContract removes from a reply what the rest of the package already
// declares, so a model that returns the whole file is not refused for it: the
// type, var and const specs and the plain functions whose names are in
// e.Declared (never the target), the methods of the types removed (never the
// target), and then the imports only that code used. It returns the removed
// names (contract names, never reply text) and the new file and source, or
// nothing when there is nothing to remove or the stripped file does not parse.
func stripContract(src string, e Expect) (names []string, nf *ast.File, nsrc string) {
	if len(e.Declared) == 0 {
		return nil, nil, ""
	}
	taken := make(map[string]bool, len(e.Declared))
	for _, n := range e.Declared {
		taken[n] = true
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return nil, nil, ""
	}
	// Re-find the target in this parse: same name and receiver shape.
	isTarget := func(fd *ast.FuncDecl) bool {
		return fd.Name.Name == e.FuncName && (fd.Recv != nil) == e.HasRecv && recvBase(fd) == e.recvBase
	}
	removedTypes := map[string]bool{}
	for _, d := range f.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
			for _, s := range gd.Specs {
				if ts := s.(*ast.TypeSpec); taken[ts.Name.Name] {
					removedTypes[ts.Name.Name] = true
				}
			}
		}
	}
	type span struct{ from, to int }
	var spans []span
	off := func(p token.Pos) int { return fset.Position(p).Offset }
	start := func(doc *ast.CommentGroup, p token.Pos) int {
		if doc != nil && doc.Pos() < p {
			p = doc.Pos()
		}
		return off(p)
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if isTarget(d) {
				continue
			}
			if d.Recv == nil && taken[d.Name.Name] {
				names = append(names, d.Name.Name)
				spans = append(spans, span{start(d.Doc, d.Pos()), off(d.End())})
			} else if d.Recv != nil && removedTypes[recvBase(d)] {
				names = append(names, recvBase(d)+"."+d.Name.Name)
				spans = append(spans, span{start(d.Doc, d.Pos()), off(d.End())})
			}
		case *ast.GenDecl:
			if d.Tok != token.TYPE && d.Tok != token.VAR && d.Tok != token.CONST {
				continue
			}
			var gone []ast.Spec
			for _, s := range d.Specs {
				hit := false
				switch s := s.(type) {
				case *ast.TypeSpec:
					if hit = taken[s.Name.Name]; hit {
						names = append(names, s.Name.Name)
					}
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if taken[n.Name] {
							hit = true
							names = append(names, n.Name)
						}
					}
				}
				if hit {
					gone = append(gone, s)
				}
			}
			switch {
			case len(gone) == 0:
			case len(gone) == len(d.Specs):
				spans = append(spans, span{start(d.Doc, d.Pos()), off(d.End())})
			default:
				for _, s := range gone {
					switch s := s.(type) {
					case *ast.TypeSpec:
						spans = append(spans, span{start(s.Doc, s.Pos()), off(s.End())})
					case *ast.ValueSpec:
						spans = append(spans, span{start(s.Doc, s.Pos()), off(s.End())})
					}
				}
			}
		}
	}
	if len(spans) == 0 {
		return nil, nil, ""
	}
	used := selectorBases(f)
	apply := func(text string, ss []span) string {
		sort.Slice(ss, func(i, j int) bool { return ss[i].from > ss[j].from })
		for _, s := range ss {
			text = text[:s.from] + text[s.to:]
		}
		return text
	}
	out := apply(src, spans)
	// Imports only the removed code used.
	fset2 := token.NewFileSet()
	nf1, err := parser.ParseFile(fset2, "", out, parser.ParseComments)
	if err != nil {
		return nil, nil, ""
	}
	left := selectorBases(nf1)
	var ispans []span
	for _, d := range nf1.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		var gone []*ast.ImportSpec
		for _, s := range gd.Specs {
			is := s.(*ast.ImportSpec)
			name := importName(is)
			if name == "" || name == "_" || name == "." {
				continue
			}
			if used[name] && !left[name] {
				gone = append(gone, is)
			}
		}
		o := func(p token.Pos) int { return fset2.Position(p).Offset }
		switch {
		case len(gone) == 0:
		case len(gone) == len(gd.Specs):
			ispans = append(ispans, span{o(gd.Pos()), o(gd.End())})
		default:
			for _, is := range gone {
				ispans = append(ispans, span{o(is.Pos()), o(is.End())})
			}
		}
	}
	out = apply(out, ispans)
	nf, err = parser.ParseFile(token.NewFileSet(), "", out, parser.ParseComments)
	if err != nil {
		return nil, nil, ""
	}
	sort.Strings(names)
	return names, nf, out
}

// selectorBases is every identifier that is the left side of a selector.
func selectorBases(f *ast.File) map[string]bool {
	m := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if se, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := se.X.(*ast.Ident); ok {
				m[id.Name] = true
			}
		}
		return true
	})
	return m
}

// importName is the name a file refers to an import by: the alias, or the
// last path element without a major-version suffix or a ".vN" tail.
func importName(is *ast.ImportSpec) string {
	if is.Name != nil {
		return is.Name.Name
	}
	p, err := strconv.Unquote(is.Path.Value)
	if err != nil {
		return ""
	}
	parts := strings.Split(p, "/")
	n := parts[len(parts)-1]
	if len(parts) > 1 && len(n) > 1 && n[0] == 'v' && strings.Trim(n[1:], "0123456789") == "" {
		n = parts[len(parts)-2]
	}
	if i := strings.Index(n, ".v"); i > 0 {
		n = n[:i]
	}
	return strings.ReplaceAll(strings.ReplaceAll(n, "-", "_"), ".", "_")
}
