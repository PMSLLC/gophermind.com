package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/report"
	"gophermind/gophermind-lib/briefv2/settings"
	"gophermind/gophermind-lib/briefv2/vault"
)

// useRunE2E is useRun with a claim that goes stale after one second (so a
// resume right after a kill is not refused) and, on darwin, a refusal to run
// without the real sandbox: a missing sandbox-exec is a failure here.
func useRunE2E(t *testing.T, fn func(context.Context, string, int) (string, error)) {
	t.Helper()
	if runtime.GOOS == "darwin" {
		if _, err := exec.LookPath("sandbox-exec"); err != nil {
			t.Fatalf("sandbox-exec is missing on darwin: %v", err)
		}
	}
	useRun(t, fn)
	hook := runHook
	runHook = func() (*settings.Config, map[string]provider.Provider, error) {
		cfg, p, err := hook()
		if cfg != nil {
			cfg.Executor.StaleClaimSeconds = 1
		}
		return cfg, p, err
	}
}

// cliStoresHold reports every store of a finished CLI run that holds needle:
// the run folder, the SQLite files under the config dir, every object of the
// repository and what the CLI printed.
func cliStoresHold(t *testing.T, repo, needle string, printed ...string) []string {
	t.Helper()
	var hits []string
	check := func(name string, b []byte) {
		if bytes.Contains(b, []byte(needle)) {
			hits = append(hits, name)
		}
	}
	_ = filepath.WalkDir(filepath.Join(repo, ".gophermind"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() != "greeter" {
			if b, rerr := os.ReadFile(p); rerr == nil {
				check(p, b)
			}
		}
		return nil
	})
	_ = filepath.WalkDir(os.Getenv("GOPHERMIND_CONFIG_DIR"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if b, rerr := os.ReadFile(p); rerr == nil {
				check(p, b)
			}
		}
		return nil
	})
	check("git objects", []byte(gitOut(t, repo, "cat-file", "--batch-all-objects", "--batch")))
	check("git log", []byte(gitOut(t, repo, "log", "--all", "-p", "--format=%B")))
	for i, s := range printed {
		check([]string{"stdout", "stderr"}[i%2], []byte(s))
	}
	return hits
}

func endState(t *testing.T, repo string) (string, []string) {
	t.Helper()
	tree := strings.TrimSpace(gitOut(t, repo, "rev-parse", "main^{tree}"))
	return tree, strings.Split(strings.TrimSpace(gitOut(t, repo, "log", "--format=%s", "main")), "\n")
}

// TestBriefRunE2EResumeAfterKill drives the CLI itself: an uninterrupted
// `brief run`, then a second run whose process is killed with SIGKILL while
// fn-hello's first reply is asked for, resumed with `brief run`. Both end with
// the same tree and commits, GopherMind's own on main, and no secret in any
// store or in what the CLI printed.
func TestBriefRunE2EResumeAfterKill(t *testing.T) {
	// The uninterrupted run.
	id, repo := plannedGreeter(t)
	useRunE2E(t, goodReplies(t, ""))
	start := time.Now()
	code, out, errs := runBriefCmd(t, "", "run", id)
	t.Logf("uninterrupted CLI run: %v", time.Since(start).Round(time.Second))
	if code != 0 {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
	requireProofLines(t, out)
	if rep, err := report.Read(runDirOf(repo, id)); err != nil || rep.Status != "verified" || (runtime.GOOS == "darwin" && rep.Sandbox != "on") {
		t.Fatalf("report = %+v, %v", rep, err)
	}
	if hits := cliStoresHold(t, repo, cliCanary, out, errs); len(hits) > 0 {
		t.Errorf("the secret is in %v", hits)
	}
	wantTree, wantSubjects := endState(t, repo)
	if len(wantSubjects) != 8 || !strings.HasPrefix(wantSubjects[0], "gm(run): ") {
		t.Fatalf("main holds %q", wantSubjects)
	}

	// The killed run.
	id, repo = plannedGreeter(t)
	marker := filepath.Join(t.TempDir(), "in-flight")
	cmd := exec.Command(os.Args[0], "-test.run=^TestBriefRunE2EChild$")
	cmd.Env = append(os.Environ(), "GM_CLI_E2E_CHILD=1", "GM_CLI_E2E_MARKER="+marker)
	var childOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &childOut, &childOut
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	deadline := time.After(8 * time.Minute)
wait:
	for {
		select {
		case <-exited:
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("the child ended before the kill point (%d bytes of output)", childOut.Len())
			}
			break wait
		case <-deadline:
			_ = cmd.Process.Kill()
			t.Fatal("the kill point was not reached in time")
		case <-time.After(50 * time.Millisecond):
			if _, err := os.Stat(marker); err == nil {
				break wait
			}
		}
	}
	_ = cmd.Process.Kill() // SIGKILL
	<-exited
	time.Sleep(1500 * time.Millisecond) // the dead process's claim is stale after 1 s

	useRunE2E(t, goodReplies(t, ""))
	code, out, errs = runBriefCmd(t, "", "run", id)
	if code != 0 {
		t.Fatalf("resume: code=%d out=%q err=%q", code, out, errs)
	}
	requireProofLines(t, out)
	rep, err := report.Read(runDirOf(repo, id))
	if err != nil || rep.Status != "verified" || !rep.Resumed {
		t.Fatalf("resumed report = %+v, %v", rep, err)
	}
	tree, subjects := endState(t, repo)
	if tree != wantTree {
		t.Errorf("final tree %s, uninterrupted %s", tree, wantTree)
	}
	sort.Strings(subjects)
	sort.Strings(wantSubjects)
	if strings.Join(subjects, "\n") != strings.Join(wantSubjects, "\n") {
		t.Errorf("commits differ:\n%s\nvs\n%s", strings.Join(subjects, "\n"), strings.Join(wantSubjects, "\n"))
	}
	if au := strings.TrimSpace(gitOut(t, repo, "log", "--format=%an", "main")); strings.Count(au, "GopherMind") != 7 {
		t.Errorf("authors on main:\n%s\nwant 7 commits by GopherMind above the seed", au)
	}
	if hits := cliStoresHold(t, repo, cliCanary, out, errs, childOut.String()); len(hits) > 0 {
		t.Errorf("the secret is in %v", hits)
	}
}

// TestBriefRunE2EChild is the child of the test above: it runs `brief run` and
// parks at fn-hello's first reply until it is killed.
func TestBriefRunE2EChild(t *testing.T) {
	if os.Getenv("GM_CLI_E2E_CHILD") != "1" {
		return
	}
	old := vaultOptions
	vaultOptions = vault.Options{WorkFactor: 10}
	defer func() { vaultOptions = old }()
	marker := os.Getenv("GM_CLI_E2E_MARKER")
	good := goodReplies(t, "")
	useRunE2E(t, func(ctx context.Context, stage string, k int) (string, error) {
		if stage == "implement:fn-hello" && k == 1 {
			_ = os.WriteFile(marker, []byte("x"), 0o600)
			time.Sleep(10 * time.Minute)
		}
		return good(ctx, stage, k)
	})
	code, _, _ := runBriefCmd(t, "", "run", execGreeterID)
	t.Fatalf("the child ran to its end (exit %d) without reaching the kill point", code)
}
