//go:build !windows

package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	promptSentinel = "PROMPT-SENTINEL-d41d8cd9"
	envSentinel    = "ENV-SENTINEL-98f00b20"
)

// fakeRun describes what the fake cline script does after it records its inputs.
type fakeRun struct {
	fixture string // file under testdata printed to stdout
	stdout  string // inline stdout, used when fixture is empty
	stderr  string
	body    string // shell run before the output; $cwd is the -c value
	exit    int
}

type fakeEnv struct {
	cl       *Cline
	req      Request
	logs     string
	workdir  string
	settings string
	tmp      string // the TMPDIR the provider sees
}

func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func newFake(t *testing.T, fr fakeRun) *fakeEnv {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("OPENAI_API_KEY", envSentinel)
	t.Setenv("ANTHROPIC_API_KEY", envSentinel)
	t.Setenv("CLINE_DIR", envSentinel)

	logs := t.TempDir()
	workdir := t.TempDir()
	settings := t.TempDir()
	writeFiles(t, workdir, map[string]string{
		"go.mod":      "module example.com/m\n\ngo 1.25\n",
		"a.go":        "package m\n",
		"sub/b.go":    "package sub\n",
		".git/config": "[core]\n",
	})
	if err := os.Symlink("a.go", filepath.Join(workdir, "link.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settings, "providers.json"), []byte(`{"keys":"placeholder"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if fr.fixture != "" {
		abs, err := filepath.Abs(filepath.Join("testdata", fr.fixture))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&out, "cat %s\n", shq(abs))
	} else if fr.stdout != "" {
		fmt.Fprintf(&out, "printf '%%s\\n' %s\n", shq(fr.stdout))
	}
	if fr.stderr != "" {
		fmt.Fprintf(&out, "printf '%%s\\n' %s >&2\n", shq(fr.stderr))
	}
	script := fmt.Sprintf(`#!/bin/sh
L=%s
printf '%%s\n' "$@" > "$L/argv"
env | sed 's/=.*//' | sort > "$L/envnames"
cwd=; dd=; prev=
for a in "$@"; do
  [ "$prev" = "-c" ] && cwd=$a
  [ "$prev" = "--data-dir" ] && dd=$a
  prev=$a
done
printf '%%s\n' "$cwd" > "$L/cwd"
ls -l "$dd/settings" > "$L/ls"
cat "$dd/settings/global-settings.json" > "$L/gs"
(cd "$cwd" && find . | sort) > "$L/tree"
%s
%s
exit %d
`, shq(logs), fr.body, out.String(), fr.exit)
	bin := filepath.Join(t.TempDir(), "fakecline")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &fakeEnv{
		cl: NewCline(ClineConfig{Name: "cline", Binary: bin, ClineProvider: "cline-pass", SettingsDir: settings, Timeout: 20 * time.Second}),
		req: Request{
			Model:    "qwen3.7-max",
			Messages: []Message{{Role: RoleSystem, Content: "system " + promptSentinel}, {Role: RoleUser, Content: "user text"}},
			Workdir:  workdir, TargetFile: "target.go",
		},
		logs: logs, workdir: workdir, settings: settings, tmp: tmp,
	}
}

const writeTarget = `printf 'package m\n\nfunc F() {}\n' > "$cwd/target.go"`

func (f *fakeEnv) read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.logs, name))
	if err != nil {
		t.Fatalf("fake did not record %s: %v", name, err)
	}
	return string(b)
}

func (f *fakeEnv) noLeftovers(t *testing.T) {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(f.tmp, "gm-cline-*"))
	if len(m) != 0 {
		t.Errorf("scratch dir left behind: %d entries", len(m))
	}
}

func hashTree(t *testing.T, root string) string {
	t.Helper()
	h := sha256.New()
	var names []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		names = append(names, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	for _, p := range names {
		fi, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(h, "%s %v\n", p, fi.Mode())
		switch {
		case fi.Mode().IsRegular():
			b, _ := os.ReadFile(p)
			h.Write(b)
		case fi.Mode()&fs.ModeSymlink != 0:
			l, _ := os.Readlink(p)
			h.Write([]byte(l))
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func hasPair(lines []string, a, b string) bool {
	for i := 0; i+1 < len(lines); i++ {
		if lines[i] == a && lines[i+1] == b {
			return true
		}
	}
	return false
}

func TestClineSuccess(t *testing.T) {
	f := newFake(t, fakeRun{fixture: "cline_run_ok.jsonl", body: writeTarget})
	resp, err := f.cl.Complete(context.Background(), f.req)
	if err != nil {
		t.Fatal(err)
	}
	want := "```go\npackage m\n\nfunc F() {}\n```"
	if resp.Text != want {
		t.Errorf("text = %q, want %q", resp.Text, want)
	}
	if resp.Usage.PromptTokens != 19120 || resp.Usage.CompletionTokens != 76 {
		t.Errorf("usage = %+v", resp.Usage)
	}
	if resp.Model != "qwen3.7-max" || resp.Duration <= 0 {
		t.Errorf("model %q duration %v", resp.Model, resp.Duration)
	}
	f.noLeftovers(t)
}

func TestClineArgv(t *testing.T) {
	f := newFake(t, fakeRun{fixture: "cline_run_ok.jsonl", body: writeTarget})
	if _, err := f.cl.Complete(context.Background(), f.req); err != nil {
		t.Fatal(err)
	}
	argv := f.read(t, "argv")
	lines := strings.Split(argv, "\n")
	cwd := strings.TrimSpace(f.read(t, "cwd"))
	if lines[0] != "--json" {
		t.Errorf("first arg %q", lines[0])
	}
	for _, p := range [][2]string{{"--auto-approve", "true"}, {"-P", "cline-pass"}, {"-m", "qwen3.7-max"}, {"-c", cwd}, {"-t", "20"}} {
		if !hasPair(lines, p[0], p[1]) {
			t.Errorf("argv lacks %s %s", p[0], p[1])
		}
	}
	if !strings.Contains(argv, "\n--data-dir\n") {
		t.Error("argv lacks --data-dir")
	}
	if cwd == f.workdir || !strings.HasPrefix(cwd, f.tmp) {
		t.Errorf("-c %q is not a scratch dir under %q", cwd, f.tmp)
	}
	if !strings.HasSuffix(strings.TrimRight(argv, "\n"), fmt.Sprintf(clineTrailer, "target.go")) {
		t.Error("prompt does not end with the trailer")
	}
	if !strings.Contains(argv, "system "+promptSentinel+"\n\nuser text") {
		t.Error("prompt does not carry the messages in order")
	}
}

func TestClineEnvIsAllowlisted(t *testing.T) {
	f := newFake(t, fakeRun{fixture: "cline_run_ok.jsonl", body: writeTarget})
	if _, err := f.cl.Complete(context.Background(), f.req); err != nil {
		t.Fatal(err)
	}
	names := "\n" + f.read(t, "envnames")
	for _, want := range []string{"PATH", "HOME", "TMPDIR"} {
		if !strings.Contains(names, "\n"+want+"\n") {
			t.Errorf("child lacks %s", want)
		}
	}
	for _, bad := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "CLINE_DIR"} {
		if strings.Contains(names, "\n"+bad+"\n") {
			t.Errorf("child received %s", bad)
		}
	}
}

func TestClineDataDir(t *testing.T) {
	f := newFake(t, fakeRun{fixture: "cline_run_ok.jsonl", body: writeTarget})
	if _, err := f.cl.Complete(context.Background(), f.req); err != nil {
		t.Fatal(err)
	}
	if gs := f.read(t, "gs"); strings.TrimSpace(gs) != `{"autoUpdateEnabled":false,"telemetryOptOut":true,"planActMode":"act"}` {
		t.Errorf("global-settings.json = %q", gs)
	}
	ls := f.read(t, "ls")
	if !strings.Contains(ls, "providers.json -> "+filepath.Join(f.settings, "providers.json")) {
		t.Errorf("providers.json is not a symlink to the settings file: %q", ls)
	}
}

func TestClineMissingProviders(t *testing.T) {
	f := newFake(t, fakeRun{fixture: "cline_run_ok.jsonl", body: writeTarget})
	if err := os.Remove(filepath.Join(f.settings, "providers.json")); err != nil {
		t.Fatal(err)
	}
	_, err := f.cl.Complete(context.Background(), f.req)
	var ea ErrAuth
	if !errors.As(err, &ea) || ea.Provider != "cline" {
		t.Fatalf("err = %v, want ErrAuth", err)
	}
	f.noLeftovers(t)
}

func TestClineAuthOutcomes(t *testing.T) {
	for _, fixture := range []string{"cline_balance.jsonl", "cline_unauthorized.jsonl"} {
		f := newFake(t, fakeRun{fixture: fixture, exit: 1})
		_, err := f.cl.Complete(context.Background(), f.req)
		var ea ErrAuth
		if !errors.As(err, &ea) {
			t.Errorf("%s: err = %v, want ErrAuth", fixture, err)
		}
		f.noLeftovers(t)
	}
}

func TestClineRateLimited(t *testing.T) {
	out := `{"type":"run_result","finishReason":"error","text":"Error: 429 Too Many Requests","usage":{"inputTokens":0,"outputTokens":0}}`
	f := newFake(t, fakeRun{stdout: out, exit: 1})
	_, err := f.cl.Complete(context.Background(), f.req)
	var er ErrRateLimited
	if !errors.As(err, &er) || er.RetryAfter != 0 {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
}

func TestClineStderrError(t *testing.T) {
	f := newFake(t, fakeRun{stderr: `{"type":"error","message":"invalid API key"}`, exit: 1})
	_, err := f.cl.Complete(context.Background(), f.req)
	var ea ErrAuth
	if !errors.As(err, &ea) {
		t.Fatalf("err = %v, want ErrAuth", err)
	}
}

func TestClineTargetNotWritten(t *testing.T) {
	f := newFake(t, fakeRun{fixture: "cline_planmode.jsonl"})
	_, err := f.cl.Complete(context.Background(), f.req)
	var et ErrTransient
	if !errors.As(err, &et) || !strings.Contains(err.Error(), "the target file was not written") {
		t.Fatalf("err = %v", err)
	}
	f.noLeftovers(t)
}

func TestClineTargetEmptyOrNotRegular(t *testing.T) {
	for name, body := range map[string]string{
		"empty":   `printf ' \n\n' > "$cwd/target.go"`,
		"symlink": `ln -s /etc/hosts "$cwd/target.go"`,
		"dir":     `mkdir "$cwd/target.go"`,
	} {
		f := newFake(t, fakeRun{fixture: "cline_run_ok.jsonl", body: body})
		_, err := f.cl.Complete(context.Background(), f.req)
		var et ErrTransient
		if !errors.As(err, &et) || !strings.Contains(err.Error(), "the target file was not written") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestClineGarbageStillSucceeds(t *testing.T) {
	f := newFake(t, fakeRun{fixture: "cline_garbage.jsonl", body: writeTarget})
	resp, err := f.cl.Complete(context.Background(), f.req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Usage.PromptTokens != 500 || resp.Usage.CompletionTokens != 40 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

func TestClineNonZeroExitNoResult(t *testing.T) {
	f := newFake(t, fakeRun{stdout: "boom", exit: 3})
	_, err := f.cl.Complete(context.Background(), f.req)
	var et ErrTransient
	if !errors.As(err, &et) {
		t.Fatalf("err = %v, want ErrTransient", err)
	}
	f.noLeftovers(t)
}

func TestClineExitZeroNoResult(t *testing.T) {
	f := newFake(t, fakeRun{body: writeTarget})
	_, err := f.cl.Complete(context.Background(), f.req)
	var et ErrTransient
	if !errors.As(err, &et) {
		t.Fatalf("err = %v, want ErrTransient", err)
	}
}

func TestClineTimeoutKillsProcessGroup(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	f := newFake(t, fakeRun{body: fmt.Sprintf("(sleep 2; echo x > %s) &\nsleep 5", shq(marker))})
	f.cl.cfg.Timeout = 300 * time.Millisecond
	start := time.Now()
	_, err := f.cl.Complete(context.Background(), f.req)
	var et ErrTransient
	if !errors.As(err, &et) || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want timed out", err)
	}
	if d := time.Since(start); d > 4*time.Second {
		t.Errorf("took %v", d)
	}
	time.Sleep(2500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Error("a background child survived the timeout")
	}
	f.noLeftovers(t)
}

func TestClineCancel(t *testing.T) {
	f := newFake(t, fakeRun{body: "sleep 5"})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(300 * time.Millisecond); cancel() }()
	_, err := f.cl.Complete(ctx, f.req)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	f.noLeftovers(t)
}

func TestClineWorkdirUntouchedAndFiltered(t *testing.T) {
	f := newFake(t, fakeRun{fixture: "cline_run_ok.jsonl", body: writeTarget + "\nrm -f \"$cwd/a.go\"; echo changed > \"$cwd/go.mod\""})
	before := hashTree(t, f.workdir)
	if _, err := f.cl.Complete(context.Background(), f.req); err != nil {
		t.Fatal(err)
	}
	if after := hashTree(t, f.workdir); after != before {
		t.Error("the real workdir changed")
	}
	tree := f.read(t, "tree")
	for _, want := range []string{"./a.go", "./go.mod", "./sub/b.go"} {
		if !strings.Contains("\n"+tree, "\n"+want+"\n") {
			t.Errorf("scratch tree lacks %s", want)
		}
	}
	if strings.Contains(tree, ".git") || strings.Contains(tree, "link.go") {
		t.Errorf("scratch tree copied .git or a symlink:\n%s", tree)
	}
}

func TestClineGitFileNotCopied(t *testing.T) {
	f := newFake(t, fakeRun{fixture: "cline_run_ok.jsonl", body: writeTarget})
	writeFiles(t, f.workdir, map[string]string{"sub/.git": "gitdir: /elsewhere\n"})
	if _, err := f.cl.Complete(context.Background(), f.req); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.read(t, "tree"), ".git") {
		t.Error(".git file was copied")
	}
}

func TestClineCopyLimits(t *testing.T) {
	f := newFake(t, fakeRun{fixture: "cline_run_ok.jsonl", body: writeTarget})
	f.cl.cfg.MaxCopyBytes = 10
	_, err := f.cl.Complete(context.Background(), f.req)
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Errorf("bytes: err = %v", err)
	}
	f.noLeftovers(t)

	f = newFake(t, fakeRun{fixture: "cline_run_ok.jsonl", body: writeTarget})
	f.cl.cfg.MaxCopyFiles = 1
	_, err = f.cl.Complete(context.Background(), f.req)
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Errorf("files: err = %v", err)
	}
	if err != nil && strings.Contains(err.Error(), f.workdir) {
		t.Error("limit error names a path")
	}
	f.noLeftovers(t)
}

func TestClineRejectsBadRequests(t *testing.T) {
	for _, target := range []string{"", "../x", "/abs", `a\b`, "a/../b.go", "./a.go", "a//b.go", "."} {
		f := newFake(t, fakeRun{fixture: "cline_run_ok.jsonl", body: writeTarget})
		f.req.TargetFile = target
		_, err := f.cl.Complete(context.Background(), f.req)
		if err == nil {
			t.Errorf("target %q accepted", target)
		}
		var et ErrTransient
		if errors.As(err, &et) {
			t.Errorf("target %q: want a plain error, got ErrTransient", target)
		}
	}
	f := newFake(t, fakeRun{})
	f.req.Workdir = "relative/dir"
	if _, err := f.cl.Complete(context.Background(), f.req); err == nil {
		t.Error("relative workdir accepted")
	}
	f.req.Workdir = filepath.Join(f.workdir, "nope")
	if _, err := f.cl.Complete(context.Background(), f.req); err == nil {
		t.Error("missing workdir accepted")
	}
	f.req.Workdir = filepath.Join(f.workdir, "go.mod")
	if _, err := f.cl.Complete(context.Background(), f.req); err == nil {
		t.Error("file workdir accepted")
	}
	f.req.Workdir = f.workdir
	f.req.Model = ""
	if _, err := f.cl.Complete(context.Background(), f.req); err == nil {
		t.Error("empty model accepted")
	}
	f.noLeftovers(t)
}

func TestClineRejectsCodeFence(t *testing.T) {
	f := newFake(t, fakeRun{fixture: "cline_run_ok.jsonl", body: `printf 'package m\n// x\n` + "```" + `\nvar a = 1\n' > "$cwd/target.go"`})
	_, err := f.cl.Complete(context.Background(), f.req)
	var et ErrTransient
	if !errors.As(err, &et) || !strings.Contains(err.Error(), "code fence") {
		t.Fatalf("err = %v", err)
	}
}

func TestClineErrorsLeakNothing(t *testing.T) {
	home, _ := os.UserHomeDir()
	cases := []fakeRun{
		{stdout: "boom " + home + "/secret", exit: 2},
		{fixture: "cline_planmode.jsonl"},
		{fixture: "cline_balance.jsonl", exit: 1},
		{stdout: `{"type":"run_result","finishReason":"error","text":"` + strings.Repeat("x", 500) + `"}`, exit: 1},
	}
	for i, fr := range cases {
		f := newFake(t, fr)
		_, err := f.cl.Complete(context.Background(), f.req)
		if err == nil {
			t.Fatalf("case %d: no error", i)
		}
		msg := err.Error()
		if strings.Contains(msg, promptSentinel) || strings.Contains(msg, envSentinel) || strings.Contains(msg, "user text") {
			t.Errorf("case %d: error leaks: %q", i, msg)
		}
		if home != "" && home != "/" && strings.Contains(msg, home) {
			t.Errorf("case %d: error names the home dir: %q", i, msg)
		}
		if len(msg) > 300 || strings.Contains(msg, "\n") {
			t.Errorf("case %d: error not short and single line: %q", i, msg)
		}
	}
}

func TestClineNameAndModels(t *testing.T) {
	c := NewCline(ClineConfig{Name: "cline", Models: []ModelInfo{{ID: "m", ContextTokens: 1}}})
	if c.Name() != "cline" || len(c.Models()) != 1 {
		t.Error("Name or Models wrong")
	}
}

func TestClineSettingsDirExpandsTilde(t *testing.T) {
	for _, tc := range []struct{ dir, sub string }{{"~/cfg", "cfg"}, {"~", ""}} {
		f := newFake(t, fakeRun{fixture: "cline_run_ok.jsonl", body: writeTarget})
		home := t.TempDir()
		t.Setenv("HOME", home)
		if err := os.MkdirAll(filepath.Join(home, tc.sub), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, tc.sub, "providers.json"), []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
		f.cl = NewCline(ClineConfig{Name: "cline", Binary: f.cl.cfg.Binary, ClineProvider: "cline-pass", SettingsDir: tc.dir, Timeout: 20 * time.Second})
		if _, err := f.cl.Complete(context.Background(), f.req); err != nil {
			t.Errorf("SettingsDir %q: %v", tc.dir, err)
		}
	}
}

func TestClineGophermindNotCopied(t *testing.T) {
	f := newFake(t, fakeRun{fixture: "cline_run_ok.jsonl", body: writeTarget})
	writeFiles(t, f.workdir, map[string]string{
		".gophermind/run1/_state/x.json": "{}\n",
		".gophermind/calls.jsonl":        "{}\n",
		"sub/deeper/.gophermind":         "a file, not a directory\n",
		"sub/deeper/keep.go":             "package deeper\n",
	})
	before := hashTree(t, f.workdir)
	if _, err := f.cl.Complete(context.Background(), f.req); err != nil {
		t.Fatal(err)
	}
	tree := f.read(t, "tree")
	if strings.Contains(tree, ".gophermind") {
		t.Errorf("scratch tree copied a .gophermind entry:\n%s", tree)
	}
	for _, want := range []string{"./a.go", "./sub/b.go", "./sub/deeper/keep.go"} {
		if !strings.Contains("\n"+tree, "\n"+want+"\n") {
			t.Errorf("scratch tree lacks %s", want)
		}
	}
	if after := hashTree(t, f.workdir); after != before {
		t.Error("the real workdir changed")
	}
}

func TestClineOmitFiles(t *testing.T) {
	f := newFake(t, fakeRun{fixture: "cline_run_ok.jsonl", body: writeTarget})
	writeFiles(t, f.workdir, map[string]string{
		"sub/zz_gm_stub_mine.go":  "package sub\n",
		"sub/zz_gm_stub_other.go": "package sub\n",
	})
	f.req.OmitFiles = []string{"sub/zz_gm_stub_mine.go", "not/there.go"}
	before := hashTree(t, f.workdir)
	if _, err := f.cl.Complete(context.Background(), f.req); err != nil {
		t.Fatal(err)
	}
	tree := f.read(t, "tree")
	if strings.Contains(tree, "zz_gm_stub_mine.go") {
		t.Errorf("the omitted file was copied:\n%s", tree)
	}
	for _, want := range []string{"./sub/zz_gm_stub_other.go", "./sub/b.go", "./a.go"} {
		if !strings.Contains("\n"+tree, "\n"+want+"\n") {
			t.Errorf("scratch tree lacks %s", want)
		}
	}
	if after := hashTree(t, f.workdir); after != before {
		t.Error("the real workdir changed")
	}
}

func TestClineOmitFilesMayEqualTarget(t *testing.T) {
	f := newFake(t, fakeRun{fixture: "cline_run_ok.jsonl", body: writeTarget})
	writeFiles(t, f.workdir, map[string]string{"target.go": "package m\n"})
	f.req.OmitFiles = []string{"target.go"}
	if _, err := f.cl.Complete(context.Background(), f.req); err != nil {
		t.Fatal(err)
	}
}

func TestClineRejectsBadOmitFiles(t *testing.T) {
	for _, omit := range []string{"", "../x", "/abs", `a\b`, "a/../b.go", "./a.go", "."} {
		f := newFake(t, fakeRun{fixture: "cline_run_ok.jsonl", body: writeTarget})
		f.req.OmitFiles = []string{"sub/b.go", omit}
		_, err := f.cl.Complete(context.Background(), f.req)
		if err == nil {
			t.Errorf("omit %q accepted", omit)
		}
		var et ErrTransient
		if errors.As(err, &et) {
			t.Errorf("omit %q: want a plain error, got ErrTransient", omit)
		}
		f.noLeftovers(t)
	}
}
