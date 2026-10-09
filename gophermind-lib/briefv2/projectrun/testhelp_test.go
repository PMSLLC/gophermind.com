package projectrun

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"gopkg.in/yaml.v3"

	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/envcheck"
	"gophermind/gophermind-lib/briefv2/executor"
	"gophermind/gophermind-lib/briefv2/packer"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/settings"
	"gophermind/gophermind-lib/briefv2/vault"
	"gophermind/gophermind-lib/gitenv"
)

// The end-to-end rig of this package. It cannot import executor/rig_test.go,
// so it has its own small copy of the idea: the real planner, the real
// executor and the real git over the greeter, with the model the only fake.

const (
	e2eID     = "gm-2026-09-30-901"
	e2eCanary = "CANARY-SECRET-VALUE"
	e2ePass   = "e2e-passphrase"
)

// sharedCfgDir is made by TestMain under os.TempDir().
var sharedCfgDir string

// TestMain unsets every GIT_* variable (a git hook exports them and they would
// point every git a test spawns at the real repository) and gives the process
// one config directory, removed at exit by removeTestDir.
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "GIT_") {
			os.Unsetenv(name)
		}
	}
	var err error
	if sharedCfgDir, err = os.MkdirTemp("", "gm-projtest-cfg-"); err != nil {
		fmt.Fprintln(os.Stderr, "TestMain: no config directory")
		os.Exit(2)
	}
	os.Setenv("GOPHERMIND_CONFIG_DIR", sharedCfgDir)
	code := m.Run()
	removeTestDir(sharedCfgDir)
	os.Exit(code)
}

// removeTestDir deletes a directory TestMain made: its name must start with
// gm-projtest- and it must sit directly in os.TempDir().
func removeTestDir(dir string) {
	if dir == "" || !strings.HasPrefix(filepath.Base(dir), "gm-projtest-") || filepath.Dir(dir) != filepath.Clean(os.TempDir()) {
		return
	}
	makeWritable(dir)
	_ = os.RemoveAll(dir)
}

// makeWritable lets a test delete a Go cache or module cache: the go tool
// writes its files and directories read-only.
func makeWritable(root string) {
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type()&os.ModeSymlink == 0 {
			_ = os.Chmod(p, 0o755)
		}
		return nil
	})
}

// fixtureDir is executor/testdata/greeter.
func fixtureDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "executor", "testdata", "greeter")
}

// ---- the combined model ----

// combo is the one provider of the rig. Planner requests are answered from
// the planner fixture (the override folders first); executor requests from
// impl/<file> named by the script, good.<node>.txt when the node has none.
type combo struct {
	fake *provider.Fake

	mu         sync.Mutex
	script     map[string][]string // node id -> impl files, in order; the last repeats
	used       map[string]int
	plannerReq map[string]int // stage -> requests
	execReq    map[string]int // stage -> requests
	seq        int64          // counts every request
	lastPlan   int64          // seq of the latest planner request
	// onExec runs when an executor request arrives, before its reply.
	onExec func(stage string)
}

func (c *combo) plannerRequests() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, v := range c.plannerReq {
		n += v
	}
	return n
}

func (c *combo) lastPlannerSeq() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastPlan
}

func (c *combo) nowSeq() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seq
}

func (c *combo) execStageCount(stage string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.execReq[stage]
}

func (c *combo) plannerStageCount(stage string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.plannerReq[stage]
}

// comboProvider builds the model over the fixture in dir. override folders
// are searched before dir/planner; script replaces the implement replies of
// the nodes it names.
func comboProvider(t *testing.T, dir string, override []string, script map[string][]string) *combo {
	t.Helper()
	plan, err := planner.FixtureProvider(append(append([]string{}, override...), filepath.Join(dir, "planner"))...)
	if err != nil {
		t.Fatal(err)
	}
	c := &combo{script: script, used: map[string]int{}, plannerReq: map[string]int{}, execReq: map[string]int{}}
	fn := func(_ int, req provider.Request) (provider.Response, error) {
		if stage := planner.StageOf(req); stage != "" {
			c.mu.Lock()
			c.seq++
			c.lastPlan = c.seq
			c.plannerReq[stage]++
			c.mu.Unlock()
			resp, err := plan.Complete(context.Background(), req)
			resp.Model = "m1"
			return resp, err
		}
		stage := ""
		for _, m := range req.Messages {
			if m.Role == provider.RoleSystem && strings.HasPrefix(m.Content, packer.SystemPrefix) {
				stage = strings.TrimPrefix(m.Content, packer.SystemPrefix)
			}
		}
		if stage == "" {
			return provider.Response{}, fmt.Errorf("combo provider: a request with neither a planner nor an executor stage")
		}
		c.mu.Lock()
		c.seq++
		c.execReq[stage]++
		hook := c.onExec
		c.mu.Unlock()
		if hook != nil {
			hook(stage)
		}
		text, err := c.execReply(dir, stage)
		return provider.Response{Text: text, Model: "m1", Usage: provider.Usage{PromptTokens: 1, CompletionTokens: 1}}, err
	}
	c.fake = provider.NewFake("a", []provider.ModelInfo{{ID: "m1", ContextTokens: 1 << 20}}, fn)
	return c
}

// execReply is the reply for one executor stage such as "implement:fn-hello".
func (c *combo) execReply(dir, stage string) (string, error) {
	kind, node, _ := strings.Cut(stage, ":")
	if kind == "revise" {
		return `{"notes":["check the default name"]}`, nil
	}
	files := []string{"good." + node + ".txt"}
	c.mu.Lock()
	if s, ok := c.script[node]; ok {
		files = s
	}
	i := c.used[node]
	c.used[node]++
	c.mu.Unlock()
	if i >= len(files) {
		i = len(files) - 1
	}
	raw, err := os.ReadFile(filepath.Join(dir, "impl", files[i]))
	if err != nil {
		return "", fmt.Errorf("combo provider: no reply for stage %q: %w", stage, err)
	}
	return string(raw), nil
}

// ---- the rig ----

type projectRig struct {
	t        *testing.T
	fixture  string
	dir      string // the rig's own temp dir: brief, settings, vault, archive
	repo     string
	cfgDir   string
	vault    string
	brief    string
	id       string
	branch   string
	model    *combo
	env      Env
	o        Options
	out, err *safeBuf
	environ  map[string]string // overrides of Getenv

	execCalls    int32
	execAtSeq    int64 // combo seq when the executor was called
	execRunIDs   []string
	providerCall int32
}

type rigOpt func(*rigCfg)

type rigCfg struct {
	clarify  string              // contents of the variant clarify.txt; "" keeps the two-question default
	override []string            // extra planner fixture folders, searched first
	script   map[string][]string // implement replies by node
	noToken  bool                // leave GREETER_TOKEN out of the vault
	noPass   bool                // an empty passphrase in the environment
}

func withOverride(dirs ...string) rigOpt {
	return func(c *rigCfg) { c.override = append(c.override, dirs...) }
}
func withScript(s map[string][]string) rigOpt {
	return func(c *rigCfg) { c.script = s }
}
func withoutToken() rigOpt      { return func(c *rigCfg) { c.noToken = true } }
func withoutPassphrase() rigOpt { return func(c *rigCfg) { c.noPass = true } }

const e2eClarify = `[
 {"id":"q1","question":"Should a name be trimmed of surrounding spaces before it is used?","why_it_matters":"It changes what Greet returns.","options":["Yes, trim it.","No, use it exactly as given."],"recommended":"Yes, trim it.","recommended_why":"Stray spaces are almost always a typing slip."},
 {"id":"q2","question":"Should an empty name fall back to world in the handlers?","why_it_matters":"It changes the HTTP behaviour.","depends_on":["q1"],"options":["Yes, use world.","No, refuse it."],"recommended":"Yes, use world.","recommended_why":"The brief says the query parameter defaults to world."}]`

func newProjectRig(t *testing.T, opts ...rigOpt) *projectRig {
	t.Helper()
	skipIfNoGit(t)
	var rc rigCfg
	for _, o := range opts {
		o(&rc)
	}
	r := &projectRig{t: t, fixture: fixtureDir(t), id: e2eID, out: &safeBuf{}, err: &safeBuf{}, environ: map[string]string{}}
	r.dir = t.TempDir()
	r.cfgDir = filepath.Join(r.dir, "cfg")
	r.vault = filepath.Join(r.dir, "vault.age")
	mustMkdir(t, r.cfgDir)
	// The planner finds its run record through the process environment: each
	// rig gets its own config dir, so ids that repeat across tests never see
	// each other's run records.
	t.Setenv("GOPHERMIND_CONFIG_DIR", r.cfgDir)
	t.Setenv("GOPHERMIND_VAULT_PATH", r.vault)
	if rc.noPass {
		t.Setenv(vault.PassphraseEnv, "")
	} else {
		t.Setenv(vault.PassphraseEnv, e2ePass)
	}

	// The repo: the fixture's cmd/greeter plus go.mod and .gitignore.
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r.repo = repo
	t.Cleanup(func() { makeWritable(repo) })
	copyFixtureTree(t, filepath.Join(r.fixture, "repo"), repo)
	writeFile(t, filepath.Join(repo, "go.mod"), "module example.com/greeter\n\ngo 1.22\n")
	writeFile(t, filepath.Join(repo, ".gitignore"), ".gophermind/\n")
	for _, a := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "rig@example.com"},
		{"config", "user.name", "Rig"},
		{"config", "commit.gpgsign", "false"},
		{"add", "go.mod", ".gitignore", "cmd/greeter/main.go"},
		{"commit", "-q", "-m", "seed"},
		{"tag", "baseline"},
	} {
		r.git(a...)
	}

	// The brief.
	raw, err := os.ReadFile(filepath.Join(r.fixture, "brief.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(raw), "REPO_DIR", repo, 1)
	text = mustReplace(t, text, "on_ambiguity: assume_and_document", "on_ambiguity: halt\nmilestone_approvals: true")
	text = mustReplace(t, text, "    purpose: \"Token the second acceptance check requires to be present\"\n",
		"    purpose: \"Token the second acceptance check requires to be present\"\n  - name: GREETER_SALT\n    purpose: \"Salt mixed into the greeting log\"\n")
	r.brief = filepath.Join(r.dir, "brief.md")
	writeFile(t, r.brief, text)
	b, err := brief.Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	r.branch = b.Front.WorkBranchName()

	// The variant clarify folder goes first.
	clarify := rc.clarify
	if clarify == "" {
		clarify = e2eClarify
	}
	vdir := filepath.Join(r.dir, "variant")
	mustMkdir(t, vdir)
	writeFile(t, filepath.Join(vdir, "clarify.txt"), clarify)
	r.model = comboProvider(t, r.fixture, append([]string{vdir}, rc.override...), rc.script)

	// The vault: GREETER_TOKEN in the harness scope, low work factor.
	if !rc.noToken {
		v, err := vault.Open(r.vault, e2ePass, vault.Options{WorkFactor: 10})
		if err != nil {
			t.Fatal(err)
		}
		if err := v.Set(vault.HarnessScope, "GREETER_TOKEN", e2eCanary); err != nil {
			t.Fatal(err)
		}
	}

	// The settings: one private provider a/m1, the go this test run uses.
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not on PATH")
	}
	cfg := settings.Default()
	cfg.Providers = []settings.ProviderConfig{{
		Name: "a", BaseURL: "http://a.invalid/v1", Visibility: settings.Private, MaxConcurrent: 1,
		Models: []settings.ModelEntry{{ID: "m1", ContextTokens: 1 << 20}},
	}}
	cfg.Models = map[string][]string{"strong": {"a/m1"}, "standard": {"a/m1"}, "any": {"a/m1"}}
	cfg.Executor.Sandbox = "off"
	if runtime.GOOS == "darwin" {
		if _, err := exec.LookPath("sandbox-exec"); err == nil {
			cfg.Executor.Sandbox = "on"
		} else {
			t.Fatalf("sandbox-exec is missing on darwin: the end-to-end run needs the real sandbox: %v", err)
		}
	} else {
		t.Logf("not darwin: executor.sandbox is off for this test")
	}
	cfg.Defaults.MaxRevisions = 1
	modBase := filepath.Join(r.dir, "modcache")
	t.Cleanup(func() { makeWritable(modBase) })
	cfg.Executor.GoModCache = filepath.Join(modBase, "gomodcache")
	cfg.Toolchain = map[string]string{"PATH": filepath.Dir(goBin) + ":/usr/bin:/bin"}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	body, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(r.cfgDir, "gophermind.yaml"), string(body))

	env := DefaultEnv()
	env.SettingsPath = func() (string, error) { return filepath.Join(r.cfgDir, "gophermind.yaml"), nil }
	env.ConfigDir = func() (string, error) { return r.cfgDir, nil }
	env.OpenVault = func(path, pass string) (*vault.Vault, error) {
		return vault.Open(path, pass, vault.Options{WorkFactor: 10})
	}
	env.Probe = func(_ context.Context, c *settings.Config) (*settings.Config, []envcheck.ProbeResult) {
		return c, []envcheck.ProbeResult{{Provider: "a", Host: "a.invalid", Answered: true}}
	}
	env.BuildProviders = func(*settings.Config, func(string) (string, error)) (map[string]provider.Provider, error) {
		atomic.AddInt32(&r.providerCall, 1)
		return map[string]provider.Provider{"a": r.model.fake}, nil
	}
	env.Getenv = func(k string) string {
		if v, ok := r.environ[k]; ok {
			return v
		}
		return os.Getenv(k)
	}
	env.Version = func() VersionInfo { return VersionInfo{"dev+abc1234", "abc1234", "2026-10-08"} }
	env.Executable = func() (string, error) { return "/opt/gophermind/bin/gophermind-dev", nil }
	env.RunExecutor = func(ctx context.Context, o executor.Options) (executor.Report, error) {
		atomic.AddInt32(&r.execCalls, 1)
		r.execAtSeq = r.model.nowSeq()
		rep, err := executor.Run(ctx, o)
		r.execRunIDs = append(r.execRunIDs, rep.RunID)
		return rep, err
	}
	r.env = env

	r.o = Options{
		BriefPath: r.brief, Repo: repo,
		Generate:   map[string]string{"GREETER_SALT": "hex32"},
		ExpectHead: "baseline", ExpectBinaryCommit: "abc1234",
		Out: r.out, Err: r.err,
	}
	r.o.Graded = true
	return r
}

func (r *projectRig) run() Result {
	r.t.Helper()
	return Run(context.Background(), r.o, r.env)
}

func (r *projectRig) runCtx(ctx context.Context) Result {
	r.t.Helper()
	return Run(ctx, r.o, r.env)
}

func (r *projectRig) runDir() string  { return filepath.Join(r.repo, ".gophermind", r.id) }
func (r *projectRig) scratch() string { return r.runDir() + "-scratch" }

// git runs the PATH git in the repo through gitenv.Command.
func (r *projectRig) git(args ...string) string {
	r.t.Helper()
	return e2eGit(r.t, r.repo, args...)
}

func e2eGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := gitenv.Command(dir, args...)
	cmd.Env = append(cmd.Env, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func (r *projectRig) project() ProjectReport {
	r.t.Helper()
	raw, err := os.ReadFile(filepath.Join(r.runDir(), "_state", "project.json"))
	if err != nil {
		r.t.Fatal(err)
	}
	var p ProjectReport
	if err := json.Unmarshal(raw, &p); err != nil {
		r.t.Fatal(err)
	}
	return p
}

func (r *projectRig) projectMap() map[string]any {
	r.t.Helper()
	raw, err := os.ReadFile(filepath.Join(r.runDir(), "_state", "project.json"))
	if err != nil {
		r.t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		r.t.Fatal(err)
	}
	return m
}

// ---- small file helpers ----

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustReplace(t *testing.T, s, old, repl string) string {
	t.Helper()
	if !strings.Contains(s, old) {
		t.Fatalf("the fixture brief has no %q", old)
	}
	return strings.Replace(s, old, repl, 1)
}

func copyFixtureTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), raw, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// walkFiles calls fn for every regular file under the roots.
func walkFiles(t *testing.T, roots []string, fn func(path string, raw []byte)) {
	t.Helper()
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && d.Name() == ".git" {
				return filepath.SkipDir
			}
			if d.Type().IsRegular() {
				if raw, err := os.ReadFile(p); err == nil {
					fn(p, raw)
				}
			}
			return nil
		})
	}
}
