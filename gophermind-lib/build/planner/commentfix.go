package planner

import (
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

// dropLongComments removes the comment groups that checkCommentVolume would
// refuse as a long block (more than maxCommentRun lines). Comments never change
// what a Go test does, so the removal is safe, with one exception: directives
// (//go:..., //line, //export, // +build) and the preamble of import "C" are
// kept. In a long group that holds a directive only the other comments go. A
// file that does not parse is returned unchanged. n counts the groups touched.
func dropLongComments(src string) (string, int) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return src, 0
	}
	preamble := map[*ast.CommentGroup]bool{}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		for _, s := range gd.Specs {
			is := s.(*ast.ImportSpec)
			if p, err := strconv.Unquote(is.Path.Value); err == nil && p == "C" {
				preamble[gd.Doc] = true
				preamble[is.Doc] = true
			}
		}
	}
	off := func(p token.Pos) int { return fset.Position(p).Offset }
	var cuts []srcCut
	n := 0
	for _, g := range f.Comments {
		lines := fset.Position(g.End()).Line - fset.Position(g.Pos()).Line + 1
		if lines <= maxCommentRun || preamble[g] {
			continue
		}
		touched := false
		for _, c := range g.List {
			if isDirective(c.Text) {
				continue
			}
			cuts = append(cuts, srcCut{from: off(c.Pos()), to: off(c.End())})
			touched = true
		}
		if touched {
			n++
		}
	}
	if n == 0 {
		return src, 0
	}
	out, err := format.Source([]byte(applyCuts(src, cuts)))
	if err != nil {
		return src, 0
	}
	return string(out), n
}

// isDirective reports whether a comment is read by the Go toolchain.
func isDirective(text string) bool {
	for _, p := range []string{"//go:", "//line ", "/*line ", "//export ", "// +build", "//+build"} {
		if strings.HasPrefix(text, p) {
			return true
		}
	}
	return false
}
