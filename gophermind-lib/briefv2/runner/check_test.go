package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/sandbox"
)

// rig is one tiny temp module plus a Runner. It is sandboxed when sandbox-exec works.
type rig struct {
	repo, scratch, cache string
	env                  []string
	r                    *Runner
	sandboxed            bool
}

func newRig(t *testing.T, files map[string]string) *rig {
	t.Helper()
	goBin := skipIfNoGo(t)
	g := &rig{repo: realDir(t), scratch: realDir(t), cache: realDir(t)}
	files["go.mod"] = "module example.com/t\n\ngo 1.22\n"
	for n, c := range files {
		p := filepath.Join(g.repo, filepath.FromSlash(n))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	g.env = []string{
		"PATH=" + filepath.Dir(goBin) + ":/usr/bin:/bin",
		"HOME=" + g.scratch, "TMPDIR=" + g.scratch, "GOCACHE=" + g.cache,
		"GOFLAGS=-mod=readonly -buildvcs=false", "GOPROXY=off", "GOTOOLCHAIN=local", "CGO_ENABLED=0",
	}
	cfg := Config{Grace: time.Second}
	if runtime.GOOS == "darwin" && sandbox.Preflight(context.Background()) == nil {
		root, err := exec.Command(goBin, "env", "GOROOT").Output()
		if err != nil {
			t.Skip("go env GOROOT failed")
		}
		p := sandbox.Profile{Repo: g.repo, Scratch: g.scratch, GoCache: g.cache, GoModCache: realDir(t),
			ReadOnly: []string{strings.TrimSpace(string(root)), filepath.Dir(goBin)}}
		if h, err := os.UserHomeDir(); err == nil {
			p.Home = h
		}
		cfg.Sandbox = &p
		g.sandboxed = true
	} else {
		t.Log("sandbox-exec unavailable: running the temp module unsandboxed")
	}
	g.r = New(cfg)
	return g
}

const okSrc = "package t\n\nfunc Add(a, b int) int { return a + b }\n"

func okTest(body string) string {
	return "package t\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n" + body + "\n}\n"
}

func (g *rig) leaf(fn string, tmo time.Duration, files ...string) LeafCheck {
	return LeafCheck{Repo: g.repo, Dir: ".", TestFunc: fn, Files: files, Env: g.env, TestTimeout: tmo}
}

func TestCheckLeafPass(t *testing.T) {
	g := newRig(t, map[string]string{"a.go": okSrc, "a_test.go": okTest("if Add(1, 2) != 3 { t.Fatal(\"bad\") }")})
	v := g.r.CheckLeaf(context.Background(), g.leaf("TestX", 30*time.Second, "a.go"))
	if !v.Pass() || v.Events < 1 {
		t.Fatalf("verdict %s events=%d", v.Reason(), v.Events)
	}
}

func TestClassifyFailures(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		fn    string
		tmo   time.Duration
		class string
		names []string
	}{
		{"unformatted", map[string]string{"a.go": "package t\nfunc Add(a,b int) int {return a+b}\n", "a_test.go": okTest("")}, "TestX", 30 * time.Second, ClassMalformed, nil},
		{"type error", map[string]string{"a.go": "package t\n\nfunc Add(a, b int) int { return a + \"s\" }\n", "a_test.go": okTest("")}, "TestX", 30 * time.Second, ClassBuild, nil},
		{"vet", map[string]string{"a.go": "package t\n\nimport \"fmt\"\n\nfunc Add(a, b int) string { return fmt.Sprintf(\"%d\", \"s\") }\n", "a_test.go": okTest("")}, "TestX", 30 * time.Second, ClassVet, nil},
		{"wrong result", map[string]string{"a.go": okSrc, "a_test.go": okTest("if Add(1, 2) != 4 { t.Fatal(\"bad\") }")}, "TestX", 30 * time.Second, ClassTestFail, []string{"TestX"}},
		{"panic", map[string]string{"a.go": okSrc, "a_test.go": okTest("var s []int\n\tidx := 3\n\t_ = s[idx]")}, "TestX", 30 * time.Second, ClassTestPanic, nil},
		{"hang", map[string]string{"a.go": okSrc, "a_test.go": okTest("for {\n\t}")}, "TestX", time.Second, ClassTestTimeout, nil},
		{"test file does not compile", map[string]string{"a.go": okSrc, "a_test.go": okTest("undefinedThing()")}, "TestX", 30 * time.Second, ClassBuild, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newRig(t, c.files)
			v := g.r.CheckLeaf(context.Background(), g.leaf(c.fn, c.tmo, "a.go"))
			if v.Class != c.class {
				t.Fatalf("class = %q (%s), want %q\n%s", v.Class, v.Reason(), c.class, v.Out.Text())
			}
			if c.names != nil && !reflect.DeepEqual(v.Names, c.names) {
				t.Fatalf("names = %v, want %v", v.Names, c.names)
			}
			if v.Pass() {
				t.Fatal("a failure must not Pass")
			}
		})
	}
}

func TestClassifyDataRaceUnderTest(t *testing.T) {
	g := newRig(t, map[string]string{"a.go": okSrc, "a_test.go": raceTest})
	v := g.r.Test(context.Background(), TestSet{Repo: g.repo, Pkg: "./...", Race: true, Env: g.env, Timeout: 2 * time.Minute})
	if v.Class != ClassTestFail {
		t.Fatalf("class = %q\n%s", v.Class, v.Out.Text())
	}
}

const raceTest = `package t

import (
	"sync"
	"testing"
)

func TestX(t *testing.T) {
	x := 0
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); for j := 0; j < 1000; j++ { x++ } }()
	}
	wg.Wait()
	_ = x
}
`

func TestNoTestsRanIsFailure(t *testing.T) {
	g := newRig(t, map[string]string{"a.go": okSrc, "a_test.go": okTest("")})
	v := g.r.CheckLeaf(context.Background(), g.leaf("TestMissing", 30*time.Second, "a.go"))
	if v.Class != ClassNoTestsRan {
		t.Fatalf("class = %q", v.Class)
	}
	g = newRig(t, map[string]string{"a.go": okSrc})
	v = g.r.CheckLeaf(context.Background(), g.leaf("TestX", 30*time.Second, "a.go"))
	if v.Class != ClassNoTestsRan {
		t.Fatalf("no test file: class = %q", v.Class)
	}
}

func TestCheckLeafSubtestNames(t *testing.T) {
	body := "t.Run(\"CANARY-sec=ret\", func(t *testing.T) { t.Fatal(\"no\") })"
	g := newRig(t, map[string]string{"a.go": okSrc, "a_test.go": okTest(body)})
	v := g.r.CheckLeaf(context.Background(), g.leaf("TestX", 30*time.Second, "a.go"))
	if v.Class != ClassTestFail || !strings.Contains(v.Reason(), "(unnamed)") || strings.Contains(v.Reason(), "CANARY") {
		t.Fatalf("reason = %q", v.Reason())
	}
}

func TestFailureReasonNeverHasOutput(t *testing.T) {
	g := newRig(t, map[string]string{"a.go": okSrc, "a_test.go": okTest("t.Fatal(\"CANARY-out\")")})
	v := g.r.CheckLeaf(context.Background(), g.leaf("TestX", 30*time.Second, "a.go"))
	if v.Class != ClassTestFail {
		t.Fatalf("class = %q", v.Class)
	}
	if strings.Contains(v.Reason(), "CANARY-out") || strings.Contains(fmt.Sprint(v), "CANARY-out") || strings.Contains(fmt.Sprintf("%+v %#v", v, v), "CANARY-out") {
		t.Fatal("output reached the reason or the verdict formatting")
	}
	if !strings.Contains(v.Out.Text(), "CANARY-out") {
		t.Fatal("Out.Text() must carry the test output")
	}

	g = newRig(t, map[string]string{"a.go": "package t\n\nvar x int = \"CANARY-out\"\n", "a_test.go": okTest("")})
	v = g.r.CheckLeaf(context.Background(), g.leaf("TestX", 30*time.Second, "a.go"))
	if v.Class != ClassBuild || strings.Contains(v.Reason(), "CANARY") || strings.Contains(fmt.Sprint(v), "CANARY") || !strings.Contains(v.Out.Text(), "CANARY-out") {
		t.Fatalf("compiler case: class=%q reason=%q", v.Class, v.Reason())
	}
}

func TestRunRegexValidated(t *testing.T) {
	got, err := RunRegex([]string{"TestA", "TestB"})
	if err != nil || got != "^(TestA|TestB)$" {
		t.Fatalf("got %q err=%v", got, err)
	}
	for _, bad := range [][]string{{"Test A"}, {"A"}, {}, {"Test.*"}, {"TestA", ""}} {
		if _, err := RunRegex(bad); err == nil {
			t.Errorf("RunRegex(%q) accepted", bad)
		}
	}
}

func TestTestPerPackageRegexHidesNothing(t *testing.T) {
	g := newRig(t, map[string]string{
		"a/a.go":      "package a\n\nfunc F() int { return 1 }\n",
		"a/a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestF(t *testing.T) {\n\tif F() != 1 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n\nfunc TestG(t *testing.T) {\n\tG()\n}\n",
		"a/g.go":      "package a\n\nfunc G() { panic(\"gm: not implemented\") }\n",
		"b/b.go":      "package b\n\nfunc H() int { return 2 }\n",
		"b/b_test.go": "package b\n\nimport \"testing\"\n\nfunc TestH(t *testing.T) {\n\tif H() != 2 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n",
	})
	ctx := context.Background()
	v := g.r.Test(ctx, TestSet{Repo: g.repo, Pkg: "./a", Funcs: []string{"TestF"}, Env: g.env, Timeout: time.Minute})
	if !v.Pass() {
		t.Fatalf("restricted run: %s\n%s", v.Reason(), v.Out.Text())
	}
	v = g.r.Test(ctx, TestSet{Repo: g.repo, Pkg: "./...", Env: g.env, Timeout: time.Minute})
	if v.Class != ClassTestPanic && v.Class != ClassTestFail {
		t.Fatalf("unrestricted run class = %q", v.Class)
	}
}

func TestBuildVetClasses(t *testing.T) {
	g := newRig(t, map[string]string{"a.go": "package t\n\nfunc Add(a, b int) int { return a + \"s\" }\n"})
	v := g.r.BuildVet(context.Background(), g.repo, g.env)
	if v.Class != ClassBuild || len(v.Locations) == 0 || v.Locations[0].File != "a.go" || v.Locations[0].Line != 3 {
		t.Fatalf("class=%q locations=%v", v.Class, v.Locations)
	}
	g = newRig(t, map[string]string{"a.go": "package t\n\nimport \"fmt\"\n\nfunc Add() string { return fmt.Sprintf(\"%d\", \"s\") }\n"})
	v = g.r.BuildVet(context.Background(), g.repo, g.env)
	if v.Class != ClassVet || len(v.Locations) == 0 || v.Locations[0].File != "a.go" {
		t.Fatalf("class=%q locations=%v\n%s", v.Class, v.Locations, v.Out.Text())
	}
	g = newRig(t, map[string]string{"a.go": okSrc})
	if v = g.r.BuildVet(context.Background(), g.repo, g.env); !v.Pass() {
		t.Fatalf("clean module: %s", v.Reason())
	}
}

// I10: -race must work under CGO_ENABLED=0. A hard failure on any machine with go, not a skip.
// If it fails on a machine, decision E13 is wrong there.
func TestRaceDetectorNeedsNoCgo(t *testing.T) {
	g := newRig(t, map[string]string{"a.go": okSrc, "a_test.go": raceTest})
	v := g.r.Test(context.Background(), TestSet{Repo: g.repo, Pkg: "./...", Race: true, Env: g.env, Timeout: 2 * time.Minute})
	if strings.Contains(v.Out.Text(), "requires cgo") {
		t.Fatalf("-race needs cgo here; E13 is wrong on this machine:\n%s", v.Out.Text())
	}
	if v.Class != ClassTestFail {
		t.Fatalf("class = %q\n%s", v.Class, v.Out.Text())
	}
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

// go build of one main package writes its binary into the working directory
// unless told otherwise; a check must leave no file behind.
func TestBuildLeavesNoOutputFile(t *testing.T) {
	g := newRig(t, map[string]string{
		"cmd/app/main.go":      "package main\n\nfunc main() {}\n",
		"cmd/app/main_test.go": "package main\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n",
	})
	before := dirNames(t, g.repo)
	lc := g.leaf("TestX", 60*time.Second, "cmd/app/main.go")
	lc.Dir = "cmd/app"
	if v := g.r.CheckLeaf(context.Background(), lc); !v.Pass() {
		t.Fatalf("CheckLeaf: %s", v.Reason())
	}
	if v := g.r.BuildVet(context.Background(), g.repo, g.env); !v.Pass() {
		t.Fatalf("BuildVet: %s", v.Reason())
	}
	if after := dirNames(t, g.repo); !reflect.DeepEqual(before, after) {
		t.Fatalf("repo root changed by the checks: before %v, after %v", before, after)
	}
	if _, err := os.Stat(filepath.Join(g.repo, "cmd", "app", "app")); err == nil {
		t.Fatal("a binary was written into the package directory")
	}
}

// Two temp repos that share one GOCACHE, with the same package path and file
// names but different code, never see each other's compile or test results;
// and go test never serves a cached result (-count=1): a test that appends to
// a file runs once per check.
func TestSharedGoCacheIsContentAddressedAndResultsAreNotCached(t *testing.T) {
	counter := filepath.Join(realDir(t), "runs")
	mk := func(body, cache string) *rig {
		g := newRig(t, map[string]string{
			"a.go": "package t\n\nfunc Val() int { return " + body + " }\n",
			"a_test.go": "package t\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestVal(t *testing.T) {\n" +
				"\tf, err := os.OpenFile(os.Getenv(\"GM_COUNT_FILE\"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)\n" +
				"\tif err == nil {\n\t\tf.WriteString(\"x\\n\")\n\t\tf.Close()\n\t}\n" +
				"\tif Val() != 1 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n",
		})
		g.r = New(Config{Grace: time.Second}) // unsandboxed: the cache is shared across two sandbox profiles otherwise
		for i, kv := range g.env {
			if strings.HasPrefix(kv, "GOCACHE=") {
				g.env[i] = "GOCACHE=" + cache
			}
		}
		g.env = append(g.env, "GM_COUNT_FILE="+counter)
		return g
	}
	cache := realDir(t)
	okRepo, badRepo := mk("1", cache), mk("2", cache)
	check := func(g *rig) Verdict {
		return g.r.CheckLeaf(context.Background(), g.leaf("TestVal", 0, "a.go"))
	}
	if v := check(okRepo); !v.Pass() {
		t.Fatalf("first repo: %q", v.Reason())
	}
	if v := check(badRepo); v.Class != ClassTestFail {
		t.Fatalf("second repo with different code: %q, want test_fail (it must not see the first repo's pass)", v.Reason())
	}
	if v := check(okRepo); !v.Pass() {
		t.Fatalf("first repo again: %q (it must not see the second repo's failure)", v.Reason())
	}
	raw, err := os.ReadFile(counter)
	if err != nil || strings.Count(string(raw), "x") != 3 {
		t.Fatalf("the test ran %d times for 3 checks (err %v): go test served a cached result", strings.Count(string(raw), "x"), err)
	}
}
