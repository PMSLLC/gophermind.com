package executor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/db"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/gitland"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/settings"
	"gophermind/gophermind-lib/briefv2/vault"
)

const (
	greeterDir    = "testdata/greeter"
	greeterID     = "gm-2026-09-30-901"
	canarySecret  = "CANARY-SECRET-VALUE"
	greeterSecret = "GREETER_TOKEN"
)

// memSecrets is an in-memory vault: it satisfies the planner's Secrets (Get,
// Set) and the executor's Secrets (Env, Get).
type memSecrets struct {
	mu   sync.Mutex
	data map[string]map[string]string
}

func newMemSecrets() *memSecrets { return &memSecrets{data: map[string]map[string]string{}} }

func (m *memSecrets) Get(scope, name string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[scope][name]
	return v, ok
}

func (m *memSecrets) Set(scope, name, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data[scope] == nil {
		m.data[scope] = map[string]string{}
	}
	m.data[scope][name] = value
	return nil
}

func (m *memSecrets) Env(scope string, names []string) ([]string, error) {
	var out []string
	for _, n := range names {
		if v, ok := m.Get(scope, n); ok {
			out = append(out, n+"="+v)
		}
	}
	return out, nil
}

var (
	_ Secrets         = (*memSecrets)(nil)
	_ planner.Secrets = (*memSecrets)(nil)
)

// rig is one greeter repository, one config dir and one database, with the
// approved plan the real planner produced offline from testdata/greeter.
// Tasks 9c to 16 add the scripted provider, the router and the gate.
type rig struct {
	t                *testing.T
	repo, runDir, id string
	cfg              *settings.Config // chains "standard" and "strong": [a/m1, b/m2]
	board            *blackboard.SQLite
	led              *ledger.SQLite
	sink             *events.Collector
	secrets          Secrets // in-memory, GREETER_TOKEN = CANARY-SECRET-VALUE
	git              gitland.Repo
	plan             *Plan
	fake             *scriptedProvider // set by wire
	router           *router.Router    // the real router over the fake providers, set by wire
	gate             *scriptGate       // set by wire
}

type rigOpts struct {
	Settings  func(*settings.Config) // edit the config before it is validated
	BriefEdit func(string) string    // edit the brief text before planning
}

func newRig(t *testing.T, mods ...func(*rigOpts)) *rig {
	t.Helper()
	var ro rigOpts
	for _, m := range mods {
		m(&ro)
	}
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A go command may still be writing its telemetry counters into the scratch
	// HOME when the test ends; retry the removal instead of failing the cleanup.
	t.Cleanup(func() {
		var err error
		for i := 0; i < 30; i++ {
			makeTreeWritable(repo)
			if err = os.RemoveAll(repo); err == nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Logf("the temporary repository %s could not be removed after 30 tries: %v", repo, err)
	})
	g := &rig{t: t, repo: repo, id: greeterID, sink: events.NewCollector()}
	g.runDir = filepath.Join(repo, ".gophermind", greeterID)

	copyTree(t, filepath.Join(greeterDir, "repo"), repo)
	write(t, filepath.Join(repo, "go.mod"), "module example.com/greeter\n\ngo 1.22\n")
	write(t, filepath.Join(repo, ".gitignore"), ".gophermind/\n")
	g.gitCmd("init", "-q", "-b", "main")
	g.gitCmd("config", "user.email", "rig@example.com")
	g.gitCmd("config", "user.name", "Rig")
	g.gitCmd("config", "commit.gpgsign", "false")
	g.gitCmd("add", "go.mod", ".gitignore", "cmd/greeter/main.go")
	g.gitCmd("commit", "-q", "-m", "seed")

	raw, err := os.ReadFile(filepath.Join(greeterDir, "brief.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(raw), "REPO_DIR", repo, 1)
	if ro.BriefEdit != nil {
		text = ro.BriefEdit(text)
	}
	briefPath := filepath.Join(t.TempDir(), "brief.md")
	write(t, briefPath, text)

	d, err := db.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	g.led, g.board = ledger.NewSQLite(d), blackboard.NewSQLite(d)

	mem := newMemSecrets()
	g.secrets = mem
	g.planOffline(briefPath, mem)

	g.cfg = settings.Default()
	g.cfg.Providers = []settings.ProviderConfig{
		{Name: "a", BaseURL: "http://a.invalid/v1", Visibility: settings.Private, MaxConcurrent: 1,
			Models: []settings.ModelEntry{{ID: "m1", ContextTokens: 32768}}},
		{Name: "b", BaseURL: "http://b.invalid/v1", Visibility: settings.Private, MaxConcurrent: 1,
			Models: []settings.ModelEntry{{ID: "m2", ContextTokens: 32768}}},
	}
	g.cfg.Models = map[string][]string{"strong": {"a/m1", "b/m2"}, "standard": {"a/m1", "b/m2"}, "any": {"a/m1", "b/m2"}}
	g.cfg.Privacy.Mode = "need_to_know"
	g.cfg.Executor.Sandbox = "off"
	g.cfg.Executor.Workers = 1
	// The module cache is a temp directory (never the real ~/.gophermind), and the
	// toolchain PATH holds the go this test run uses.
	modDir := t.TempDir()
	t.Cleanup(func() { makeTreeWritable(modDir) }) // the go tool makes cached modules read-only
	g.cfg.Executor.GoModCache = filepath.Join(modDir, "gomodcache")
	g.cfg.Toolchain = map[string]string{"PATH": testToolchainPATH(t)}
	if ro.Settings != nil {
		ro.Settings(g.cfg)
	}
	if err := g.cfg.Validate(); err != nil {
		t.Fatal(err)
	}

	cli, err := gitland.NewCLI(repo, greeterID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cli.Close() })
	g.git = cli

	g.plan, err = LoadPlan(g.runDir, repo)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// planOffline runs the real planner over the scripted replies in
// testdata/greeter/planner, so approval, coverage, the tree, the test files
// and _state/leaf_tests.json are produced by the code under test.
func (g *rig) planOffline(briefPath string, mem *memSecrets) {
	g.t.Helper()
	fake, err := planner.FixtureProvider(filepath.Join(greeterDir, "planner"))
	if err != nil {
		g.t.Fatal(err)
	}
	cfg := planner.FixtureSettings()
	rt := router.New(cfg, map[string]provider.Provider{"fake": fake}, g.led, g.sink)
	deps := planner.Deps{
		Caller: rt, Sink: g.sink, Board: g.board, Settings: cfg, LedgerErrors: rt.LedgerErrors,
		OpenSecrets:  func() (planner.Secrets, error) { return mem, nil },
		PromptSecret: func(name, purpose string) (string, error) { return canarySecret, nil },
	}
	out, err := planner.New(deps).Run(context.Background(), planner.Options{BriefPath: briefPath, Yes: true})
	if err != nil || out != planner.Done {
		g.t.Fatalf("planner.Run = %q, %v; want done", out, err)
	}
	if v, ok := mem.Get(vault.RunScope(g.id), greeterSecret); !ok || v != canarySecret {
		g.t.Fatal("the planner did not store the brief's secret")
	}
}

// gitCmd runs git in the repo with no user or system configuration.
func (g *rig) gitCmd(args ...string) string {
	g.t.Helper()
	bin := os.Getenv("GITLAND_TEST_GIT")
	if bin == "" {
		bin = "git"
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = g.repo
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		g.t.Fatalf("git %s: %v\n%s", args[0], err, out)
	}
	return string(out)
}

// testToolchainPATH is the directory of the go on PATH plus the system bins.
func testToolchainPATH(t *testing.T) string {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not on PATH")
	}
	return filepath.Dir(goBin) + ":/usr/bin:/bin"
}

// makeTreeWritable lets a test delete a module cache: the go tool writes its
// files and directories read-only.
func makeTreeWritable(root string) {
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Type()&os.ModeSymlink == 0 { // Chmod follows links: never chmod through one
			_ = os.Chmod(p, 0o755)
		}
		return nil
	})
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
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

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func containsCanary(s string) bool { return strings.Contains(s, canarySecret) }
