package planner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"

	"gophermind/gophermind-lib/briefv2/contract"
	"gophermind/gophermind-lib/briefv2/pathsafe"
	"gophermind/gophermind-lib/briefv2/router"
)

// maxAcceptanceSource caps one acceptance test file.
const maxAcceptanceSource = 64 * 1024

// acceptKey is the testwriterState key of a Go acceptance test; node ids never
// contain a colon.
func acceptKey(b goBullet) string { return "accept:" + b.Requirement }

// forbiddenAcceptImports start processes, load code or run servers: an
// acceptance test only talks to the server the harness started.
var forbiddenAcceptImports = map[string]bool{
	"os/exec": true, "syscall": true, "plugin": true, "unsafe": true, "os/signal": true,
	"net/http/httptest": true, "net/http/httputil": false,
}

// writeAcceptanceTests writes acceptance/<id>_test.go for every Go acceptance
// bullet of the plan, each from one model call, under the same rules as the
// leaf test files.
func (p *Planner) writeAcceptanceTests(ctx context.Context, r *run, c *contract.Contracts, st *testwriterState) error {
	cov, err := ReadCoverage(r.dir)
	if err != nil {
		return err
	}
	for _, b := range goBullets(r.reqs, cov.Serve) {
		key := acceptKey(b)
		if _, done := st.Nodes[key]; done {
			continue
		}
		wt, err := p.writeAcceptanceTest(ctx, r, c, b, cov.Serve, st)
		if err != nil {
			return fmt.Errorf("acceptance test %s: %w", b.Requirement, err)
		}
		st.Nodes[key] = wt
		delete(st.Pending, key)
		if err := writeJSON(r.path(stateTestwriter), *st); err != nil {
			return err
		}
	}
	return nil
}

func (p *Planner) writeAcceptanceTest(ctx context.Context, r *run, c *contract.Contracts, b goBullet, serve *Serve, st *testwriterState) (writtenTests, error) {
	key := acceptKey(b)
	abs, err := pathsafe.ResolveTest(r.repo, b.File)
	if err != nil {
		return writtenTests{}, err
	}
	if _, err := os.Lstat(abs); err == nil {
		if pend, ok := st.Pending[key]; ok {
			if raw, err := os.ReadFile(abs); err == nil && hashHex(raw) == pend.SHA256 {
				return pend, nil
			}
		}
		return writtenTests{}, fmt.Errorf("test file %s already exists and this run did not write it; move it away, then resume", b.File)
	}
	var text string
	for _, q := range r.reqs {
		if q.ID == b.Requirement {
			text = q.Text
		}
	}
	prefix := strings.ToLower(b.Requirement)
	var source string
	cs := callSpec{stage: "testwrite:accept-" + b.Requirement, taskType: "testwrite", scope: router.ScopeBrief, maxTokens: maxTokensTestwrite}
	build := func(level int) (string, error) {
		return render("testwriter_accept", map[string]string{
			"ID": b.Requirement, "Text": text, "Func": b.Func, "File": b.File, "Prefix": prefix,
			"Serve": serve.Command, "Ready": serve.Ready,
			"Excerpt": briefExcerpt(r, allFeatureNames(r), level)})
	}
	err = p.callSized(ctx, r, cs, build, func(reply string) error {
		var rep struct {
			TestFile string `json:"test_file"`
		}
		if err := json.Unmarshal([]byte(StripReply(reply)), &rep); err != nil {
			return fmt.Errorf("acceptance test reply is not a JSON object (%s)", jsonErr(err))
		}
		src, err := checkAcceptanceSource(rep.TestFile, b)
		if err != nil {
			return err
		}
		source = src
		return nil
	})
	if err != nil {
		return writtenTests{}, err
	}
	wt := writtenTests{Tests: []map[string]any{}, TestFile: b.File, SHA256: hashHex([]byte(source))}
	if st.Pending == nil {
		st.Pending = map[string]writtenTests{}
	}
	st.Pending[key] = wt
	if err := writeJSON(r.path(stateTestwriter), *st); err != nil {
		return writtenTests{}, err
	}
	beforeTestWrite()
	if err := placeTestFile(r, abs, b.File, source); err != nil {
		return writtenTests{}, err
	}
	return wt, nil
}

// checkAcceptanceSource checks a model-written acceptance test and returns the
// file to write: the build line is the harness's (a model-written one is
// dropped), the package is acceptance, only the standard library is imported
// and nothing that starts a process or a server, there is exactly one test
// function (named for the bullet) that reads GM_ACCEPTANCE_URL and calls
// t.Fatal, nothing skips, and every other top-level name carries the bullet's
// prefix. No error quotes the source.
func checkAcceptanceSource(src string, b goBullet) (string, error) {
	if len(src) > maxAcceptanceSource {
		return "", fmt.Errorf("acceptance test file is %d bytes; the cap is %d", len(src), maxAcceptanceSource)
	}
	var kept []string
	pastPackage := false
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		if !pastPackage {
			if strings.HasPrefix(t, "package ") {
				pastPackage = true
			} else if strings.HasPrefix(t, "//go:build") || strings.HasPrefix(t, "// +build") {
				continue
			}
		}
		kept = append(kept, line)
	}
	final := "//go:build " + AcceptanceTag + "\n\n" + strings.TrimLeft(strings.Join(kept, "\n"), "\n")
	if !strings.HasSuffix(final, "\n") {
		final += "\n"
	}
	f, err := parser.ParseFile(token.NewFileSet(), "", final, parser.SkipObjectResolution)
	if err != nil {
		return "", fmt.Errorf("acceptance test file (%d bytes) is not valid Go: %s", len(src), syntaxErr(err))
	}
	if f.Name.Name != AcceptanceDir {
		return "", errors.New("acceptance test file is not in package " + AcceptanceDir)
	}
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return "", errors.New("acceptance test file has an unreadable import")
		}
		first, _, _ := strings.Cut(path, "/")
		if !stdTopLevel[first] || forbiddenAcceptImports[path] {
			return "", fmt.Errorf("acceptance test file imports a package (%d bytes) that is not allowed; only the standard library, without os/exec, syscall or httptest", len(path))
		}
	}
	prefix := strings.ToLower(b.Requirement)
	named := func(name string) bool { return strings.HasPrefix(name, prefix) }
	foundTest := false
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv != nil {
				continue // a method belongs to a type, whose name is checked
			}
			if d.Name.Name == b.Func {
				if !isTestSignature(d) {
					return "", fmt.Errorf("acceptance test file declares %s but not as func %s(t *testing.T)", b.Func, b.Func)
				}
				foundTest = true
				continue
			}
			if !named(d.Name.Name) {
				return "", fmt.Errorf("acceptance test file declares a function (%d bytes) that is neither %s nor named with the prefix %s", len(d.Name.Name), b.Func, prefix)
			}
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				continue
			}
			for _, spec := range d.Specs {
				var names []*ast.Ident
				switch sp := spec.(type) {
				case *ast.TypeSpec:
					names = []*ast.Ident{sp.Name}
				case *ast.ValueSpec:
					names = sp.Names
				}
				for _, n := range names {
					if !named(n.Name) {
						return "", fmt.Errorf("acceptance test file declares a name (%d bytes) without the prefix %s", len(n.Name), prefix)
					}
				}
			}
		}
	}
	if !foundTest {
		return "", fmt.Errorf("acceptance test file has no func %s(t *testing.T)", b.Func)
	}
	var readsURL, fatal bool
	var skips int
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "Skip", "Skipf", "SkipNow":
			skips++
		case "Fatal", "Fatalf", "FailNow":
			fatal = true
		case "Getenv":
			if x, ok := sel.X.(*ast.Ident); ok && x.Name == "os" && len(call.Args) == 1 {
				if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if v, err := strconv.Unquote(lit.Value); err == nil && v == "GM_ACCEPTANCE_URL" {
						readsURL = true
					}
				}
			}
		}
		return true
	})
	switch {
	case skips > 0:
		return "", errors.New("acceptance test file skips; a test that cannot run must fail")
	case !readsURL:
		return "", errors.New("acceptance test file does not read GM_ACCEPTANCE_URL with os.Getenv")
	case !fatal:
		return "", errors.New("acceptance test file never calls t.Fatal or t.Fatalf, so it cannot fail")
	}
	return final, nil
}

// isTestSignature reports func Name(t *testing.T).
func isTestSignature(fd *ast.FuncDecl) bool {
	ps := fd.Type.Params
	if ps == nil || len(ps.List) != 1 || fd.Type.Results != nil {
		return false
	}
	star, ok := ps.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "T" {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && x.Name == "testing"
}
