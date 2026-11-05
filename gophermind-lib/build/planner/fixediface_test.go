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
	if !strings.Contains(out, "<fixed_interfaces>\ncmd/slugify: func main()\n</fixed_interfaces>") {
		t.Errorf("prompt lacks the fixed interfaces block:\n%s", out)
	}
}
