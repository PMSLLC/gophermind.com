package projectrun

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"gopkg.in/yaml.v3"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/settings"
	"gophermind/gophermind-lib/briefv2/vault"
)

const rigRunID = "gm-2026-10-08-001"
const rigPass = "test-passphrase"

func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

// rig is a throwaway repo, config dir and Env. Nothing here touches the real
// ~/.gophermind or the real vault.
type rig struct {
	t         *testing.T
	repo      string
	cfgDir    string
	vaultPath string
	cfg       *settings.Config
	env       Env
	o         Options
	b         *brief.Brief
	srv       *httptest.Server
	environ   map[string]string

	providerCalls int32
	backendCalls  int32
}

func closedAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

func newRig(t *testing.T) *rig {
	t.Helper()
	skipIfNoGit(t)
	r := &rig{t: t, repo: t.TempDir(), cfgDir: t.TempDir(), environ: map[string]string{}}
	t.Setenv("GOPHERMIND_CONFIG_DIR", r.cfgDir)
	r.vaultPath = filepath.Join(t.TempDir(), "vault.age")

	gitIn(t, r.repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(r.repo, "README.md"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, r.repo, "add", "README.md")
	gitIn(t, r.repo, "commit", "-q", "-m", "init")

	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "{}") }))
	t.Cleanup(r.srv.Close)

	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "go"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitPath, _ := exec.LookPath("git")

	cfg := settings.Default()
	cfg.Providers = []settings.ProviderConfig{{
		Name: "fake", BaseURL: r.srv.URL + "/v1", Visibility: settings.Private, MaxConcurrent: 1,
		Models: []settings.ModelEntry{{ID: "fixture", ContextTokens: 1 << 20}},
	}}
	cfg.Models = map[string][]string{"strong": {"fake/fixture"}, "standard": {"fake/fixture"}, "any": {"fake/fixture"}}
	cfg.Executor.Sandbox = "off"
	cfg.Executor.GoModCache = filepath.Join(t.TempDir(), "gomodcache")
	cfg.Toolchain = map[string]string{"PATH": binDir}
	r.cfg = cfg
	r.environ["PATH"] = filepath.Dir(gitPath)
	r.environ[vault.PassphraseEnv] = rigPass

	env := DefaultEnv()
	env.SettingsPath = func() (string, error) { return filepath.Join(r.cfgDir, "gophermind.yaml"), nil }
	env.ConfigDir = func() (string, error) { return r.cfgDir, nil }
	env.VaultPath = func() (string, error) { return r.vaultPath, nil }
	env.OpenVault = func(path, pass string) (*vault.Vault, error) {
		return vault.Open(path, pass, vault.Options{WorkFactor: 10})
	}
	env.SandboxPreflight = func(context.Context) error { return nil }
	env.Getenv = func(k string) string { return r.environ[k] }
	env.Version = func() VersionInfo { return VersionInfo{"v0.0.0-test", "abc1234def", "now"} }
	env.Backends = func() (blackboard.Blackboard, ledger.Ledger) {
		atomic.AddInt32(&r.backendCalls, 1)
		return nil, nil
	}
	build := env.BuildProviders
	env.BuildProviders = func(c *settings.Config, s func(string) (string, error)) (map[string]provider.Provider, error) {
		atomic.AddInt32(&r.providerCalls, 1)
		return build(c, s)
	}
	r.env = env

	r.b = &brief.Brief{}
	r.b.Front.ID = rigRunID
	r.b.Front.Repo = r.repo
	r.b.Front.BaseBranch = "main"
	r.b.Front.Landing = "commit"
	r.o = Options{Repo: r.repo}
	r.writeSettings()
	return r
}

func (r *rig) writeSettings() {
	r.t.Helper()
	body, err := yaml.Marshal(r.cfg)
	if err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.cfgDir, "gophermind.yaml"), body, 0o600); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) declare(names ...string) {
	for _, n := range names {
		r.b.Front.Secrets = append(r.b.Front.Secrets, brief.Secret{Name: n})
	}
}

// seed puts harness-scope secrets into the rig's vault file.
func (r *rig) seed(scope string, kv map[string]string) {
	r.t.Helper()
	v, err := vault.Open(r.vaultPath, rigPass, vault.Options{WorkFactor: 10})
	if err != nil {
		r.t.Fatal(err)
	}
	for k, val := range kv {
		if err := v.Set(scope, k, val); err != nil {
			r.t.Fatal(err)
		}
	}
}

func (r *rig) run() PreflightResult {
	r.t.Helper()
	return Preflight(context.Background(), r.o, r.env, r.b)
}

func find(res PreflightResult, name string) (Check, bool) {
	for _, c := range res.Checks {
		if c.Name == name {
			return c, true
		}
	}
	return Check{}, false
}

func wantOK(t *testing.T, res PreflightResult, name string) Check {
	t.Helper()
	c, ok := find(res, name)
	if !ok {
		t.Fatalf("no check %q in %v", name, names(res))
	}
	if !c.OK {
		t.Fatalf("check %q failed: %q (fix %q)", name, c.Detail, c.Fix)
	}
	return c
}

func wantFail(t *testing.T, res PreflightResult, name string) Check {
	t.Helper()
	c, ok := find(res, name)
	if !ok {
		t.Fatalf("no check %q in %v", name, names(res))
	}
	if c.OK {
		t.Fatalf("check %q passed (%q), want a failure", name, c.Detail)
	}
	if c.Fix == "" {
		t.Errorf("check %q failed without a Fix", name)
	}
	return c
}

func names(res PreflightResult) []string {
	var out []string
	for _, c := range res.Checks {
		out = append(out, c.Name)
	}
	return out
}

func TestPreflightAllGood(t *testing.T) {
	r := newRig(t)
	res := r.run()
	if f := Failed(res.Checks); len(f) != 0 {
		t.Fatalf("failed: %+v", f)
	}
	want := []string{"repo", "clean tree", "stale state", "landing", "sandbox", "go toolchain", "git", "disk space", "settings", "provider fake", "vault passphrase"}
	if got := names(res); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("checks\n got %v\nwant %v", got, want)
	}
	if res.Config == nil || len(res.Probes) != 1 || !res.Probes[0].Answered {
		t.Fatalf("config %v probes %+v", res.Config, res.Probes)
	}
	if res.Store != nil {
		t.Fatalf("no vault was needed, Store = %v", res.Store)
	}
	var buf bytes.Buffer
	PrintPreflight(&buf, res.Checks)
	if buf.String() != fmt.Sprintf("preflight: ok (%d checks)\n", len(res.Checks)) {
		t.Fatalf("output %q", buf.String())
	}
}

func TestPreflightHumanListNumbered(t *testing.T) {
	r := newRig(t)
	if err := os.WriteFile(filepath.Join(r.repo, "dirty.txt"), []byte("d"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.b.Front.Landing = "pull_request"
	r.b.Front.BaseBranch = "nope"
	res := r.run()
	var buf bytes.Buffer
	PrintPreflight(&buf, res.Checks)
	out := buf.String()
	failed := Failed(res.Checks)
	if len(failed) != 3 {
		t.Fatalf("failed %d: %+v", len(failed), failed)
	}
	if !strings.HasPrefix(out, "preflight: FAILED (3 missing)\nWhat a human must provide, in this order:\n") {
		t.Fatalf("header: %q", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	last := lines[len(lines)-1]
	if !strings.Contains(last, "not a run failure") {
		t.Fatalf("last line %q", last)
	}
	for i, c := range failed {
		n := fmt.Sprintf("  %d. %s: ", i+1, c.Name)
		idx := -1
		for j, l := range lines {
			if strings.HasPrefix(l, n) {
				idx = j
			}
		}
		if idx < 0 || idx+1 >= len(lines) || !strings.HasPrefix(lines[idx+1], "     Fix: ") {
			t.Fatalf("entry %d (%q) not followed by a Fix line in\n%s", i+1, n, out)
		}
	}
	pathRE := regexp.MustCompile(`/[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)+`)
	for _, c := range res.Checks {
		for _, p := range pathRE.FindAllString(c.Detail, -1) {
			if !strings.HasPrefix(p, r.repo) && !strings.HasPrefix(p, r.cfgDir) {
				t.Errorf("check %q detail has a path outside the repo and config dir: %q", c.Name, p)
			}
		}
	}
	buf.Reset()
	PrintPreflight(&buf, append(res.Checks, Check{Name: "warning", OK: true, Detail: "line 3: FOO_KEY is not declared"}))
	if !strings.Contains(buf.String(), "warning: line 3: FOO_KEY is not declared\n") {
		t.Fatalf("warning not printed: %q", buf.String())
	}
}

// snapshot lists every file under the roots with its size.
func snapshot(t *testing.T, roots ...string) []string {
	t.Helper()
	var out []string
	for _, root := range roots {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && d.Name() == ".git" {
				return filepath.SkipDir
			}
			info, _ := d.Info()
			out = append(out, fmt.Sprintf("%s %d", p, info.Size()))
			return nil
		})
	}
	sort.Strings(out)
	return out
}

func TestPreflightMissingListedNoModelCallNothingWritten(t *testing.T) {
	r := newRig(t)
	// Everything missing at once.
	if err := os.Remove(filepath.Join(r.cfgDir, "gophermind.yaml")); err != nil {
		t.Fatal(err)
	}
	notRepo := t.TempDir()
	r.o.Repo = notRepo
	r.o.RequirePrivate = true
	r.o.Graded = true
	r.o.ExpectHead = "deadbeef"
	r.o.ExpectBinaryCommit = "zzz"
	r.b.Front.Landing = "pull_request"
	r.declare("DATABASE_URL")
	r.environ[vault.PassphraseEnv] = ""
	before := snapshot(t, r.cfgDir, filepath.Dir(r.vaultPath), r.repo, notRepo)
	res := r.run()
	after := snapshot(t, r.cfgDir, filepath.Dir(r.vaultPath), r.repo, notRepo)
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Fatalf("preflight wrote files:\nbefore %v\nafter  %v", before, after)
	}
	for _, name := range []string{"repo", "repo head", "landing", "settings", "binary", "vault passphrase", "secret DATABASE_URL"} {
		wantFail(t, res, name)
	}
	if n := len(Failed(res.Checks)); n < 8 {
		t.Errorf("only %d failures listed: %v", n, names(res))
	}
	if r.providerCalls != 0 || r.backendCalls != 0 {
		t.Errorf("provider calls %d, backend calls %d", r.providerCalls, r.backendCalls)
	}
	if res.Config != nil || res.Store != nil {
		t.Errorf("config %v store %v", res.Config, res.Store)
	}
}
