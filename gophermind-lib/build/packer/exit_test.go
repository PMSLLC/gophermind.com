package packer

import (
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/contract"
)

const exitMain = "package main\n\nimport (\n\t\"os\"\n\n\t\"example.com/m/internal/cli\"\n)\n\n// main exits with the code cli.Run returns.\nfunc main() {\n\tos.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))\n}\n"

func exitExpect(t *testing.T, pkg, sig string) Expect {
	t.Helper()
	e, err := NewExpect(pkg, sig)
	if err != nil {
		t.Fatal(err)
	}
	e.Declared = []string{}
	e.AllowExit = AllowsExit(pkg, sig)
	return e
}

func TestExitAcceptedInValidatedMain(t *testing.T) {
	e := exitExpect(t, "main", "func main()")
	if !e.AllowExit {
		t.Fatal("AllowsExit(main, func main()) = false, want true")
	}
	if _, err := ParseReply(exitMain, e); err != nil {
		t.Fatalf("ParseReply refused os.Exit in the validated main: %v", err)
	}
	// syscall.Exit is the same process action.
	src := strings.Replace(strings.Replace(exitMain, "\"os\"\n", "\"os\"\n\t\"syscall\"\n", 1), "os.Exit(", "syscall.Exit(", 1)
	if _, err := ParseReply(src, e); err != nil {
		t.Fatalf("ParseReply refused syscall.Exit in the validated main: %v", err)
	}
}

func TestExitRefusedInRun(t *testing.T) {
	sig := "func Run(args []string, stdout, stderr io.Writer) int"
	e := exitExpect(t, "cli", sig)
	if e.AllowExit {
		t.Fatal("AllowsExit(cli, Run) = true, want false")
	}
	src := "package cli\n\nimport (\n\t\"io\"\n\t\"os\"\n)\n\n// Run runs.\nfunc Run(args []string, stdout, stderr io.Writer) int {\n\tos.Exit(1)\n\treturn 1\n}\n"
	_, err := ParseReply(src, e)
	if err == nil || ErrorClass(err) != ClassForbidden || ErrorReason(err) != "forbidden:forbidden_call" {
		t.Fatalf("err = %v (reason %q), want forbidden:forbidden_call", err, ErrorReason(err))
	}
}

func TestExitRefusedOutsideMain(t *testing.T) {
	// Package main, but not the validated entry: a helper signature.
	e := exitExpect(t, "main", "func run() int")
	if e.AllowExit {
		t.Fatal("AllowsExit(main, func run() int) = true, want false")
	}
	src := "package main\n\nimport \"os\"\n\n// run runs.\nfunc run() int {\n\tos.Exit(2)\n\treturn 2\n}\n"
	if _, err := ParseReply(src, e); ErrorClass(err) != ClassForbidden {
		t.Fatalf("err = %v, want forbidden", err)
	}
	// AllowExit set by hand still covers only the body of the target main: a
	// helper in the same file that exits is refused.
	m := exitExpect(t, "main", "func main()")
	helper := "package main\n\nimport \"os\"\n\nfunc quit() { os.Exit(3) }\n\n// main quits.\nfunc main() {\n\tquit()\n}\n"
	if _, err := ParseReply(helper, m); ErrorClass(err) != ClassForbidden {
		t.Fatalf("helper exit: err = %v, want forbidden", err)
	}
	// log.Fatal stays refused in the validated main.
	fatal := "package main\n\nimport \"log\"\n\n// main fails.\nfunc main() {\n\tlog.Fatal(\"x\")\n}\n"
	if _, err := ParseReply(fatal, m); ErrorClass(err) != ClassForbidden {
		t.Fatalf("log.Fatal in main: err = %v, want forbidden", err)
	}
}

const mainContractsJSON = `{
 "spec_version": "2.0", "brief_id": "b", "revision": 0, "module": "example.com/m",
 "conventions": {"layout": ["x"], "naming": ["y"], "errors": "z"},
 "types": [],
 "functions": [
  {"id": "fn-main", "package": "main", "file": "cmd/hello/main.go", "signature": "func main()", "doc": "main exits with the code cli.Run returns.", "uses": [], "component": "c"},
  {"id": "fn-run", "package": "cli", "file": "internal/cli/run.go", "signature": "func Run() int", "doc": "Run returns a code.", "uses": [], "component": "c"}
 ],
 "components": []
}`

func TestImplementPromptNamesExitOnlyForMain(t *testing.T) {
	c, err := contract.Load([]byte(mainContractsJSON))
	if err != nil {
		t.Fatal(err)
	}
	pack := func(n NodeView) string {
		t.Helper()
		p, err := Pack(n, c, Inputs{Budget: 8000})
		if err != nil {
			t.Fatal(err)
		}
		return p.Text
	}
	main := pack(NodeView{ID: "fn-main", FuncID: "fn-main", Package: "main", File: "cmd/hello/main.go", Signature: "func main()", TestFile: "cmd/hello/main_test.go", TestSource: "package main\n"})
	if !strings.Contains(main, "Call os.Exit only once, at the end of main, with the result of the entry call.") || strings.Contains(main, "Never call os.Exit") {
		t.Error("the prompt of the validated main must allow one os.Exit and not forbid it")
	}
	if !strings.Contains(main, "log.Fatal, log.Panic, runtime.Goexit or recover") {
		t.Error("the prompt of main must keep the rest of the rule")
	}
	run := pack(NodeView{ID: "fn-run", FuncID: "fn-run", Package: "cli", File: "internal/cli/run.go", Signature: "func Run() int", TestFile: "internal/cli/run_test.go", TestSource: "package cli\n"})
	if !strings.Contains(run, "Never call os.Exit, log.Fatal, log.Panic, runtime.Goexit or recover") || strings.Contains(run, "Call os.Exit only once") {
		t.Error("the prompt of a non-entry function must forbid os.Exit")
	}
}
