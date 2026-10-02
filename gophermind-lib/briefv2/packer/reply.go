// Package packer owns both halves of the executor's model contract: the
// prompt (Task 5b) and the reply checks here. Reply text never appears in an
// error: errors name a kind, a line and column, and sizes.
//
// Residual risk: the reply gate is an AST check and cannot prove that model
// code never ends the process early. Known paths (os.Exit, log.Fatal*, flag
// ExitOnError, the testing package, init functions) are refused, but other std
// paths may still exit or print. The runner's pass rule and the acceptance run
// against the built binary are the backstop, not this gate.
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
	"unicode"
)

// Expect is what a reply is checked against. Build it once per node with NewExpect.
type Expect struct {
	Package  string
	FuncName string
	HasRecv  bool
	// Declared lists the names the rest of the contract declares at package
	// level. A helper in a reply may not reuse one. Callers MUST fill it (an
	// empty non-nil slice means the contract declares nothing else): nil fails
	// closed, and every helper declaration in the reply is then refused.
	Declared []string
	canon    string
	recvBase string
}

// Size caps on a reply, checked before anything is parsed.
const (
	MaxReplyBytes = 256 << 10
	MaxReplyLines = 6000
)

// canonical renders a function head without body, doc, comments or layout.
// The type parameter list (names and constraints) is part of it.
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
	b.WriteString("|" + fd.Name.Name + "|")
	if tp := fd.Type.TypeParams; tp != nil {
		b.WriteString("[")
		for _, f := range tp.List {
			for _, n := range f.Names {
				b.WriteString(n.Name + ",")
			}
			b.WriteString(" " + types.ExprString(f.Type) + ";")
		}
		b.WriteString("]")
	}
	b.WriteString(types.ExprString(fd.Type))
	return b.String()
}

// recvBase names the receiver's base type ("" for a plain function).
func recvBase(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return ""
	}
	x := fd.Recv.List[0].Type
	for {
		switch v := x.(type) {
		case *ast.StarExpr:
			x = v.X
		case *ast.ParenExpr:
			x = v.X
		case *ast.IndexExpr:
			x = v.X
		case *ast.IndexListExpr:
			x = v.X
		case *ast.Ident:
			return v.Name
		default:
			return ""
		}
	}
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
	return Expect{Package: pkg, FuncName: fd.Name.Name, HasRecv: fd.Recv != nil, canon: canonical(fd), recvBase: recvBase(fd)}, nil
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
	if len(text) > MaxReplyBytes {
		return r, malformed(fmt.Sprintf("reply too large (%d bytes, limit %d)", len(text), MaxReplyBytes))
	}
	if n := strings.Count(text, "\n") + 1; n > MaxReplyLines {
		return r, malformed(fmt.Sprintf("reply has too many lines (%d, limit %d)", n, MaxReplyLines))
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
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == e.FuncName && (fd.Recv != nil) == e.HasRecv && recvBase(fd) == e.recvBase {
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
	if kind := gate(f, e, found[0]); kind != "" {
		return r, malformed(kind)
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

var forbiddenDirectives = []string{"//go:generate", "//go:linkname", "//go:embed", "//go:build", "//go:cgo", "//export ", "// +build", "//+build"}

// processControl lists the calls that could end or fake the end of a test run.
var processControl = map[string]map[string]bool{
	"os":      {"Exit": true},
	"syscall": {"Exit": true},
	"log":     {"Fatal": true, "Fatalf": true, "Fatalln": true, "Panic": true, "Panicf": true, "Panicln": true},
	"runtime": {"Goexit": true},
}

// exitMethods are the method and function names the final source scan of the
// executor (scan.go) refuses on any receiver: a logger reached through a
// variable, a parameter or an interface can end the process. The reply gate
// refuses the same names so a reply the scan would refuse never reaches it.
var exitMethods = map[string]bool{"Fatal": true, "Fatalf": true, "Fatalln": true, "Panic": true, "Panicf": true, "Panicln": true}

// gate enforces, on every reply, what leaf code may do. It returns a fixed
// kind and never any reply text. The runner cannot tell a real pass from an
// init() that prints pass lines and calls os.Exit(0); this is the mitigation.
func gate(f *ast.File, e Expect, target *ast.FuncDecl) string {
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			for _, p := range forbiddenDirectives {
				if strings.HasPrefix(c.Text, p) {
					return "directive not allowed"
				}
			}
		}
	}
	alias := map[string]string{}
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		name := p[strings.LastIndex(p, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if p == "testing" {
			return "testing import not allowed in model source"
		}
		if name == "." && (processControl[p] != nil || p == "flag") {
			return "dot import of a process-control package"
		}
		alias[name] = p
	}
	taken := map[string]bool{}
	for _, n := range e.Declared {
		taken[n] = true
	}
	local := map[string]bool{}
	for _, d := range f.Decls {
		if gd, ok := d.(*ast.GenDecl); ok {
			for _, s := range gd.Specs {
				if ts, ok := s.(*ast.TypeSpec); ok {
					local[ts.Name.Name] = true
				}
			}
		}
	}
	okName := func(n string) bool {
		if e.Declared == nil {
			return false
		}
		for _, r := range n {
			return unicode.IsLower(r) && !taken[n]
		}
		return false
	}
	const bad = "declaration not allowed (init, exported, blank, foreign method or name collision)"
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d == target {
				continue
			}
			if d.Recv == nil && d.Name.Name == "init" {
				return "init function not allowed"
			}
			if d.Recv != nil {
				if !local[recvBase(d)] {
					return bad
				}
			} else if !okName(d.Name.Name) {
				return bad
			}
		case *ast.GenDecl:
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					if !okName(s.Name.Name) {
						return bad
					}
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if !okName(n.Name) {
							return bad
						}
					}
				}
			}
		}
	}
	kind := ""
	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.SelectorExpr:
			if exitMethods[v.Sel.Name] {
				kind = "forbidden call (process exit or fatal log)"
			}
			if x, ok := v.X.(*ast.Ident); ok {
				if processControl[alias[x.Name]][v.Sel.Name] {
					kind = "forbidden call (process exit or fatal log)"
				}
				if alias[x.Name] == "flag" && (v.Sel.Name == "ExitOnError" || v.Sel.Name == "ContinueOnError") {
					kind = "forbidden flag error handling (ExitOnError)"
				}
			}
		case *ast.Ident:
			if v.Name == "recover" {
				kind = "forbidden call (recover)"
			}
		}
		return kind == ""
	})
	return kind
}
