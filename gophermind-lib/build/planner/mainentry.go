package planner

import (
	"path"
	"strings"
)

// mainEntryTest is the test of a func main() in a main package, written by the
// harness instead of a model: main takes and returns nothing, so the only test
// is that it exists, and its behaviour is the acceptance commands' job. The
// model got even this wrong (it called `_ = main()`, which does not compile, and
// named the test TestMain, which Go reserves), and planning stopped there. ok is
// false for any other function.
func mainEntryTest(sig, file, funcName string) (tests []map[string]any, src string, ok bool) {
	if strings.Join(strings.Fields(sig), " ") != "func main()" || PackageClause(path.Dir(file)) != "main" {
		return nil, "", false
	}
	tests = []map[string]any{{
		"name": "entry point exists", "level": "unit",
		"given":   "the main package",
		"expect":  "func main is declared; its behaviour is checked by the acceptance commands",
		"command": testCommand(file, funcName), "polarity": "success", "covers": "happy",
	}}
	src = "package main\n\nimport \"testing\"\n\nfunc " + funcName + "(t *testing.T) {\n\t_ = main\n}\n"
	return tests, src, true
}
