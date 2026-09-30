package executor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/runner"
	"gophermind/gophermind-lib/briefv2/sandbox"
	"gophermind/gophermind-lib/briefv2/settings"
)

func twoDepProxy(t *testing.T) string {
	t.Helper()
	url := makeModuleProxy(t, "example.org/dep1", "v1.0.0", map[string]string{"d.go": "package dep1\n\nconst X = 1\n"})
	addModule(t, url, "example.org/dep2", "v1.2.3", map[string]string{"d.go": "package dep2\n\nconst Y = 2\n"})
	addModule(t, url, "example.org/dep3", "v1.0.0", map[string]string{"d.go": "package dep3\n\nconst Z = 3\n"})
	useModuleProxy(t, url)
	return url
}

var twoDeps = []planner.Dependency{
	{Module: "example.org/dep1", Version: "v1.0.0", Purpose: "p"},
	{Module: "example.org/dep2", Version: "v1.2.3", Purpose: "p"},
}

func TestDepsStepFetchesOnlyListed(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc := g.newRC(t)
	twoDepProxy(t)
	rc.plan.Deps = twoDeps
	if err := rc.runDeps(context.Background()); err != nil {
		t.Fatalf("runDeps: %v", err)
	}
	mod := g.read("go.mod")
	if !strings.Contains(mod, "example.org/dep1 v1.0.0") || !strings.Contains(mod, "example.org/dep2 v1.2.3") {
		t.Fatalf("go.mod does not require both listed modules:\n%s", mod)
	}
	if strings.Contains(mod, "dep3") || strings.Count(mod, "example.org/dep") != 2 {
		t.Fatalf("go.mod requires something that was not listed:\n%s", mod)
	}
	for _, p := range []string{"example.org/dep3@v1.0.0", "cache/download/example.org/dep3"} {
		if fileExists(filepath.Join(rc.modCache, filepath.FromSlash(p))) {
			t.Fatalf("the module cache holds an unlisted module (%s)", p)
		}
	}
	for _, p := range []string{"example.org/dep1@v1.0.0", "example.org/dep2@v1.2.3"} {
		if !fileExists(filepath.Join(rc.modCache, filepath.FromSlash(p))) {
			t.Fatalf("the module cache lacks %s", p)
		}
	}
	sum := g.read("go.sum")
	if !strings.Contains(sum, "example.org/dep1 v1.0.0 h1:") || !strings.Contains(sum, "example.org/dep2 v1.2.3 h1:") {
		t.Fatalf("go.sum lacks entries:\n%s", sum)
	}
	// file:// bypasses the harness proxy, so its log has no request.
	if fi, err := os.Stat(filepath.Join(g.runDir, "proxy.log")); err == nil && fi.Size() != 0 {
		t.Fatalf("the proxy saw a request (%d bytes of log)", fi.Size())
	}
}

func TestDepsStepBadVersionStopsRun(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc := g.newRC(t)
	twoDepProxy(t)
	rc.plan.Deps = []planner.Dependency{{Module: "example.org/dep1", Version: "v9.9.9", Purpose: "p"}}
	err := rc.runDeps(context.Background())
	var se *stopError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want *stopError", err)
	}
	if se.Status != "failed" || se.Reason != "deps_failed" {
		t.Fatalf("stop = %s/%s, want failed/deps_failed", se.Status, se.Reason)
	}
	if !strings.Contains(se.Message, "example.org/dep1@v9.9.9") || !strings.Contains(se.Message, "exit ") {
		t.Fatalf("message = %q, want module@version and an exit code", se.Message)
	}
	for _, leak := range []string{"file://", "reading", "no such file", filepath.Dir(rc.modCache)} {
		if strings.Contains(se.Message, leak) {
			t.Fatalf("message carries command output (%q): %q", leak, se.Message)
		}
	}
	if n := len(g.fake.Requests()); n != 0 {
		t.Fatalf("the deps step made %d model calls", n)
	}
}

func TestDepsStepUnreachableIsClassified(t *testing.T) {
	g := newRig(t)
	rc := g.newRC(t)
	useModuleProxy(t, "http://127.0.0.1:1") // loopback, nothing listens: connection refused
	rc.plan.Deps = twoDeps[:1]
	err := rc.runDeps(context.Background())
	var se *stopError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want *stopError", err)
	}
	if se.Status != "failed" || se.Reason != "deps_unreachable" {
		t.Fatalf("stop = %s/%s, want failed/deps_unreachable", se.Status, se.Reason)
	}
	if !strings.Contains(se.Message, "not reachable") {
		t.Fatalf("message = %q, want a clear statement that the proxy is not reachable", se.Message)
	}
	if strings.Contains(se.Message, "127.0.0.1") {
		t.Fatalf("message carries command output: %q", se.Message)
	}
}

func TestVetDeps(t *testing.T) {
	ok := planner.Dependency{Module: "example.org/dep1", Version: "v1.0.0", Purpose: "p"}
	cases := []struct {
		name string
		deps []planner.Dependency
		bad  bool
	}{
		{"valid", []planner.Dependency{ok}, false},
		{"pseudo version", []planner.Dependency{{Module: "example.org/dep1", Version: "v0.0.0-20240101000000-abcdef123456", Purpose: "p"}}, false},
		{"prerelease", []planner.Dependency{{Module: "example.org/dep1", Version: "v1.0.0-rc.1", Purpose: "p"}}, false},
		{"latest", []planner.Dependency{{Module: "example.org/dep1", Version: "latest", Purpose: "p"}}, true},
		{"branch", []planner.Dependency{{Module: "example.org/dep1", Version: "master", Purpose: "p"}}, true},
		{"range", []planner.Dependency{{Module: "example.org/dep1", Version: ">=v1.0.0", Purpose: "p"}}, true},
		{"empty version", []planner.Dependency{{Module: "example.org/dep1", Purpose: "p"}}, true},
		{"replace syntax", []planner.Dependency{{Module: "example.org/dep1 => ../x", Version: "v1.0.0", Purpose: "p"}}, true},
		{"local path", []planner.Dependency{{Module: "../dep1", Version: "v1.0.0", Purpose: "p"}}, true},
		{"absolute path", []planner.Dependency{{Module: "/tmp/dep1", Version: "v1.0.0", Purpose: "p"}}, true},
		{"no dot in host", []planner.Dependency{{Module: "dep1/x", Version: "v1.0.0", Purpose: "p"}}, true},
		{"flag like", []planner.Dependency{{Module: "-x.org/dep", Version: "v1.0.0", Purpose: "p"}}, true},
		{"at sign", []planner.Dependency{{Module: "example.org/dep1@v2", Version: "v1.0.0", Purpose: "p"}}, true},
		{"duplicate", []planner.Dependency{ok, ok}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := vetDeps(c.deps)
			if (err != nil) != c.bad {
				t.Fatalf("vetDeps err = %v, bad = %v", err, c.bad)
			}
			if err != nil {
				var se *stopError
				if !errors.As(err, &se) || se.Status != "failed" || se.Reason != "deps_invalid" {
					t.Fatalf("err = %v, want failed/deps_invalid", err)
				}
				for _, d := range c.deps {
					if d.Module != "" && d.Module != "example.org/dep1" && strings.Contains(se.Message, d.Module) {
						t.Fatalf("message quotes a module string: %q", se.Message)
					}
				}
			}
		})
	}
}

func TestDepsStepVetsBeforeTouchingAnything(t *testing.T) {
	g := newRig(t)
	rc := g.newRC(t)
	useModuleProxy(t, "http://127.0.0.1:1")
	rc.plan.Deps = []planner.Dependency{{Module: "example.org/dep1", Version: "latest", Purpose: "p"}}
	before := g.read("go.mod")
	err := rc.runDeps(context.Background())
	var se *stopError
	if !errors.As(err, &se) || se.Reason != "deps_invalid" {
		t.Fatalf("err = %v, want deps_invalid", err)
	}
	if g.read("go.mod") != before {
		t.Fatal("go.mod changed although the dependency was refused")
	}
	if len(g.sink.OfKind("deps_start")) != 0 {
		t.Fatal("the deps step started for an invalid dependency")
	}
}

func TestGoModVerifyAtEnd(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc := g.newRC(t)
	twoDepProxy(t)
	rc.plan.Deps = twoDeps
	ctx := context.Background()
	if err := rc.runDeps(ctx); err != nil {
		t.Fatal(err)
	}
	if err := rc.verifyModules(ctx); err != nil {
		t.Fatalf("a clean cache failed verify: %v", err)
	}
	target := filepath.Join(rc.modCache, "example.org", "dep1@v1.0.0", "d.go")
	makeTreeWritable(rc.modCache)
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-2] ^= 0x01
	if err := os.WriteFile(target, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	var se *stopError
	if err := rc.verifyModules(ctx); !errors.As(err, &se) || se.Status != "failed" || se.Reason != "mod_verify" {
		t.Fatalf("verify of a flipped byte = %v, want failed/mod_verify", err)
	}
}

func TestLeafCommandsOffline(t *testing.T) {
	g := newRig(t)
	rc := g.newRC(t)
	useModuleProxy(t, makeModuleProxy(t, "example.org/nope", "v1.0.0", map[string]string{"n.go": "package nope\n"}))
	ctx := context.Background()
	res := rc.goCmd(ctx, "n", envLeaf, "get", "example.org/nope@v1.0.0")
	if res.Err != nil || res.ExitCode == 0 {
		t.Fatalf("a leaf go get = exit %d, err %v; want a non-zero exit (GOPROXY=off)", res.ExitCode, res.Err)
	}
	if strings.Contains(g.read("go.mod"), "nope") {
		t.Fatal("a leaf command changed go.mod")
	}
	if fi, err := os.Stat(filepath.Join(g.runDir, "proxy.log")); err == nil && fi.Size() != 0 {
		t.Fatal("the proxy saw a request from a leaf command")
	}
	env, err := rc.env("n", envLeaf)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"GOPROXY=off", "GOFLAGS=-mod=readonly -buildvcs=false", "GOSUMDB=off", "GOPRIVATE=off", "GOTOOLCHAIN=local", "CGO_ENABLED=0"} {
		if !hasEnv(env, want) {
			t.Errorf("leaf env lacks %s", want)
		}
	}
}

func hasEnv(env []string, kv string) bool {
	for _, e := range env {
		if e == kv {
			return true
		}
	}
	return false
}

// sandboxRC is a runCtx with the sandbox on, or a skip when sandbox-exec does not work here.
func sandboxRC(t *testing.T) (*rig, *runCtx) {
	t.Helper()
	if err := sandbox.Preflight(context.Background()); err != nil {
		t.Skipf("sandbox-exec is not usable here: %v", err)
	}
	g := newRig(t, func(o *rigOpts) { o.Settings = func(c *settings.Config) { c.Executor.Sandbox = "on" } })
	rc := g.newRC(t)
	if !rc.sandboxOn {
		t.Fatal("the sandbox is not on")
	}
	return g, rc
}

func TestLeafCommandsOfflineUnderSandbox(t *testing.T) {
	g, rc := sandboxRC(t)
	useModuleProxy(t, makeModuleProxy(t, "example.org/nope", "v1.0.0", map[string]string{"n.go": "package nope\n"}))
	ctx := context.Background()
	if res := rc.goCmd(ctx, "n", envLeaf, "get", "example.org/nope@v1.0.0"); res.Err != nil || res.ExitCode == 0 {
		t.Fatalf("a sandboxed leaf go get = exit %d, err %v; want non-zero", res.ExitCode, res.Err)
	}
	// A test that dials a non-loopback address (TEST-NET-1, never routable) must get an error.
	write(t, filepath.Join(g.repo, "netprobe", "net_test.go"), `package netprobe

import (
	"net"
	"testing"
	"time"
)

func TestDialDenied(t *testing.T) {
	c, err := net.DialTimeout("tcp", "192.0.2.1:80", time.Second)
	if err == nil {
		c.Close()
		t.Fatal("a non-loopback dial succeeded")
	}
}
`)
	env, err := rc.env("n", envLeaf)
	if err != nil {
		t.Fatal(err)
	}
	v := rc.chk.Test(ctx, runner.TestSet{Repo: g.repo, Pkg: "./netprobe", Funcs: []string{"TestDialDenied"}, Env: env})
	if !v.Pass() {
		t.Fatalf("the dial probe did not pass (class %q)", v.Class)
	}
}

func TestDepsModeUsesWritableModCache(t *testing.T) {
	_, rc := sandboxRC(t)
	ctx := context.Background()
	probe := filepath.Join(rc.modCache, "probe")
	env, err := rc.env("n", envLeaf)
	if err != nil {
		t.Fatal(err)
	}
	touch := runner.Spec{Dir: rc.o.Repo, Argv: []string{"/usr/bin/touch", probe}, Env: env}
	if res := rc.chk.Run(ctx, touch); res.Err != nil || res.ExitCode == 0 {
		t.Fatalf("a leaf wrote the module cache (exit %d, err %v)", res.ExitCode, res.Err)
	}
	touch.ModCacheWritable = true
	if res := rc.chk.Run(ctx, touch); res.Err != nil || res.ExitCode != 0 {
		t.Fatalf("Spec.ModCacheWritable did not make the cache writable (exit %d, err %v)", res.ExitCode, res.Err)
	}
	_ = os.Remove(probe)

	// The deps step fills the cache under the sandbox: only the per-call flag differs.
	useModuleProxy(t, makeModuleProxy(t, "example.org/dep1", "v1.0.0", map[string]string{"d.go": "package dep1\n"}))
	rc.plan.Deps = twoDeps[:1]
	if err := rc.runDeps(ctx); err != nil {
		t.Fatalf("runDeps under the sandbox: %v", err)
	}
	if !fileExists(filepath.Join(rc.modCache, "example.org", "dep1@v1.0.0")) {
		t.Fatal("the deps step did not fill the module cache")
	}
}

func envValue(env []string, name string) (string, bool) {
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, name+"="); ok {
			return v, true
		}
	}
	return "", false
}

func TestDepsEnvIsNetworkedLeafEnvIsNot(t *testing.T) {
	g := newRig(t, func(o *rigOpts) {
		o.BriefEdit = func(s string) string {
			return strings.Replace(s, "\n---\n\n## Overview", "\nenv:\n  - name: GREETER_MODE\n    purpose: \"x\"\n    default: \"CANARY-env-value\"\n---\n\n## Overview", 1)
		}
	})
	rc := g.newRC(t)
	url := makeModuleProxy(t, "example.org/dep1", "v1.0.0", map[string]string{"d.go": "package dep1\n"})
	useModuleProxy(t, url)

	deps, err := rc.env("deps", envDeps)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"GOPROXY": url, "GOFLAGS": "-mod=mod -buildvcs=false", "GOSUMDB": "off", "GOTOOLCHAIN": "local",
		"GOPRIVATE": "off", "CGO_ENABLED": "0", "NO_PROXY": "127.0.0.1,localhost,::1", "GOPHERMIND_NODE": "deps",
	} {
		if got, ok := envValue(deps, name); !ok || got != want {
			t.Errorf("deps env %s = %q (set %v), want %q", name, got, ok, want)
		}
	}
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY"} {
		if got, _ := envValue(deps, name); !strings.HasPrefix(got, "http://node-deps@127.0.0.1:") {
			t.Errorf("deps env %s = %q, want the harness proxy URL for node deps", name, got)
		}
	}
	for _, e := range deps {
		if strings.Contains(e, "CANARY") || strings.HasPrefix(e, "GREETER_") {
			t.Fatalf("the deps env carries a brief env value or a secret: %s", strings.SplitN(e, "=", 2)[0])
		}
	}
	if !hasEnv(deps, "GOSUMDB=off") {
		t.Error("the injected checksum database setting is missing")
	}

	leaf, err := rc.env("fn-greet", envLeaf)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"GOPROXY": "off", "GOFLAGS": "-mod=readonly -buildvcs=false", "GOSUMDB": "off",
		"GREETER_TOKEN": canarySecret, "GREETER_MODE": "CANARY-env-value",
	} {
		if got, ok := envValue(leaf, name); !ok || got != want {
			t.Errorf("leaf env %s = %q (set %v), want %q", name, got, ok, want)
		}
	}

	// With the checksum database hook unset the variable is absent, so the go default applies.
	testHooks.GoSumDB = ""
	if d2, err := rc.env("deps", envDeps); err != nil {
		t.Fatal(err)
	} else if _, ok := envValue(d2, "GOSUMDB"); ok {
		t.Error("GOSUMDB is set although the hook is empty")
	}
	acc, err := rc.env("fn-greet", envAcceptance)
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := envValue(acc, "PATH"); !strings.HasPrefix(p, rc.binDir+":") {
		t.Errorf("acceptance PATH = %q, want %s first", p, rc.binDir)
	}
}
