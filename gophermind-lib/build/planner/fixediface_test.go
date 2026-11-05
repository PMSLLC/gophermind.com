package planner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// archOf is the Architecture section of a fixture brief.
func archOf(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", dir, "brief.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(string(raw), "## Architecture\n")
	if !ok {
		t.Fatalf("%s brief has no Architecture section", dir)
	}
	arch, _, _ := strings.Cut(rest, "\n## ")
	return arch
}

// wantFixed checks that ifs holds exactly want, matched by Dir.
func wantFixed(t *testing.T, ifs []fixedIface, want []fixedIface) {
	t.Helper()
	if len(ifs) != len(want) {
		t.Fatalf("fixed interfaces = %v, want %v", ifs, want)
	}
	byDir := map[string]fixedIface{}
	for _, f := range ifs {
		byDir[f.Dir] = f
	}
	for _, w := range want {
		if got, ok := byDir[w.Dir]; !ok || got != w {
			t.Errorf("interface of %s = %+v, want %+v", w.Dir, got, w)
		}
	}
}

func TestFixedInterfacesOfSlugifyArchitecture(t *testing.T) {
	arch := archOf(t, "slugify")
	wantFixed(t, fixedInterfaces(arch), []fixedIface{
		{"internal/slug", "Make", "func Make(s string, max int) string"},
		{"internal/cli", "Run", "func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int"},
		{"cmd/slugify", "main", "func main()"},
	})
	if pkg, name := entryCall(arch); pkg != "cli" || name != "Run" {
		t.Errorf("entryCall = %q, %q; want cli, Run", pkg, name)
	}
}

func TestFixedInterfacesOfHelloArchitecture(t *testing.T) {
	arch := archOf(t, "hello")
	wantFixed(t, fixedInterfaces(arch), []fixedIface{
		{"internal/greet", "Greeting", "func Greeting(name string) string"},
		{"internal/cli", "Run", "func Run(args []string, stdout, stderr io.Writer) int"},
		{"cmd/hello", "main", "func main()"},
	})
	if pkg, name := entryCall(arch); pkg != "cli" || name != "Run" {
		t.Errorf("entryCall = %q, %q; want cli, Run", pkg, name)
	}
}

func TestFixedInterfacesOfEmptyArchitecture(t *testing.T) {
	if ifs := fixedInterfaces(""); len(ifs) != 0 {
		t.Errorf("fixedInterfaces(\"\") = %v, want none", ifs)
	}
	if pkg, name := entryCall(""); pkg != "" || name != "" {
		t.Errorf("entryCall(\"\") = %q, %q; want empty", pkg, name)
	}
}

func cliDoc(fns ...map[string]any) map[string]any {
	var list []any
	for _, f := range fns {
		f["component"] = "cli-logic"
		list = append(list, f)
	}
	return map[string]any{"module": "slugify", "functions": list}
}

func TestFixedInterfaceSignatureRestored(t *testing.T) {
	const want = "func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int"
	run := map[string]any{"id": "fn-run", "signature": "func run(args []string, in io.Reader, out, errw io.Writer) int"}
	doc := cliDoc(run)
	applied, missing := applyFixedInterfaces(doc, "cli-logic", "internal/cli", fixedInterfaces(archOf(t, "slugify")))
	if got := run["signature"]; got != want {
		t.Errorf("signature = %v, want %s", got, want)
	}
	if m := funcSigNameRE.FindStringSubmatch(run["signature"].(string)); m == nil || m[1] != "Run" {
		t.Errorf("name = %v, want Run", m)
	}
	if len(applied) != 1 {
		t.Errorf("applied = %v, want one entry", applied)
	}
	if len(missing) != 0 {
		t.Errorf("missing = %v, want none", missing)
	}
}

func TestFixedInterfaceMissingIsADefect(t *testing.T) {
	doc := cliDoc(map[string]any{"id": "fn-parse-args", "signature": "func parseArgs(args []string) (int, error)"})
	_, missing := applyFixedInterfaces(doc, "cli-logic", "internal/cli", fixedInterfaces(archOf(t, "slugify")))
	if len(missing) != 1 || missing[0].Name != "Run" {
		t.Fatalf("missing = %v, want the Run interface", missing)
	}
	if text := fixedDefectText(missing); !strings.Contains(text, "func Run(args []string") {
		t.Errorf("defect text = %q", text)
	}
}

func TestComponentPromptNamesTheFixedEntryCall(t *testing.T) {
	out, err := render("contract_component", map[string]string{
		"Defect": "", "Surface": "", "Component": "c", "Outline": "o", "Declared": "d", "Written": "w",
		"BriefSection": "bs", "Answers": "a", "ItemSchemas": "i", "EntryCall": "cli.Run",
		"Fixed": "cmd/slugify: func main()",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "calls `os.Exit` with the result of `cli.Run`") {
		t.Errorf("prompt does not name the entry call:\n%s", out)
	}
	if strings.Contains(out, "`run` has the signature") {
		t.Errorf("prompt still asks for run:\n%s", out)
	}
	if !strings.Contains(out, "for each remaining parameter of its fixed signature, the matching `os.Stdin`, `os.Stdout` or `os.Stderr`") {
		t.Errorf("prompt does not tie the os streams to the fixed signature:\n%s", out)
	}
	if !strings.Contains(out, "<fixed_interfaces>\ncmd/slugify: func main()\n</fixed_interfaces>") {
		t.Errorf("prompt lacks the fixed interfaces block:\n%s", out)
	}
}

func TestForeignFunctionsDroppedFromComponentReply(t *testing.T) {
	reply := `{"types": [], "functions": [` +
		`{"id": "fn-main", "package": "main", "file": "cmd/slugify/main.go", "signature": "func main()"},` +
		`{"id": "fn-parse-args", "package": "cli", "file": "internal/cli/args.go", "signature": "func parseArgs(args []string) error"},` +
		`{"id": "fn-helper", "package": "slugify/internal/cli", "signature": "func helper()"}` +
		`], "more": false}`
	out, dropped := dropForeignFunctions(reply, "internal/cli", "slugify")
	if len(dropped) != 1 || dropped[0] != "fn-main (package main)" {
		t.Fatalf("dropped = %v, want fn-main", dropped)
	}
	if strings.Contains(out, "fn-main") || !strings.Contains(out, "fn-parse-args") || !strings.Contains(out, "fn-helper") {
		t.Errorf("reply after the drop = %s", out)
	}
	if got, want := foreignDroppedText("cli-logic", dropped), "component_foreign_function_dropped: cli-logic: fn-main (package main)"; got != want {
		t.Errorf("warning = %q, want %q", got, want)
	}
	// The entry component keeps its own main, written as package main.
	if _, dropped := dropForeignFunctions(reply, "cmd/slugify", "slugify"); len(dropped) != 2 {
		t.Errorf("entry component dropped %v, want the two cli functions", dropped)
	}
	// func main() outside cmd/ goes even when its package is the component's.
	inCli := `{"functions": [{"id": "fn-main", "package": "internal/cli", "signature": "func main()"}]}`
	if _, dropped := dropForeignFunctions(inCli, "internal/cli", "slugify"); len(dropped) != 1 {
		t.Errorf("func main() in internal/cli was kept")
	}
}

func TestComponentPromptNamesTheSurfaceTarget(t *testing.T) {
	out, err := render("contract_component", map[string]string{
		"Defect": "", "Surface": "--max N", "SurfaceName": "Run", "Component": "c", "Outline": "o", "Declared": "d",
		"Written": "w", "BriefSection": "bs", "Answers": "a", "ItemSchemas": "i", "EntryCall": "", "Fixed": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "The doc of `Run` must state every item below") {
		t.Errorf("surface paragraph does not name Run:\n%s", out)
	}
}
