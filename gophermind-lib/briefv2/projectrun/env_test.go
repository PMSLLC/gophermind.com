package projectrun

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/ledger"
)

func TestDefaultEnvFieldsAreSet(t *testing.T) {
	v := reflect.ValueOf(DefaultEnv())
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).IsZero() {
			t.Errorf("Env.%s is nil", v.Type().Field(i).Name)
		}
	}
}

func TestDefaultEnvBackendsAreFilesystem(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("GOPHERMIND_CONFIG_DIR", cfg)
	bb, lg := DefaultEnv().Backends()
	if _, ok := bb.(*blackboard.FS); !ok {
		t.Fatalf("blackboard is %T", bb)
	}
	if _, ok := lg.(*ledger.FS); !ok {
		t.Fatalf("ledger is %T", lg)
	}
	ents, err := os.ReadDir(cfg)
	if err != nil || len(ents) != 0 {
		t.Fatalf("Backends created files: %v %v", ents, err)
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestEnvGitUsesCleanEnvAndAllowList(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	for _, d := range []string{a, b} {
		gitIn(t, d, "init", "-q", "-b", "main")
		gitIn(t, d, "commit", "-q", "--allow-empty", "-m", "x")
	}
	t.Setenv("GIT_DIR", filepath.Join(b, ".git"))
	e := DefaultEnv()
	ctx := context.Background()
	top, err := e.Git(ctx, a, "rev-parse", "--show-toplevel")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(a)
	got, _ := filepath.EvalSymlinks(strings.TrimSpace(top))
	if got != want {
		t.Fatalf("toplevel %q, want %q (GIT_DIR leaked)", got, want)
	}
	for _, args := range [][]string{
		{"rev-parse", "--is-inside-work-tree"}, {"rev-parse", "HEAD"}, {"rev-parse", "--verify", "HEAD^{commit}"},
		{"symbolic-ref", "--short", "HEAD"}, {"status", "--porcelain", "-uall"}, {"branch", "--list", "main"},
	} {
		if _, err := e.Git(ctx, a, args...); err != nil {
			t.Errorf("%v refused: %v", args, err)
		}
	}
	for _, args := range [][]string{
		{"push"}, {"reset", "--hard"}, {"checkout", "main"}, {"branch", "-D", "main"},
		{"status", "--porcelain", "-uall", "--ignored"}, {"status"}, {"rev-parse", "--git-dir"},
		{"rev-parse", "--verify", "--output=x"}, {"branch", "--list", "-a"}, {}, {"-C", "/", "status", "--porcelain", "-uall"},
	} {
		if _, err := e.Git(ctx, a, args...); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
}

func TestVaultPathHonorsOverride(t *testing.T) {
	t.Setenv("GOPHERMIND_VAULT_PATH", "/x/v.age")
	if p, err := DefaultEnv().VaultPath(); err != nil || p != "/x/v.age" {
		t.Fatalf("%q %v", p, err)
	}
	t.Setenv("GOPHERMIND_VAULT_PATH", "")
	cfg := t.TempDir()
	t.Setenv("GOPHERMIND_CONFIG_DIR", cfg)
	if p, err := DefaultEnv().VaultPath(); err != nil || p != filepath.Join(cfg, "vault.age") {
		t.Fatalf("%q %v", p, err)
	}
}

func TestDefaultDialTimesOut(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	start := time.Now()
	if err := DefaultEnv().Dial(context.Background(), addr); err == nil {
		t.Fatal("dial to a closed port succeeded")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
}
