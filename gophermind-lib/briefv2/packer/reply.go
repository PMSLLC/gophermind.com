// Package packer owns both halves of the executor's model contract: the
// prompt (Task 5b) and the reply checks here. Reply text never appears in an
// error: errors name a kind, a line and column, and sizes.
package packer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Expect is what a reply is checked against. Build it once per node with NewExpect.
type Expect struct {
	Package  string
	FuncName string
	HasRecv  bool
	canon    string
}

// canonical renders a function head without body, doc, comments or layout.
func canonical(fd *ast.FuncDecl) string {
	var b strings.Builder
	if fd.Recv != nil {
		for _, f := range fd.Recv.List {
			for _, n := range f.Names {
				b.WriteString(n.Name + " ")
			}
			b.WriteString(types.ExprString(f.Type) + ";")
		}
	}
	b.WriteString("|" + fd.Name.Name + "|" + types.ExprString(fd.Type))
	return b.String()
}

// NewExpect parses the contract's signature. An error means the contract is
// defective, not the reply.
func NewExpect(pkg, signature string) (Expect, error) {
	f, err := parser.ParseFile(token.NewFileSet(), "", "package p\n"+signature+"\n", parser.SkipObjectResolution)
	if err != nil {
		return Expect{}, errors.New("the contract signature does not parse as Go")
	}
	if len(f.Decls) != 1 {
		return Expect{}, errors.New("the contract signature is not a single function declaration")
	}
	fd, ok := f.Decls[0].(*ast.FuncDecl)
	if !ok {
		return Expect{}, errors.New("the contract signature is not a function declaration")
	}
	return Expect{Package: pkg, FuncName: fd.Name.Name, HasRecv: fd.Recv != nil, canon: canonical(fd)}, nil
}

// Reply is the checked outcome of a model reply.
type Reply struct {
	Source          []byte   // formatted file, ready to write; nil when ContractProblem or Forbidden is set
	ContractProblem string   // the one sentence, when the model answered CONTRACT_PROBLEM
	Forbidden       string   // "" or "file_header", "diff_header", "multiple_fences"
	Imports         []string // unquoted import paths of Source
	SHA256          string   // hex SHA-256 of the raw reply text
	Bytes           int
}

var (
	fileHeaderRE = regexp.MustCompile(`^\s*//\s*FILE:`)
	diffHeaderRE = regexp.MustCompile(`^(diff --git |--- a/|\+\+\+ b/|@@ )`)
)

const problemPrefix = "CONTRACT_PROBLEM:"

func malformed(kind string) error { return errors.New("malformed reply: " + kind) }

// ParseReply returns a non-nil error only for a malformed reply. Forbidden and
// ContractProblem replies return a Reply and a nil error.
func ParseReply(text string, e Expect) (Reply, error) {
	sum := sha256.Sum256([]byte(text))
	r := Reply{SHA256: hex.EncodeToString(sum[:]), Bytes: len(text)}
	body := strings.TrimSpace(text)
	if body == "" {
		return r, malformed("empty reply")
	}
	lines := strings.Split(body, "\n")
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), problemPrefix) {
			if len(lines) > 1 {
				return r, malformed("contract problem mixed with other text")
			}
			rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), problemPrefix))
			if len(rest) > 300 {
				return r, malformed(fmt.Sprintf("contract problem too long (%d bytes, limit 300)", len(rest)))
			}
			if rest != "" {
				r.ContractProblem = rest
				return r, nil
			}
		}
	}
	fences := 0
	for _, l := range lines {
		switch {
		case fileHeaderRE.MatchString(l):
			r.Forbidden = "file_header"
			return r, nil
		case diffHeaderRE.MatchString(l):
			r.Forbidden = "diff_header"
			return r, nil
		case strings.HasPrefix(strings.TrimSpace(l), "```"):
			fences++
		}
	}
	if fences > 2 {
		r.Forbidden = "multiple_fences"
		return r, nil
	}
	src := body
	switch fences {
	case 1:
		return r, malformed("unbalanced fence")
	case 2:
		first := strings.HasPrefix(strings.TrimSpace(lines[0]), "```")
		last := strings.TrimSpace(lines[len(lines)-1]) == "```"
		if !first || !last || len(lines) < 3 {
			return r, malformed("text outside the code fence")
		}
		src = strings.Join(lines[1:len(lines)-1], "\n")
	}
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.ParseComments)
	if err != nil {
		var list scanner.ErrorList
		if errors.As(err, &list) && len(list) > 0 {
			return r, malformed(fmt.Sprintf("syntax error at line %d col %d (%d bytes)", list[0].Pos.Line, list[0].Pos.Column, len(src)))
		}
		return r, malformed("syntax error")
	}
	switch {
	case strings.HasSuffix(f.Name.Name, "_test"):
		return r, malformed("test package")
	case f.Name.Name != e.Package:
		return r, malformed("package mismatch")
	}
	var imports []string
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return r, malformed("unreadable import path")
		}
		if p == "C" {
			return r, malformed("cgo import")
		}
		imports = append(imports, p)
	}
	var found []*ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == e.FuncName && (fd.Recv != nil) == e.HasRecv {
			found = append(found, fd)
		}
	}
	switch {
	case len(found) == 0:
		return r, malformed("function " + e.FuncName + " not found")
	case len(found) > 1:
		return r, malformed(fmt.Sprintf("function %s declared %d times", e.FuncName, len(found)))
	}
	if canonical(found[0]) != e.canon {
		return r, malformed("signature mismatch for " + e.FuncName)
	}
	out, err := format.Source([]byte(src))
	if err != nil {
		return r, malformed("file cannot be formatted")
	}
	sort.Strings(imports)
	for i, p := range imports {
		if i == 0 || p != imports[i-1] {
			r.Imports = append(r.Imports, p)
		}
	}
	r.Source = out
	return r, nil
}
