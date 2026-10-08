package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"golang.org/x/term"

	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/envcheck"
	"gophermind/gophermind-lib/briefv2/executor"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/sandbox"
	"gophermind/gophermind-lib/briefv2/settings"
	"gophermind/gophermind-lib/briefv2/vault"
)

// minFreeBytes is the free space the module cache's file system must have.
var minFreeBytes = envcheck.DefaultMinFreeBytes

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

	// vault passphrase, declared secrets. The vault is opened by a run only for a
	// declared secret or a provider's api_key_secret; otherwise no passphrase is needed.
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
	vaultNeeded := len(secretNames) > 0
	for _, p := range cfg.Providers {
		if p.APIKeySecret != "" {
			vaultNeeded = true
		}
	}
	_, pwErr := runPassphraseNoPrompt(in)
	switch {
	case !vaultNeeded:
		c.pass("vault passphrase", "not needed")
	case pwErr != nil:
		c.fail("vault passphrase", "not set and no terminal to prompt; set GOPHERMIND_VAULT_PASSPHRASE")
	default:
		c.pass("vault passphrase", "")
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

	// providers every tier chain uses
	_, res := resolveBaseURLs(ctx, cfg, providerHTTPClient(cfg))
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

func lookInPath(name, path string) (string, bool) { return envcheck.LookInPath(name, path) }

func dirtyOutsideTests(repo, runDir string) (int, error) {
	return envcheck.DirtyOutsideTests(repo, runDir)
}

func loopbackAddr(val string) (string, bool) { return envcheck.LoopbackAddr(val) }

func moduleCacheFree(modCache string) (uint64, error) { return envcheck.ModuleCacheFree(modCache) }
