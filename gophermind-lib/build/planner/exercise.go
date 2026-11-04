package planner

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
)

// failingCalls are the testing.TB methods that make a test fail. A file with
// none of them passes whatever the function returns.
var failingCalls = map[string]bool{
	"Error": true, "Errorf": true, "Fatal": true, "Fatalf": true, "Fail": true, "FailNow": true,
}

// checkTestExercises refuses a test file that never names the function under
// test or has no way to fail. Such a file hard-codes its expected values and
// passes against any implementation, so it proves nothing. The check is
// syntactic: a call through a helper or a method value counts, since the name
// still appears. main is skipped because its test is generated, and an empty
// name means the signature could not be read, which an earlier check reports.
func checkTestExercises(src, name string) error {
	if name == "" || name == "main" {
		return nil
	}
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution)
	if err != nil {
		// The earlier checks already reported a file that does not parse.
		return nil
	}
	var declName *ast.Ident
	named, fails := false, false
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncDecl:
			// A helper declared with the function's name is not a call to it.
			declName = x.Name
		case *ast.Ident:
			if x != declName && x.Name == name {
				named = true
			}
		case *ast.CallExpr:
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok && failingCalls[sel.Sel.Name] {
				fails = true
			}
		}
		return true
	})
	if !named {
		return fmt.Errorf("test file never calls %s, the function under test: every case must call it and compare what it returns", name)
	}
	if !fails {
		return errors.New("test file cannot fail: it has no t.Error, t.Errorf, t.Fatal or t.Fatalf; compare each case's result with the expected value and report a mismatch")
	}
	return nil
}
