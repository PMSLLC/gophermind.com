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
	return map[string]any{"module": "slugify", "functions": list,
		"components": []any{map[string]any{"id": "cli-logic", "package": "internal/cli"}}}
}

func TestFixedInterfaceSignatureRestored(t *testing.T) {
	const want = "func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int"
	run := map[string]any{"id": "fn-run", "signature": "func run(args []string, in io.Reader, out, errw io.Writer) int"}
	doc := cliDoc(run)
	applied, missing := applyFixedInterfaces(doc, "internal/cli", fixedInterfaces(archOf(t, "slugify")))
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
	_, missing := applyFixedInterfaces(doc, "internal/cli", fixedInterfaces(archOf(t, "slugify")))
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

func TestFixedInterfacePrefersTheExactName(t *testing.T) {
	const want = "func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int"
	const helper = "func run(cfg config) int"
	run := map[string]any{"id": "fn-run", "signature": helper}
	exported := map[string]any{"id": "fn-cli-run", "signature": "func Run(args []string, in io.Reader, out, errw io.Writer) int"}
	doc := cliDoc(run, exported)
	applied, missing := applyFixedInterfaces(doc, "internal/cli", fixedInterfaces(archOf(t, "slugify")))
	if run["signature"] != helper {
		t.Errorf("helper run signature = %v, want it untouched", run["signature"])
	}
	if exported["signature"] != want {
		t.Errorf("Run signature = %v, want %s", exported["signature"], want)
	}
	if len(applied) != 1 || !strings.HasPrefix(applied[0], "fn-cli-run: ") || len(missing) != 0 {
		t.Errorf("applied = %v, missing = %v", applied, missing)
	}
}

func TestFixedInterfaceIgnoresMethods(t *testing.T) {
	method := map[string]any{"id": "fn-server-run", "signature": "func (s *Server) Run(args []string) int"}
	doc := cliDoc(method)
	_, missing := applyFixedInterfaces(doc, "internal/cli", fixedInterfaces(archOf(t, "slugify")))
	if len(missing) != 1 || missing[0].Name != "Run" {
		t.Errorf("missing = %v, want the Run interface", missing)
	}
	if method["signature"] != "func (s *Server) Run(args []string) int" {
		t.Errorf("method signature changed to %v", method["signature"])
	}
}

func TestFixedInterfaceAmbiguousCaseIsMissing(t *testing.T) {
	a := map[string]any{"id": "fn-run", "signature": "func run(args []string) int"}
	b := map[string]any{"id": "fn-run-upper", "signature": "func RUN(args []string) int"}
	doc := cliDoc(a, b)
	applied, missing := applyFixedInterfaces(doc, "internal/cli", fixedInterfaces(archOf(t, "slugify")))
	if len(applied) != 0 || len(missing) != 1 || missing[0].Name != "Run" {
		t.Errorf("applied = %v, missing = %v; want Run missing and nothing applied", applied, missing)
	}
}

// twoCliDoc is a doc with two components in internal/cli; fns belong to the
// first unless they name a component.
func twoCliDoc(fns ...map[string]any) map[string]any {
	var list []any
	for _, f := range fns {
		if f["component"] == nil {
			f["component"] = "cli-logic"
		}
		list = append(list, f)
	}
	return map[string]any{"module": "slugify", "functions": list, "components": []any{
		map[string]any{"id": "cli-logic", "package": "internal/cli"},
		map[string]any{"id": "cli-io", "package": "internal/cli"},
	}}
}

func TestFixedInterfaceDeclaredBySiblingComponentIsNotMissing(t *testing.T) {
	run := map[string]any{"id": "fn-run", "signature": "func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int"}
	read := map[string]any{"id": "fn-read-input", "component": "cli-io", "signature": "func readInput(r io.Reader) (string, error)"}
	doc := twoCliDoc(run, read)
	ifs := fixedInterfaces(archOf(t, "slugify"))
	// cli-io is processed with its directory, internal/cli: Run is declared
	// there by its sibling cli-logic, so nothing is missing.
	if _, missing := applyFixedInterfaces(doc, "internal/cli", ifs); len(missing) != 0 {
		t.Errorf("missing = %v, want none", missing)
	}
	// With no Run in the directory at all, it is missing.
	if _, missing := applyFixedInterfaces(twoCliDoc(read), "internal/cli", ifs); len(missing) != 1 || missing[0].Name != "Run" {
		t.Errorf("no Run anywhere: missing = %v, want Run", missing)
	}
}

func TestFixedInterfaceWithoutAComponentWarns(t *testing.T) {
	doc := map[string]any{"module": "slugify", "components": []any{
		map[string]any{"id": "cli-logic", "package": "internal/cli"},
		map[string]any{"id": "entry-point", "package": "slugify/cmd/slugify"},
	}}
	got := unownedFixedInterfaces(doc, fixedInterfaces(archOf(t, "slugify")), "slugify")
	if len(got) != 1 || got[0].Dir != "internal/slug" {
		t.Fatalf("unowned = %v, want the internal/slug interface", got)
	}
	if text, want := unownedText(got[0]), "fixed_interface_unowned: internal/slug: func Make(s string, max int) string"; text != want {
		t.Errorf("warning = %q, want %q", text, want)
	}
}

func TestEntryCallSkipsStandardLibraryQualifiers(t *testing.T) {
	const others = "- `internal/slug`: `Make(s string, max int) string` is pure.\n" +
		"- `internal/cli`: `Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int` runs.\n"
	for _, bullet := range []string{
		"- `cmd/slugify` (package `main`): calls `cli.Run` with `os.Args[1:]`, `os.Stdout` and `os.Stderr`.\n",
		"- `cmd/slugify` (package `main`): passes `os.Args[1:]` to `cli.Run` and exits with its code.\n",
	} {
		if pkg, name := entryCall(bullet + others); pkg != "cli" || name != "Run" {
			t.Errorf("entryCall(%q) = %q, %q; want cli, Run", bullet, pkg, name)
		}
	}
	// No qualifier names an Architecture package: no entry call.
	if pkg, name := entryCall("- `cmd/x`: passes `os.Args[1:]` on.\n" + others); pkg != "" || name != "" {
		t.Errorf("entryCall with only os qualifiers = %q, %q; want empty", pkg, name)
	}
}

func TestFixedInterfacesSkipCallExamples(t *testing.T) {
	arch := "- `internal/slug`: `Make(s string, max int) string` lowercases; `Make(s, 0)` means no limit, as does `Make(text, max)`.\n"
	wantFixed(t, fixedInterfaces(arch), []fixedIface{
		{"internal/slug", "Make", "func Make(s string, max int) string"},
	})
}

func TestOwnPackageIsSymmetric(t *testing.T) {
	cases := []struct{ pkg, comp string }{
		{"internal/greet", "greet"},
		{"greet", "internal/greet"},
		{"hello/internal/greet", "greet"},
	}
	for _, c := range cases {
		if !ownPackage(c.pkg, c.comp, "hello") {
			t.Errorf("ownPackage(%q, %q) = false, want true", c.pkg, c.comp)
		}
	}
	if ownPackage("internal/cli", "greet", "hello") {
		t.Errorf("ownPackage(internal/cli, greet) = true, want false")
	}
}

func TestModuleRootMainIsNotForeign(t *testing.T) {
	reply := `{"functions": [{"id": "fn-main", "package": "main", "file": "main.go", "signature": "func main()"}]}`
	for _, comp := range []string{"slugify", "."} {
		if _, dropped := dropForeignFunctions(reply, comp, "slugify"); len(dropped) != 0 {
			t.Errorf("module-root component %q dropped %v, want its main kept", comp, dropped)
		}
	}
}

func TestFixedDirIsModuleStripped(t *testing.T) {
	arch := "- `example.com/slugify/internal/slug`: `Make(s string, max int) string` is pure.\n"
	mk := map[string]any{"id": "fn-make", "component": "slug-logic", "signature": "func make(s string, n int) string"}
	doc := map[string]any{"module": "example.com/slugify", "functions": []any{mk}, "components": []any{
		map[string]any{"id": "slug-logic", "package": "internal/slug"},
	}}
	ifs := fixedInterfaces(arch)
	applied, missing := applyFixedInterfaces(doc, "internal/slug", ifs)
	if len(applied) != 1 || len(missing) != 0 || mk["signature"] != "func Make(s string, max int) string" {
		t.Errorf("applied = %v, missing = %v, signature = %v", applied, missing, mk["signature"])
	}
	if got := unownedFixedInterfaces(doc, ifs, "example.com/slugify"); len(got) != 0 {
		t.Errorf("unowned = %v, want none", got)
	}
}

func TestMainDefectTextNamesNoRunFunction(t *testing.T) {
	if msg := mainDefectText("cmd/slugify"); strings.Contains(msg, "run function") || !strings.Contains(msg, "exits with the returned code") {
		t.Errorf("main defect = %q", msg)
	}
}

// The first of two components in internal/cli declares nothing fixed: Run is
// not asked of it, because its sibling comes later and declares Run; after
// the sibling's pass nothing is missing.
func TestFixedInterfaceWaitsForTheSiblingComponent(t *testing.T) {
	ifs := fixedInterfaces(archOf(t, "slugify"))
	parse := map[string]any{"id": "fn-parse-args", "signature": "func parseArgs(args []string) (int, error)"}
	doc := twoCliDoc(parse)
	if _, missing := fixedMissingNow(doc, "cli-logic", "internal/cli", ifs, nil); len(missing) != 0 {
		t.Errorf("first component: missing = %v, want none while cli-io is still to come", missing)
	}
	doc["functions"] = append(doc["functions"].([]any),
		map[string]any{"id": "fn-run", "component": "cli-io", "signature": "func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int"})
	if _, missing := fixedMissingNow(doc, "cli-io", "internal/cli", ifs, []string{"cli-logic"}); len(missing) != 0 {
		t.Errorf("second component: missing = %v, want none", missing)
	}
	// The last component of the directory is asked when Run is still missing.
	if _, missing := fixedMissingNow(twoCliDoc(parse), "cli-io", "internal/cli", ifs, []string{"cli-logic"}); len(missing) != 1 {
		t.Errorf("last component without Run: missing = %v, want Run", missing)
	}
}
