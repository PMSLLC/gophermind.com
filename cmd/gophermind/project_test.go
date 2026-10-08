package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/executor"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/projectrun"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/settings"
	"gophermind/gophermind-lib/briefv2/vault"
)

const projGreeterID = "gm-2026-09-29-900"

// projRig is a throwaway repo, config dir and Env of fakes. Nothing here
// touches the real ~/.gophermind, the real vault or the network.
type projRig struct {
	t          *testing.T
	repo       string
	brief      string
	env        projectrun.Env
	exec       func(executor.Options) (executor.Report, error)
	execCalls  int32
	provCalls  int32
	vaultOpens int32
	cfgDir     string
	environ    map[string]string
}

func projGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	cmd.Env = nil
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GIT_") {
			cmd.Env = append(cmd.Env, e)
		}
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func projTestdata(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "gophermind-lib", "briefv2", "planner", "testdata", "greeter")
}

func newProjRig(t *testing.T) *projRig {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	r := &projRig{t: t, repo: t.TempDir()}
	cfgDir := t.TempDir()
	r.cfgDir = cfgDir
	t.Setenv("GOPHERMIND_CONFIG_DIR", cfgDir)
	projGit(t, r.repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(r.repo, "README.md"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	projGit(t, r.repo, "add", "README.md")
	projGit(t, r.repo, "commit", "-q", "-m", "init")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "{}") }))
	t.Cleanup(srv.Close)
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "go"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitPath, _ := exec.LookPath("git")

	cfg := settings.Default()
	cfg.Providers = []settings.ProviderConfig{{
		Name: "fake", BaseURL: srv.URL + "/v1", Visibility: settings.Private, MaxConcurrent: 1,
		Models: []settings.ModelEntry{{ID: "fixture", ContextTokens: 1 << 20}},
	}}
	cfg.Models = map[string][]string{"strong": {"fake/fixture"}, "standard": {"fake/fixture"}, "any": {"fake/fixture"}}
	cfg.Executor.Sandbox = "off"
	cfg.Executor.GoModCache = filepath.Join(t.TempDir(), "gomodcache")
	cfg.Toolchain = map[string]string{"PATH": binDir}
	body, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "gophermind.yaml"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	environ := map[string]string{"PATH": filepath.Dir(gitPath), vault.PassphraseEnv: "test-passphrase"}
	r.environ = environ
	vaultPath := filepath.Join(t.TempDir(), "vault.age")

	env := projectrun.DefaultEnv()
	env.SettingsPath = func() (string, error) { return filepath.Join(cfgDir, "gophermind.yaml"), nil }
	env.ConfigDir = func() (string, error) { return cfgDir, nil }
	env.VaultPath = func() (string, error) { return vaultPath, nil }
	env.OpenVault = func(path, pass string) (*vault.Vault, error) {
		atomic.AddInt32(&r.vaultOpens, 1)
		return vault.Open(path, pass, vault.Options{WorkFactor: 10})
	}
	env.SandboxPreflight = func(context.Context) error { return nil }
	env.Getenv = func(k string) string { return environ[k] }
	env.Version = func() projectrun.VersionInfo {
		return projectrun.VersionInfo{Version: "v0.0.0-test", Commit: "abc1234def", Date: "now"}
	}
	env.Backends = func() (blackboard.Blackboard, ledger.Ledger) {
		return blackboard.NewFS(projRunDir), ledger.NewFS(projRunDir)
	}
	fake, err := planner.FixtureProvider(projTestdata(t))
	if err != nil {
		t.Fatal(err)
	}
	env.BuildProviders = func(*settings.Config, func(string) (string, error)) (map[string]provider.Provider, error) {
		atomic.AddInt32(&r.provCalls, 1)
		return map[string]provider.Provider{"fake": fake}, nil
	}
	env.RunExecutor = func(_ context.Context, o executor.Options) (executor.Report, error) {
		atomic.AddInt32(&r.execCalls, 1)
		return r.exec(o)
	}
	r.exec = func(executor.Options) (executor.Report, error) {
		return executor.Report{RunID: projGreeterID, Status: "verified", Sandbox: "off"}, nil
	}
	r.env = env

	raw, err := os.ReadFile(filepath.Join(projTestdata(t), "brief.md"))
	if err != nil {
		t.Fatal(err)
	}
	r.brief = filepath.Join(t.TempDir(), "brief.md")
	if err := os.WriteFile(r.brief, []byte(strings.Replace(string(raw), "REPO_DIR", r.repo, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	old := projectEnv
	projectEnv = func() projectrun.Env { return r.env }
	t.Cleanup(func() { projectEnv = old })
	return r
}

func projRunDir(runID string) (string, error) {
	rec, err := planner.LookupRun(runID)
	if err != nil {
		return "", err
	}
	return rec.RunDir, nil
}

func (r *projRig) cmd(args ...string) (int, string, string) {
	r.t.Helper()
	var out, errb bytes.Buffer
	code := runProject(append([]string{r.brief, "--repo", r.repo}, args...), nil, &out, &errb)
	return code, out.String(), errb.String()
}

func TestProjectCLIUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"--bogus"}} {
		var out, errb bytes.Buffer
		code := runProject(args, nil, &out, &errb)
		if code != 1 {
			t.Errorf("%v: code = %d, want 1", args, code)
		}
		if !strings.Contains(errb.String(), "gophermind project <brief.md>") {
			t.Errorf("%v: stderr lacks the usage: %q", args, errb.String())
		}
		if len(args) > 0 && !strings.Contains(errb.String(), "error: ") {
			t.Errorf("%v: stderr lacks the error line: %q", args, errb.String())
		}
		if out.Len() != 0 {
			t.Errorf("%v: stdout = %q", args, out.String())
		}
	}
}

func TestProjectCLIExitCodes(t *testing.T) {
	rows := []struct {
		name  string
		code  int
		setup func(*projRig)
		// end is a proof line the stdout must end with, "" when none.
		end string
	}{
		{"verified", 0, nil, "Acceptance passed"},
		{"failed", 1, func(r *projRig) {
			r.exec = func(executor.Options) (executor.Report, error) {
				return executor.Report{RunID: projGreeterID, Status: "failed", StopReason: "acceptance_failed", Sandbox: "off"}, nil
			}
		}, "Acceptance passed"},
		{"invalid", 2, func(r *projRig) {
			r.exec = func(executor.Options) (executor.Report, error) {
				return executor.Report{}, &brief.InvalidError{Reason: "x"}
			}
		}, ""},
		{"escalated", 4, func(r *projRig) {
			r.exec = func(executor.Options) (executor.Report, error) {
				return executor.Report{RunID: projGreeterID, Status: "escalated", StopReason: "human_stop", Sandbox: "off"}, nil
			}
		}, "Acceptance passed"},
		{"interrupted", 5, func(r *projRig) {
			r.exec = func(executor.Options) (executor.Report, error) {
				return executor.Report{RunID: projGreeterID, Status: "interrupted", StopReason: "interrupted", Sandbox: "off"}, nil
			}
		}, "resume with --resume"},
		{"preflight", 6, func(r *projRig) {
			r.env.LookPath = func(string, string) (string, bool) { return "", false }
		}, ""},
		{"fault", 7, func(r *projRig) {
			r.exec = func(executor.Options) (executor.Report, error) {
				return executor.Report{}, errors.New("executor: no repo")
			}
		}, ""},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			r := newProjRig(t)
			if row.setup != nil {
				row.setup(r)
			}
			code, out, errs := r.cmd()
			if code != row.code {
				t.Fatalf("code = %d, want %d\nout:\n%s\nerr:\n%s", code, row.code, out, errs)
			}
			if row.end != "" {
				if !strings.Contains(projLastLines(out, 4), row.end) {
					t.Errorf("stdout does not end with %q:\n%s", row.end, out)
				}
			}
			if row.code == 6 {
				if out != "" || errs == "" {
					t.Errorf("preflight failure: stdout %q stderr %q", out, errs)
				}
			}
		})
	}
}

func projLastLines(s string, n int) string {
	ls := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(ls) > n {
		ls = ls[len(ls)-n:]
	}
	return strings.Join(ls, "\n")
}

func TestProjectCLIPrintStatePaths(t *testing.T) {
	r := newProjRig(t)
	// Config, vault and settings live under a parent that does not exist, and
	// the preflight would fail (no passphrase, no tools): the command must
	// still exit 0, create nothing and open no vault.
	missing := filepath.Join(t.TempDir(), "absent")
	r.env.ConfigDir = func() (string, error) { return filepath.Join(missing, "cfg"), nil }
	r.env.VaultPath = func() (string, error) { return filepath.Join(missing, "cfg", "vault.age"), nil }
	r.env.SettingsPath = func() (string, error) { return filepath.Join(missing, "cfg", "gophermind.yaml"), nil }
	r.env.Getenv = func(string) string { return "" }
	r.env.LookPath = func(string, string) (string, bool) { return "", false }
	code, out, errs := r.cmd("--print-state-paths")
	if code != 0 {
		t.Fatalf("code = %d, err %q", code, errs)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("out = %q", out)
	}
	for _, l := range lines {
		if len(strings.Split(l, "\t")) != 3 {
			t.Errorf("line is not <action>\\t<scope>\\t<path>: %q", l)
		}
	}
	if r.provCalls != 0 || r.execCalls != 0 || r.vaultOpens != 0 {
		t.Errorf("work started: provider %d executor %d vault %d", r.provCalls, r.execCalls, r.vaultOpens)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("the nonexistent parent was created (stat err %v)", err)
	}
	if _, err := os.Stat(filepath.Join(r.repo, ".gophermind")); err == nil {
		t.Error("a run folder was made")
	}
}

// An unreadable settings file makes StatePaths fail, and the command exits 1.
// That is intended: the paths depend on executor.go_mod_cache from settings,
// and a guess would list the wrong folder to delete.
func TestProjectCLIPrintStatePathsInvalidSettings(t *testing.T) {
	r := newProjRig(t)
	if err := os.WriteFile(filepath.Join(r.cfgDir, "gophermind.yaml"), []byte("not_a_setting: [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errs := r.cmd("--print-state-paths")
	if code != 1 || out != "" || !strings.Contains(errs, "error: ") || !strings.Contains(errs, "settings") {
		t.Errorf("code %d out %q err %q", code, out, errs)
	}
}

func TestProjectCLIPreflightOnly(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		r := newProjRig(t)
		code, out, _ := r.cmd("--preflight-only")
		if code != 0 || !strings.Contains(out, "preflight: ok") {
			t.Errorf("code %d out %q", code, out)
		}
		if r.provCalls != 0 || r.execCalls != 0 {
			t.Error("work started")
		}
		// The greeter brief declares no secrets, so the preflight does not
		// open the vault and Run opens nothing before or beside it.
		if r.vaultOpens != 0 {
			t.Errorf("vault opened %d times, want 0", r.vaultOpens)
		}
	})
	t.Run("missing", func(t *testing.T) {
		r := newProjRig(t)
		r.env.LookPath = func(string, string) (string, bool) { return "", false }
		code, out, errs := r.cmd("--preflight-only")
		if code != 6 {
			t.Fatalf("code = %d, err %q", code, errs)
		}
		if out != "" {
			t.Errorf("stdout = %q, want empty", out)
		}
		if !strings.Contains(errs, "git") && !strings.Contains(errs, "go") {
			t.Errorf("stderr lists no missing item: %q", errs)
		}
	})
	t.Run("no vault open before or without a passphrase", func(t *testing.T) {
		for _, args := range [][]string{{"--preflight-only"}, {}} {
			r := newProjRig(t)
			r.environ[vault.PassphraseEnv] = ""
			r.env.LookPath = func(string, string) (string, bool) { return "", false }
			code, _, errs := r.cmd(args...)
			if code != 6 {
				t.Fatalf("%v: code = %d, err %q", args, code, errs)
			}
			if r.vaultOpens != 0 || r.provCalls != 0 || r.execCalls != 0 {
				t.Errorf("%v: vault %d provider %d executor %d, want 0", args, r.vaultOpens, r.provCalls, r.execCalls)
			}
		}
	})
}

func TestProjectCLINeverReadsStdinUnattended(t *testing.T) {
	r := newProjRig(t)
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	defer pw.Close()
	if _, err := pw.WriteString("y\n"); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := runProject([]string{r.brief, "--repo", r.repo}, pr, &out, &errb)
	if code != 0 {
		t.Fatalf("code = %d\nout:\n%s\nerr:\n%s", code, out.String(), errb.String())
	}
	// The bytes written before the run must still be there, unread.
	if err := pr.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 8)
	n, err := pr.Read(buf)
	if err != nil || string(buf[:n]) != "y\n" {
		t.Errorf("stdin was consumed: read %q, err %v", buf[:n], err)
	}
}

func TestProjectCLIGradedFlags(t *testing.T) {
	r := newProjRig(t)
	head := strings.TrimSpace(projGitOut(t, r.repo, "rev-parse", "HEAD"))
	code, out, errs := r.cmd("--expect-head", head)
	if code != 0 {
		t.Fatalf("code = %d\nout:\n%s\nerr:\n%s", code, out, errs)
	}
	if !strings.Contains(out, "graded: yes") {
		t.Errorf("--expect-head alone did not set graded:\n%s", out)
	}
	code, out, errs = r.cmd("--graded", "--resume")
	if code != 1 || !strings.Contains(errs, "--graded needs --expect-head") && !strings.Contains(errs, "never resumes") {
		t.Errorf("--graded --resume: code %d err %q", code, errs)
	}
	if out != "" {
		t.Errorf("stdout = %q", out)
	}
}

func projGitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GIT_") {
			cmd.Env = append(cmd.Env, e)
		}
	}
	b, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
