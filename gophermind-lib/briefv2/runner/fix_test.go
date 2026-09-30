package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/sandbox"
)

func fakeToolDir(t *testing.T, scripts map[string]string) string {
	t.Helper()
	dir := realDir(t)
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func fakeEnv(dir string) []string { return []string{"PATH=" + dir + ":/bin:/usr/bin"} }

func notModelFailure(t *testing.T, name string, v Verdict, class string) {
	t.Helper()
	switch v.Class {
	case ClassBuild, ClassVet, ClassTestFail, ClassMalformed, ClassTestPanic, ClassTestTimeout, ClassNoTestsRan, "":
		t.Errorf("%s: class %q must not be a model failure or a pass", name, v.Class)
	}
	if v.Class != class || v.Pass() || v.Err == nil {
		t.Errorf("%s: class=%q pass=%v err=%v, want class %q with an error", name, v.Class, v.Pass(), v.Err, class)
	}
	if v.Reason() != class {
		t.Errorf("%s: reason = %q", name, v.Reason())
	}
}

func TestHarnessFaultsAreNeverModelFailures(t *testing.T) {
	ctx := context.Background()
	repo := realDir(t)
	env := []string{"PATH=" + realDir(t)} // no go, no gofmt
	r := New(Config{})
	notModelFailure(t, "leaf with gofmt", r.CheckLeaf(ctx, LeafCheck{Repo: repo, Dir: ".", TestFunc: "TestX", Files: []string{"a.go"}, Env: env}), ClassHarness)
	notModelFailure(t, "leaf", r.CheckLeaf(ctx, LeafCheck{Repo: repo, Dir: ".", TestFunc: "TestX", Env: env}), ClassHarness)
	notModelFailure(t, "buildvet", r.BuildVet(ctx, repo, env), ClassHarness)
	notModelFailure(t, "test", r.Test(ctx, TestSet{Repo: repo, Pkg: "./...", Env: env}), ClassHarness)
}

func TestHarnessSandboxRefusal(t *testing.T) {
	dir := fakeToolDir(t, map[string]string{"go": "exit 0", "gofmt": "exit 0"})
	r := New(Config{Sandbox: &sandbox.Profile{}}) // no Repo: the wrap is refused
	repo := realDir(t)
	notModelFailure(t, "gofmt", r.CheckLeaf(context.Background(), LeafCheck{Repo: repo, Dir: ".", TestFunc: "TestX", Files: []string{"a.go"}, Env: fakeEnv(dir)}), ClassHarness)
	notModelFailure(t, "build", r.BuildVet(context.Background(), repo, fakeEnv(dir)), ClassHarness)
	notModelFailure(t, "test", r.Test(context.Background(), TestSet{Repo: repo, Pkg: "./...", Env: fakeEnv(dir)}), ClassHarness)
}

func TestCancellationIsNeverAFailureOrAPass(t *testing.T) {
	dir := fakeToolDir(t, map[string]string{"go": "sleep 60", "gofmt": "sleep 60"})
	env := fakeEnv(dir)
	repo := realDir(t)
	r := New(Config{Grace: 300 * time.Millisecond})
	runs := map[string]func(ctx context.Context) Verdict{
		"gofmt": func(ctx context.Context) Verdict {
			return r.CheckLeaf(ctx, LeafCheck{Repo: repo, Dir: ".", TestFunc: "TestX", Files: []string{"a.go"}, Env: env})
		},
		"build": func(ctx context.Context) Verdict {
			return r.CheckLeaf(ctx, LeafCheck{Repo: repo, Dir: ".", TestFunc: "TestX", Env: env})
		},
		"buildvet": func(ctx context.Context) Verdict { return r.BuildVet(ctx, repo, env) },
		"test": func(ctx context.Context) Verdict {
			return r.Test(ctx, TestSet{Repo: repo, Pkg: "./...", Env: env, Timeout: time.Minute})
		},
	}
	for name, run := range runs {
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(300*time.Millisecond, cancel)
		start := time.Now()
		v := run(ctx)
		cancel()
		if time.Since(start) > 20*time.Second {
			t.Errorf("%s: not bounded by the context", name)
		}
		notModelFailure(t, name, v, ClassCancelled)
	}
}

func TestEveryStageHasADefaultTimeout(t *testing.T) {
	repo := realDir(t)
	cfg := Config{StageTimeout: 400 * time.Millisecond, Grace: 200 * time.Millisecond}
	cases := []struct {
		name   string
		tools  map[string]string
		run    func(r *Runner, env []string) Verdict
		expect string
	}{
		{"gofmt", map[string]string{"gofmt": "sleep 60", "go": "exit 0"},
			func(r *Runner, env []string) Verdict {
				return r.CheckLeaf(context.Background(), LeafCheck{Repo: repo, Dir: ".", TestFunc: "TestX", Files: []string{"a.go"}, Env: env})
			}, ClassTimeout},
		{"build", map[string]string{"go": "sleep 60"},
			func(r *Runner, env []string) Verdict { return r.BuildVet(context.Background(), repo, env) }, ClassTimeout},
		{"vet", map[string]string{"go": `case "$1" in build) exit 0;; *) sleep 60;; esac`},
			func(r *Runner, env []string) Verdict { return r.BuildVet(context.Background(), repo, env) }, ClassTimeout},
		{"test with no timeout of its own", map[string]string{"go": "sleep 60"},
			func(r *Runner, env []string) Verdict {
				return r.Test(context.Background(), TestSet{Repo: repo, Pkg: "./...", Env: env})
			}, ClassTestTimeout},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := fakeToolDir(t, c.tools)
			start := time.Now()
			v := c.run(New(cfg), fakeEnv(dir))
			if v.Class != c.expect || v.Pass() {
				t.Fatalf("class = %q, want %q", v.Class, c.expect)
			}
			if time.Since(start) > 15*time.Second {
				t.Fatal("stage was not bounded by the default timeout")
			}
		})
	}
}

func TestDefaultStageTimeoutIsSet(t *testing.T) {
	if New(Config{}).cfg.StageTimeout <= 0 {
		t.Fatal("StageTimeout has no default")
	}
}

func TestHostileArgumentsAreRefusedAndTerminated(t *testing.T) {
	logf := filepath.Join(realDir(t), "argv.log")
	body := `echo "$@" >> ` + logf + `; exit 0`
	dir := fakeToolDir(t, map[string]string{"go": body, "gofmt": body})
	env := fakeEnv(dir)
	repo := realDir(t)
	r := New(Config{})
	ctx := context.Background()
	for _, pkg := range []string{"-toolexec=/bin/sh", "-w", "", "../x", "./a/../../b", "./a b", "/abs/pkg", "./a;id", "$(id)"} {
		v := r.Test(ctx, TestSet{Repo: repo, Pkg: pkg, Env: env})
		if v.Class != ClassHarness {
			t.Errorf("Test Pkg %q: class %q", pkg, v.Class)
		}
	}
	for _, f := range []string{"-w", "", "/abs.go", "../x.go", "a/../../x.go", "a\nb.go"} {
		v := r.CheckLeaf(ctx, LeafCheck{Repo: repo, Dir: ".", TestFunc: "TestX", Files: []string{"ok.go", f}, Env: env})
		if v.Class != ClassHarness {
			t.Errorf("gofmt file %q: class %q", f, v.Class)
		}
	}
	for _, d := range []string{"-x", "../up", "/abs", "a b"} {
		v := r.CheckLeaf(ctx, LeafCheck{Repo: repo, Dir: d, TestFunc: "TestX", Env: env})
		if v.Class != ClassHarness {
			t.Errorf("Dir %q: class %q", d, v.Class)
		}
	}
	if _, err := os.Stat(logf); err == nil {
		b, _ := os.ReadFile(logf)
		t.Fatalf("a tool ran for a refused value: %q", b)
	}
	// valid values run, with "--" ahead of files and packages where the tool accepts it
	for _, pkg := range []string{"./...", "./a/b", "example.com/t/x", ".", "..."} {
		if v := r.Test(ctx, TestSet{Repo: repo, Pkg: pkg, Env: env}); v.Class == ClassHarness {
			t.Errorf("valid Pkg %q refused: %v", pkg, v.Err)
		}
	}
	_ = os.Remove(logf)
	r.CheckLeaf(ctx, LeafCheck{Repo: repo, Dir: "internal/x", TestFunc: "TestX", Files: []string{"internal/x/a.go"}, Env: env})
	b, _ := os.ReadFile(logf)
	log := string(b)
	for _, want := range []string{"-l -- internal/x/a.go", "build -- ./internal/x", "vet -- ./internal/x"} {
		if !strings.Contains(log, want) {
			t.Errorf("argv log missing %q:\n%s", want, log)
		}
	}
}

func evP(action, pkg, test string) string {
	s := `{"Action":"` + action + `","Package":"` + pkg + `"`
	if test != "" {
		s += `,"Test":"` + test + `"`
	}
	return s + "}\n"
}

func evOutP(pkg, test, out string) string {
	s := `{"Action":"output","Package":"` + pkg + `"`
	if test != "" {
		s += `,"Test":"` + test + `"`
	}
	return s + `,"Output":"` + out + `"}` + "\n"
}

func chattyStream(pkgs int, lastOK bool) string {
	var b strings.Builder
	chat := strings.Repeat("x", 1000) + "\\n"
	for i := 0; i < pkgs; i++ {
		p := fmt.Sprintf("m/p%d", i)
		b.WriteString(evP("run", p, "TestA"))
		for j := 0; j < 20; j++ {
			b.WriteString(evOutP(p, "TestA", chat))
		}
		if i == pkgs-1 && !lastOK {
			b.WriteString(evOutP(p, "", "panic: test timed out after 1s\\n"))
			b.WriteString(evP("fail", p, ""))
			break
		}
		b.WriteString(evP("pass", p, "TestA"))
		b.WriteString(evP("pass", p, ""))
	}
	return b.String()
}

func fakeGoPrinting(t *testing.T, stream string, exit int) []string {
	t.Helper()
	f := filepath.Join(realDir(t), "stream.json")
	if err := os.WriteFile(f, []byte(stream), 0o644); err != nil {
		t.Fatal(err)
	}
	return fakeEnv(fakeToolDir(t, map[string]string{"go": fmt.Sprintf("cat %s; exit %d", f, exit)}))
}

func TestChattyStreamKeepsEveryEventPastTheOutputCap(t *testing.T) {
	env := fakeGoPrinting(t, chattyStream(200, true), 0)
	v := New(Config{OutputCap: 4096}).Test(context.Background(), TestSet{Repo: realDir(t), Pkg: "./...", Env: env})
	if !v.Pass() || v.Events != 400 {
		t.Fatalf("class=%q events=%d, want a pass with 400 events", v.Class, v.Events)
	}
	if !v.Out.Truncated() || len(v.Out.Text()) > 4096 {
		t.Fatalf("Out must stay capped: truncated=%v len=%d", v.Out.Truncated(), len(v.Out.Text()))
	}
}

func TestChattyStreamStillReportsATimeout(t *testing.T) {
	env := fakeGoPrinting(t, chattyStream(200, false), 1)
	v := New(Config{OutputCap: 4096}).Test(context.Background(), TestSet{Repo: realDir(t), Pkg: "./...", Env: env})
	if v.Class != ClassTestTimeout {
		t.Fatalf("class = %q", v.Class)
	}
	if len(v.QNames) != 1 || v.QNames[0] != "m/p199.TestA" {
		t.Fatalf("qualified names = %v", v.QNames)
	}
}

func TestChattyRealGoKeepsEveryPass(t *testing.T) {
	var src strings.Builder
	src.WriteString("package t\n\nimport (\n\t\"fmt\"\n\t\"strings\"\n\t\"testing\"\n)\n\n")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&src, "func TestN%d(t *testing.T) { fmt.Println(strings.Repeat(\"y\", 2000)) }\n", i)
	}
	g := newRig(t, map[string]string{"a.go": okSrc, "a_test.go": src.String()})
	g.r = New(Config{Sandbox: g.r.cfg.Sandbox, Grace: time.Second, OutputCap: 8192})
	v := g.r.Test(context.Background(), TestSet{Repo: g.repo, Pkg: "./...", Env: g.env, Timeout: 2 * time.Minute, Expect: []string{"TestN0", "TestN199"}})
	if !v.Pass() || v.Events != 400 {
		t.Fatalf("class=%q events=%d\n%.300s", v.Class, v.Events, v.Out.Text())
	}
}

const hostileHeader = "package t\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\t\"strings\"\n\t\"testing\"\n)\n\nvar _ = fmt.Sprint\nvar _ = os.Exit\nvar _ = strings.Repeat\n\n"

func hostile(t *testing.T, testSrc string) Verdict {
	t.Helper()
	g := newRig(t, map[string]string{"a.go": okSrc, "a_test.go": hostileHeader + testSrc})
	return g.r.CheckLeaf(context.Background(), g.leaf("TestX", time.Minute, "a.go"))
}

func TestHostileOutputFromAPassingTestDoesNotForceAFailure(t *testing.T) {
	cases := map[string]string{
		"fake json lines": `func TestX(t *testing.T) {
	fmt.Println(` + "`" + `{"Action":"fail","Package":"example.com/t","Test":"TestX"}` + "`" + `)
	fmt.Println(` + "`" + `{"Action":"fail","Package":"example.com/t","FailedBuild":"example.com/t"}` + "`" + `)
	fmt.Println("[build failed]")
	fmt.Println("panic: not a real panic")
	fmt.Println("WARNING: DATA RACE")
}`,
		"ansi":               `func TestX(t *testing.T) { fmt.Print("\x1b[31mFAIL\x1b[0m\r\x1b[2K\n--- FAIL: TestX\n") }`,
		"five megabyte line": `func TestX(t *testing.T) { fmt.Println(strings.Repeat("z", 5<<20)) }`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if v := hostile(t, src); !v.Pass() {
				t.Fatalf("class = %q (%s)", v.Class, v.Reason())
			}
		})
	}
}

func TestHostileFakePassLineForATestThatFails(t *testing.T) {
	v := hostile(t, `func TestX(t *testing.T) {
	fmt.Println("--- PASS: TestX (0.00s)")
	t.Fatal("really failing")
}`)
	if v.Class != ClassTestFail {
		t.Fatalf("class = %q", v.Class)
	}
}

func TestInterleavedPackagesAreQualified(t *testing.T) {
	src := func(name, body string) string {
		return "package " + name + "\n\nimport \"testing\"\n\nfunc Test" + strings.ToUpper(name) + "(t *testing.T) {\n\tt.Parallel()\n" + body + "\n}\n"
	}
	g := newRig(t, map[string]string{
		"a/a.go": "package a\n", "a/a_test.go": src("a", ""),
		"b/b.go": "package b\n", "b/b_test.go": src("b", "t.Fatal(\"no\")"),
		"c/c.go": "package c\n", "c/c_test.go": src("c", ""),
	})
	v := g.r.Test(context.Background(), TestSet{Repo: g.repo, Pkg: "./...", Env: g.env, Timeout: time.Minute})
	if v.Class != ClassTestFail || len(v.QNames) != 1 || v.QNames[0] != "example.com/t/b.TestB" || v.Names[0] != "TestB" {
		t.Fatalf("class=%q names=%v qnames=%v", v.Class, v.Names, v.QNames)
	}
	g = newRig(t, map[string]string{
		"a/a.go": "package a\n", "a/a_test.go": src("a", ""),
		"c/c.go": "package c\n", "c/c_test.go": src("c", ""),
	})
	if v := g.r.Test(context.Background(), TestSet{Repo: g.repo, Pkg: "./...", Env: g.env, Timeout: time.Minute, Expect: []string{"TestA", "TestC"}}); !v.Pass() {
		t.Fatalf("interleaved passing packages: %s", v.Reason())
	}
}

func TestExpectListMustAllPass(t *testing.T) {
	g := newRig(t, map[string]string{"a.go": okSrc, "a_test.go": okTest("")})
	v := g.r.Test(context.Background(), TestSet{Repo: g.repo, Pkg: "./...", Env: g.env, Timeout: time.Minute, Expect: []string{"TestX", "TestGone"}})
	if v.Class != ClassTestFail || len(v.Names) != 1 || v.Names[0] != "TestGone" {
		t.Fatalf("class=%q names=%v", v.Class, v.Names)
	}
}

func TestSubtestNamedLikeTheLeafDoesNotSatisfyIt(t *testing.T) {
	in := ev("run", "TestOther") + ev("run", "TestOther/x.TestGreet") + ev("pass", "TestOther/x.TestGreet") + ev("pass", "TestOther") + ev("pass", "")
	env := fakeGoPrinting(t, in, 0)
	v := New(Config{}).Test(context.Background(), TestSet{Repo: realDir(t), Pkg: "./...", Env: env, Expect: []string{"TestGreet"}})
	if v.Class != ClassTestFail || len(v.Names) != 1 || v.Names[0] != "TestGreet" {
		t.Fatalf("class=%q names=%v", v.Class, v.Names)
	}
}

func TestPassNeedsAPackageLevelPass(t *testing.T) {
	// exit 0, a passing test, but no package-level result: not a pass
	env := fakeGoPrinting(t, ev("run", "TestX")+ev("pass", "TestX"), 0)
	v := New(Config{}).Test(context.Background(), TestSet{Repo: realDir(t), Pkg: "./...", Env: env})
	if v.Pass() {
		t.Fatal("a stream with no package result must not pass")
	}
	// a nonzero exit is never a pass even when every event says pass
	env = fakeGoPrinting(t, ev("run", "TestX")+ev("pass", "TestX")+ev("pass", ""), 1)
	if v := New(Config{}).Test(context.Background(), TestSet{Repo: realDir(t), Pkg: "./...", Env: env}); v.Pass() {
		t.Fatal("exit 1 must not pass")
	}
}

// Residual risk, documented in the package comment: code that runs before the testing
// framework (an init function) can print the exact lines go test converts into events and
// exit 0. This test records what actually happens; the acceptance run against the built
// binary is the backstop.
func TestInitForgeryDocumentsActualBehaviour(t *testing.T) {
	v := hostile(t, `func init() {
	fmt.Print("=== RUN   TestX\n--- PASS: TestX (0.00s)\nPASS\n")
	os.Exit(0)
}

func TestX(t *testing.T) { t.Fatal("never reached") }`)
	// Observed on go 1.27.1: the forgery passes. If this ever fails, the toolchain now catches it
	// and the residual-risk paragraph in the package comment should be updated.
	if !v.Pass() {
		t.Fatalf("init forgery no longer passes (class %q): update the package comment", v.Class)
	}
}
