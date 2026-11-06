package planner

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"gophermind/gophermind-lib/build/router"
)

// checkTestTarget refuses a test-writer packet that contradicts itself before
// any model call is made: no reply can satisfy it, and run
// hello-v1.1-20261106-r001 spent eighteen refusals finding that out on one
// node. node is the node id, sig its contract signature, file its contract
// file and testFile the test file the writer is asked for. The first reason
// that applies is reported.
func checkTestTarget(node, sig, file, testFile string) error {
	name, _, _ := internalTarget(sig)
	reason := ""
	switch {
	case isTestingFunc(sig, name):
		reason = "the function under test is a test function"
	case strings.HasSuffix(file, "_test.go"):
		reason = "the contract file is a test file"
	case testFile == file:
		reason = "the artifact and the target are the same file"
	case name == "":
		reason = "the function under test has no name"
	default:
		return nil
	}
	return fmt.Errorf("invalid_test_target: node %s (%s): %s", node, sig, reason)
}

// isTestingFunc reports whether the declared name is a Test, Benchmark, Fuzz or
// Example name and a parameter's type is from package testing (*testing.T and
// the like).
func isTestingFunc(sig, name string) bool {
	if !hasAnyPrefix(name, "Test", "Benchmark", "Fuzz", "Example") {
		return false
	}
	f, err := parser.ParseFile(token.NewFileSet(), "d.go", "package p\n"+sig, 0)
	if err != nil {
		return false
	}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Type.Params == nil {
			continue
		}
		for _, p := range fd.Type.Params.List {
			if star, ok := p.Type.(*ast.StarExpr); ok {
				if sel, ok := star.X.(*ast.SelectorExpr); ok {
					if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "testing" {
						return true
					}
				}
			}
		}
	}
	return false
}

// errTestwriterNoProgress is what a noProgressError is (errors.Is).
var errTestwriterNoProgress = errors.New("testwriter_no_progress")

// noProgressError is a test-writer refusal repeated word for word for the same
// Go block. It wraps router.ErrNoRetry, so the router asks no further, and the
// writer's loop stops with it.
type noProgressError struct {
	node    string
	refusal error
}

func (e *noProgressError) Error() string {
	return "testwriter_no_progress: node " + e.node + ": " + e.refusal.Error()
}

func (e *noProgressError) Unwrap() error { return e.refusal }

func (e *noProgressError) Is(target error) bool {
	return target == errTestwriterNoProgress || target == router.ErrNoRetry
}

// replyGoBlock is the Go source of a test-writer reply: the fenced go block,
// else the test_file field, else the whole reply.
func replyGoBlock(text string) string {
	head, fenced := splitSourceReply(text)
	if fenced != "" {
		return fenced
	}
	var reply struct {
		TestFile string `json:"test_file"`
	}
	if json.Unmarshal([]byte(StripReply(head)), &reply) == nil && reply.TestFile != "" {
		return reply.TestFile
	}
	return text
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
