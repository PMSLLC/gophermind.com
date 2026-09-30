package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func kinds(fs []Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, fmt.Sprintf("%s@%d", f.Kind, f.Line))
	}
	return out
}

func TestScanRejectsReplaceToolchainGenerateCgo(t *testing.T) {
	mod := []struct {
		name, src string
		want      []string
	}{
		{"replace line", "module x\n\nreplace x => ../y\n", []string{"replace@3"}},
		{"replace block", "module x\n\nreplace (\n a => b\n c => d\n)\n", []string{"replace@3", "replace@4", "replace@5"}},
		{"replace glued paren", "module x\nreplace(\n a => b\n)\n", []string{"replace@2", "replace@3"}},
		{"toolchain", "module x\n\ntoolchain go1.99\n", []string{"toolchain@3"}},
		{"godebug", "module x\n\ngodebug default=go1.21\n", []string{"godebug@3"}},
		{"godebug block", "module x\n\ngodebug (\n panicnil=1\n)\n", []string{"godebug@3", "godebug@4"}},
		{"exclude", "module x\n\nexclude a.b/c v1.0.0\n", []string{"exclude@3"}},
		{"exclude block", "module x\n\nexclude (\n a.b/c v1.0.0\n)\n", []string{"exclude@3", "exclude@4"}},
		{"comment is clean", "module x\n\nrequire a.b/c v1.0.0 // replace\n", nil},
		{"replacement is not a directive", "module x\n\nreplacement a.b/c v1.0.0\n", nil},
		{"require block clean", "module x\n\nrequire (\n a.b/c v1.0.0\n)\n", nil},
		{"crlf", "module x\r\n\r\nreplace x => ../y\r\n", []string{"replace@3"}},
	}
	for _, c := range mod {
		t.Run("gomod "+c.name, func(t *testing.T) {
			got := kinds(ScanGoMod([]byte(c.src)))
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("findings = %v, want %v", got, c.want)
			}
		})
	}

	src := []struct {
		name, rel, src string
		want           []string
	}{
		{"generate", "a.go", "package a\n\n//go:generate echo hi\nfunc F() {}\n", []string{"generate@3"}},
		{"linkname", "a.go", "package a\n\nimport _ \"unsafe\"\n\n//go:linkname f runtime.f\nfunc f()\n", []string{"linkname@5"}},
		{"embed", "a.go", "package a\n\nimport _ \"embed\"\n\n//go:embed x.txt\nvar s string\n", []string{"embed@5"}},
		{"cgo", "a.go", "package a\n\nimport \"C\"\n", []string{"cgo@3"}},
		{"cgo grouped", "a.go", "package a\n\nimport (\n\t\"fmt\"\n\t\"C\"\n)\n\nvar _ = fmt.Sprint\n", []string{"cgo@5"}},
		{"clean", "a.go", "package a\n\nimport \"fmt\"\n\nfunc F() string { return fmt.Sprint(1) }\n", nil},
		{"directive text in a string is clean", "a.go", "package a\n\nvar s = \"//go:generate x\"\n", nil},
	}
	for _, c := range src {
		t.Run("src "+c.name, func(t *testing.T) {
			fs, err := ScanGoSource(c.rel, []byte(c.src))
			if err != nil {
				t.Fatal(err)
			}
			if got := kinds(fs); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("findings = %v, want %v", got, c.want)
			}
			for _, f := range fs {
				if f.File != c.rel {
					t.Fatalf("File = %q, want %q", f.File, c.rel)
				}
			}
		})
	}

	flags := []struct {
		in   string
		want int
	}{
		{"-toolexec=x", 1}, {"-toolexec x", 1}, {"--overlay=f.json", 1}, {"-overlay f.json", 1},
		{"-mod=readonly -trimpath", 0}, {"", 0}, {"-toolexecx", 0},
	}
	for _, c := range flags {
		if got := len(ScanGoFlags(c.in)); got != c.want {
			t.Errorf("ScanGoFlags(%q) = %d findings, want %d", c.in, got, c.want)
		}
	}
}

func TestScanExitAndForgeryConstructs(t *testing.T) {
	const pre = "package a\n\n"
	cases := []struct {
		name, rel, src string
		want           []string
	}{
		{"os.Exit in init", "a.go", pre + "import \"os\"\n\nfunc init() { os.Exit(0) }\n", []string{"exit@5"}},
		{"os.Exit as value", "a.go", pre + "import \"os\"\n\nvar f = os.Exit\n", []string{"exit@5"}},
		{"aliased os", "a.go", pre + "import o \"os\"\n\nfunc F() { o.Exit(0) }\n", []string{"exit@5"}},
		{"dot import os", "a.go", pre + "import . \"os\"\n\nfunc F() { Exit(0) }\n", []string{"exit@5"}},
		{"syscall.Exit", "a.go", pre + "import \"syscall\"\n\nfunc F() { syscall.Exit(0) }\n", []string{"exit@5"}},
		{"log.Fatal", "a.go", pre + "import \"log\"\n\nfunc F() { log.Fatalf(\"x\") }\n", []string{"exit@5"}},
		{"log.Panicln", "a.go", pre + "import \"log\"\n\nfunc F() { log.Panicln(1) }\n", []string{"exit@5"}},
		{"logger method", "a.go", pre + "import \"log\"\n\nfunc F() { log.Default().Fatal(1) }\n", []string{"exit@5"}},
		{"runtime.Goexit", "a.go", pre + "import \"runtime\"\n\nfunc F() { runtime.Goexit() }\n", []string{"exit@5"}},
		{"recover", "a.go", pre + "func F() { defer func() { recover() }() }\n", []string{"recover@3"}},
		{"testing in non-test", "a.go", pre + "import \"testing\"\n\nvar _ testing.T\n", []string{"testing@3"}},
		{"testing in test file is fine", "a_test.go", pre + "import \"testing\"\n\nfunc TestX(t *testing.T) {}\n", nil},
		{"flag.ExitOnError outside main", "a.go", pre + "import \"flag\"\n\nvar _ = flag.ExitOnError\n", []string{"flagexit@5"}},
		{"flag.ContinueOnError outside main", "a.go", pre + "import \"flag\"\n\nvar _ = flag.ContinueOnError\n", []string{"flagexit@5"}},
		{"flag.PanicOnError is fine", "a.go", pre + "import \"flag\"\n\nvar _ = flag.PanicOnError\n", nil},
		{"shadowed os is not the package", "a.go", pre + "type T struct{}\n\nfunc (T) Exit(int) {}\n\nfunc F() { os := T{}; os.Exit(1) }\n", nil},
		{"own func named Exit is fine", "a.go", pre + "import . \"fmt\"\n\nfunc Exit(int) {}\n\nfunc F() { Exit(1); Println() }\n", nil},
		{"main may exit", "m.go", "package main\n\nimport (\n\t\"log\"\n\t\"os\"\n)\n\nfunc main() { defer func() { recover() }(); log.Fatal(1); os.Exit(1) }\n", nil},
		{"main package other func may not exit", "m.go", "package main\n\nimport \"os\"\n\nfunc helper() { os.Exit(1) }\n\nfunc main() {}\n", []string{"exit@5"}},
		{"main package init may not exit", "m.go", "package main\n\nimport \"os\"\n\nfunc init() { os.Exit(0) }\n\nfunc main() {}\n", []string{"exit@5"}},
		{"func main of another package is not exempt", "a.go", pre + "import \"os\"\n\nfunc main() { os.Exit(1) }\n", []string{"exit@5"}},
		{"method named main is not exempt", "m.go", "package main\n\nimport \"os\"\n\ntype T struct{}\n\nfunc (T) main() { os.Exit(1) }\n\nfunc main() {}\n", []string{"exit@7"}},
		{"main package flag.ExitOnError is fine", "m.go", "package main\n\nimport \"flag\"\n\nvar _ = flag.ExitOnError\n\nfunc main() {}\n", nil},
		{"test files are not scanned for exits", "a_test.go", pre + "import (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc init() { os.Exit(0) }\n\nfunc TestMain(m *testing.M) { os.Exit(m.Run()) }\n\nfunc TestX(t *testing.T) { t.Fatal(1) }\n", nil},
		{"logger method without importing log", "a.go", pre + "type L interface{ Fatalf(string, ...any) }\n\nfunc F(l L) { l.Fatalf(\"x\") }\n", []string{"exit@5"}},
		{"logger made by log.New and passed on", "a.go", pre + "import (\n\t\"log\"\n\t\"os\"\n)\n\nfunc use(l *log.Logger) { l.Panicln(1) }\n\nfunc F() { use(log.New(os.Stderr, \"\", 0)) }\n", []string{"exit@8"}},
		{"Fatal on a user type is flagged (documented false positive)", "a.go", pre + "type T struct{}\n\nfunc (T) Fatal(string) {}\n\nfunc F(t T) { t.Fatal(\"x\") }\n", []string{"exit@7"}},
		{"Fatal of another package is flagged", "a.go", pre + "import z \"example.org/zlog\"\n\nfunc F() { z.Fatal(1) }\n", []string{"exit@5"}},
		{"t.Fatal in a non-test file is flagged", "a.go", pre + "import \"testing\"\n\nfunc F(t *testing.T) { t.Fatal(1) }\n", []string{"testing@3", "exit@5"}},
		{"t.Fatal in a file importing log is fine in a test file", "a_test.go", pre + "import (\n\t\"log\"\n\t\"testing\"\n)\n\nfunc TestX(t *testing.T) { log.Print(1); t.Fatal(1) }\n", nil},
		{"shadowed recover is fine", "a.go", pre + "func F() { recover := func() {}; recover() }\n", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs, err := ScanGoSource(c.rel, []byte(c.src))
			if err != nil {
				t.Fatal(err)
			}
			if got := kinds(fs); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("findings = %v, want %v", got, c.want)
			}
		})
	}
}

func TestScanGoModBlockForms(t *testing.T) {
	src := "module x\n\ngo 1.22\n\nrequire (\n\ta.b/c v1.0.0\n\treplace v1 // a module named like a directive\n)\n\nreplace (\n\ta => b\n)\n"
	got := kinds(ScanGoMod([]byte(src)))
	want := []string{"replace@10", "replace@11"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findings = %v, want %v", got, want)
	}
}

func TestScanSourceParseErrorHasNoText(t *testing.T) {
	_, err := ScanGoSource("a.go", []byte("package a\n\nfunc F( { // CANARY-scan\n"))
	if err == nil {
		t.Fatal("a syntax error parsed")
	}
	if strings.Contains(err.Error(), "CANARY-scan") || strings.Contains(err.Error(), "expected") {
		t.Fatalf("the error carries parser text: %v", err)
	}
	if !strings.Contains(err.Error(), "a.go") || !strings.Contains(err.Error(), "line 3") {
		t.Fatalf("the error must name the file and line: %v", err)
	}
}

func TestScanRepoWalk(t *testing.T) {
	repo := t.TempDir()
	write(t, filepath.Join(repo, "go.mod"), "module x\n\nreplace a => b\n")
	write(t, filepath.Join(repo, "ok.go"), "package x\n")
	write(t, filepath.Join(repo, "sub", "bad.go"), "package sub\n\nimport \"C\"\n")
	write(t, filepath.Join(repo, "sub", "broken.go"), "package sub\n\nfunc F( {\n")
	write(t, filepath.Join(repo, ".git", "hook.go"), "package x\n\nimport \"C\"\n")
	write(t, filepath.Join(repo, ".gophermind", "run", "x.go"), "package x\n\nimport \"C\"\n")
	write(t, filepath.Join(repo, "testdata", "t.go"), "package x\n\nimport \"C\"\n")
	fs, err := ScanRepo(repo)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range fs {
		got = append(got, fmt.Sprintf("%s:%s@%d", f.File, f.Kind, f.Line))
	}
	want := []string{"go.mod:replace@3", "sub/bad.go:cgo@3", "sub/broken.go:parse@3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findings = %v, want %v", got, want)
	}
	// A symbolic link to a .go file is never followed, and is itself a finding.
	if err := os.Symlink(filepath.Join(repo, "ok.go"), filepath.Join(repo, "link.go")); err != nil {
		t.Fatal(err)
	}
	fs, _ = ScanRepo(repo)
	var sawLink bool
	for _, f := range fs {
		if f.File == "link.go" && f.Kind == "symlink" {
			sawLink = true
		}
	}
	if !sawLink {
		t.Fatal("a symbolic link to a go file was not reported")
	}
}

func TestScanFilesOnlyNamedFiles(t *testing.T) {
	repo := t.TempDir()
	write(t, filepath.Join(repo, "bad.go"), "package x\n\nimport \"C\"\n")
	write(t, filepath.Join(repo, "good.go"), "package x\n")
	fs, err := ScanFiles(repo, []string{"good.go"})
	if err != nil || len(fs) != 0 {
		t.Fatalf("ScanFiles(good) = %v, %v", fs, err)
	}
	fs, err = ScanFiles(repo, []string{"bad.go", "go.mod"})
	if err != nil || len(fs) != 1 || fs[0].Kind != "cgo" {
		t.Fatalf("ScanFiles(bad) = %v, %v", fs, err)
	}
}

func TestDescribeFindingsNamesNodesAndLinesOnly(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	l := g.plan.Leaves[0]
	msg := describeFindings(g.plan, []Finding{
		{Kind: "cgo", File: l.File, Line: 7},
		{Kind: "replace", File: "go.mod", Line: 3},
		{Kind: "exit", File: "weird CANARY-name.go", Line: 9},
	})
	if !strings.Contains(msg, l.ID) || !strings.Contains(msg, ":7") || !strings.Contains(msg, "go.mod:3") {
		t.Fatalf("message lacks node id or line: %q", msg)
	}
	if strings.Contains(msg, "CANARY") || strings.Contains(msg, l.File) {
		t.Fatalf("message carries a file name: %q", msg)
	}
}
