package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/term"

	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/executor"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/sandbox"
	"gophermind/gophermind-lib/briefv2/settings"
	"gophermind/gophermind-lib/briefv2/vault"
	"gophermind/gophermind-lib/gitenv"
)

// minFreeBytes is the free space the module cache's file system must have.
var minFreeBytes uint64 = 2 << 30

// checkList collects the lines of `brief run --check-env`. Names are fixed
// words and ids; reasons are fixed sentences. No value of a secret, no URL and
// no output of a command is ever in a line.
type checkList struct {
	out    io.Writer
	passed int
	total  int
}

func (c *checkList) pass(name, detail string) {
	c.total++
	c.passed++
	if detail != "" {
		fmt.Fprintf(c.out, "pass: %s: %s\n", name, detail)
		return
	}
	fmt.Fprintf(c.out, "pass: %s\n", name)
}

func (c *checkList) fail(name, reason string) {
	c.total++
	fmt.Fprintf(c.out, "FAIL: %s: %s\n", name, reason)
}

// briefPreflight is the environment preflight: spec 19, no model call, exit 6
// on any failed check and 0 when all pass. It is not a run: nothing is written.
func briefPreflight(ctx context.Context, rec planner.RunRecord, repo string, cfg *settings.Config, openVault func() (*vault.Vault, error), in *os.File, out io.Writer) int {
	c := &checkList{out: out}

	// sandbox
	if on, err := sandbox.Required(cfg.Executor.Sandbox, runtime.GOOS); err != nil {
		c.fail("sandbox", "required on this platform is not met; set executor.sandbox: off to run without one")
	} else if !on {
		c.pass("sandbox", "setting off (recorded in the report)")
	} else if err := sandbox.Preflight(ctx); err != nil {
		c.fail("sandbox", "setting on but sandbox-exec is not available")
	} else {
		c.pass("sandbox", "setting on, sandbox-exec available")
	}

	// go toolchain
	if _, ok := lookInPath("go", cfg.Toolchain["PATH"]); ok {
		c.pass("go toolchain", "")
	} else {
		c.fail("go toolchain", "go was not found on the settings toolchain PATH")
	}

	// git repository and a clean tree
	if _, err := os.Lstat(filepath.Join(repo, ".git")); err != nil {
		c.fail("git repository", "the repository root has no .git")
	} else {
		c.pass("git repository", "")
	}
	if st, err := executor.LoadState(rec.RunDir); err == nil && st.StartedAt != "" {
		c.pass("clean tree", "the run has started; the executor checks a resume itself")
	} else if n, err := dirtyOutsideTests(repo, rec.RunDir); err != nil {
		c.fail("clean tree", "git status could not be read")
	} else if n > 0 {
		c.fail("clean tree", fmt.Sprintf("%d changed file(s) are not test files of the plan", n))
	} else {
		c.pass("clean tree", "")
	}

	// vault passphrase, declared secrets
	_, pwErr := runPassphraseNoPrompt(in)
	if pwErr != nil {
		c.fail("vault passphrase", "not set and no terminal to prompt; set GOPHERMIND_VAULT_PASSPHRASE")
	} else {
		c.pass("vault passphrase", "")
	}
	var secretNames []string
	var b *brief.Brief
	if src, err := os.ReadFile(filepath.Join(rec.RunDir, "brief.md")); err == nil {
		b, _ = brief.Parse(src)
	}
	if b != nil {
		for _, s := range b.Front.Secrets {
			secretNames = append(secretNames, s.Name)
		}
	}
	if len(secretNames) > 0 {
		var v *vault.Vault
		var verr error
		if pwErr != nil {
			verr = pwErr
		} else if path, err := vaultPath(); err != nil {
			verr = err
		} else if _, serr := os.Stat(path); serr != nil {
			v = nil // no vault file yet: nothing is in it
		} else {
			v, verr = openVault()
		}
		for _, name := range secretNames {
			label := "secret " + name
			switch {
			case pwErr != nil:
				c.fail(label, "not checked: no vault passphrase")
			case verr != nil:
				c.fail(label, "the vault could not be opened")
			default:
				val, ok := "", false
				if v != nil {
					val, ok = v.Get(vault.RunScope(rec.RunID), name)
				}
				if !ok {
					c.fail(label, "not in the vault under this run; run gophermind brief resume "+rec.RunID)
					continue
				}
				c.pass(label, "")
				if addr, ok := loopbackAddr(val); ok {
					conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
					if err != nil {
						c.fail("secret host "+name, "its host is not reachable on loopback")
					} else {
						conn.Close()
						c.pass("secret host "+name, "")
					}
				}
			}
		}
	}

	// providers of the strong tier
	_, res := resolveBaseURLs(ctx, cfg, &http.Client{})
	for _, r := range res {
		name := "provider " + r.Provider
		switch {
		case !r.Answered:
			c.fail(name, fmt.Sprintf("no base_url answered within %d s", int(probeTimeout/time.Second)))
		case r.Fallback:
			c.pass(name, "answered on fallback host "+safeLine(r.Host))
		default:
			c.pass(name, "")
		}
	}

	// approval
	if err := planner.VerifyApproval(rec.RunDir); err != nil {
		c.fail("approval", "approval.json is missing or does not match the plan")
	} else {
		c.pass("approval", "")
	}

	// disk space for the module cache
	if free, err := moduleCacheFree(cfg.Executor.GoModCache); err != nil {
		c.fail("disk space", "free space could not be read")
	} else if free < minFreeBytes {
		c.fail("disk space", fmt.Sprintf("less than %d MiB free for the module cache", minFreeBytes>>20))
	} else {
		c.pass("disk space", "")
	}

	fmt.Fprintf(out, "preflight: %d of %d passed\n", c.passed, c.total)
	if c.passed != c.total {
		return exitPreflight
	}
	return exitDone
}

// runPassphraseNoPrompt says whether a passphrase is available without
// asking: the environment variable, or a terminal to prompt on at run time.
func runPassphraseNoPrompt(in *os.File) (string, error) {
	if v := os.Getenv(vault.PassphraseEnv); v != "" {
		return v, nil
	}
	if in != nil && term.IsTerminal(int(in.Fd())) {
		return "", nil
	}
	return "", errNoPassphrase
}

func lookInPath(name, path string) (string, bool) {
	for _, dir := range filepath.SplitList(path) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0 {
			return p, true
		}
	}
	return "", false
}

// dirtyOutsideTests counts changed paths that are neither the test files the
// planner placed (_state/test_files.json) nor under .gophermind/.
func dirtyOutsideTests(repo, runDir string) (int, error) {
	allowed := map[string]bool{}
	if raw, err := os.ReadFile(filepath.Join(runDir, "_state", "test_files.json")); err == nil {
		var files []string
		if json.Unmarshal(raw, &files) == nil {
			for _, f := range files {
				allowed[filepath.ToSlash(f)] = true
			}
		}
	}
	// gitenv.Command drops every inherited GIT_* variable (a hook exports
	// GIT_DIR), so git reads the repository it is given and no other.
	cmd := gitenv.Command(repo, "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "core.sshCommand=",
		"status", "--porcelain=v1", "-z", "--untracked-files=all")
	cmd.Env = append(cmd.Env, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_OPTIONAL_LOCKS=0")
	raw, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range strings.Split(string(raw), "\x00") {
		if len(e) < 4 {
			continue
		}
		p := e[3:]
		if allowed[p] || p == ".gophermind" || strings.HasPrefix(p, ".gophermind/") {
			continue
		}
		n++
	}
	return n, nil
}

// loopbackAddr returns host:port when val is a URL whose host is loopback.
func loopbackAddr(val string) (string, bool) {
	u, err := url.Parse(val)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	host := u.Hostname()
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return "", false
	}
	port := u.Port()
	if port == "" {
		port = map[string]string{"postgres": "5432", "postgresql": "5432", "mysql": "3306", "redis": "6379", "http": "80", "https": "443"}[u.Scheme]
	}
	if port == "" {
		return "", false
	}
	return net.JoinHostPort(host, port), true
}

// moduleCacheFree is the free space on the file system that holds the module
// cache (or its nearest existing parent).
func moduleCacheFree(modCache string) (uint64, error) {
	p := modCache
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return 0, err
		}
		p = filepath.Join(home, p[2:])
	}
	for {
		if _, err := os.Stat(p); err == nil {
			return freeBytes(p)
		}
		next := filepath.Dir(p)
		if next == p {
			return 0, os.ErrNotExist
		}
		p = next
	}
}
