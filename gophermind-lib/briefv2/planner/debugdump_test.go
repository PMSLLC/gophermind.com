package planner_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
)

func dumpFiles(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

func captureDumpNotice(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	restore := planner.SetDebugOut(&buf)
	t.Cleanup(restore)
	return &buf
}

func TestDebugDumpIsOffByDefault(t *testing.T) {
	dir := t.TempDir()
	buf := captureDumpNotice(t)
	t.Setenv(planner.DebugDumpEnv, "")
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if n := len(dumpFiles(t, dir)); n != 0 || buf.Len() != 0 {
		t.Errorf("files %d, notice %q: nothing may be written or said when the variable is unset", n, buf.String())
	}
}

func TestDebugDumpWritesRequestsAndRepliesWhenAsked(t *testing.T) {
	plain := newRig(t, approving())
	plain.mustPlan(planner.Options{StopAfter: "contract"})
	want := string(plain.read("contracts.json"))

	dir := t.TempDir()
	buf := captureDumpNotice(t)
	t.Setenv(planner.DebugDumpEnv, dir)
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if got := strings.Count(buf.String(), "debug dump enabled"); got != 1 {
		t.Errorf("notice %q, want one line", buf.String())
	}
	files := dumpFiles(t, dir)
	var haveReq, haveReply bool
	for _, f := range files {
		fi, err := os.Stat(filepath.Join(dir, f))
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %v err %v, want 0600", f, fi.Mode().Perm(), err)
		}
		haveReq = haveReq || strings.HasSuffix(f, ".request.json")
		haveReply = haveReply || strings.HasSuffix(f, ".reply.txt")
	}
	if !haveReq || !haveReply || len(files) < 8 {
		t.Errorf("files = %v", files)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "contract_types-1.reply.txt"))
	if err != nil || !strings.Contains(string(raw), "fn-name-error-error") {
		t.Errorf("reply file: %v %.80s", err, raw)
	}
	if string(g.read("contracts.json")) != want {
		t.Error("the dump changed the plan")
	}
	// Nothing about it reaches the stores or the events.
	for _, e := range g.sink.Events() {
		if strings.Contains(e.Message, dir) {
			t.Errorf("event mentions the dump dir: %v", e)
		}
	}
}

func TestDebugDumpIsRefusedInsideTheRunOrTheRepoOrTheConfigDir(t *testing.T) {
	for _, name := range []string{"run dir", "repo", "config dir"} {
		t.Run(name, func(t *testing.T) {
			g := newRig(t, approving())
			g.mustPlan(planner.Options{StopAfter: "load"})
			base := map[string]string{"run dir": g.runDir, "repo": g.repo, "config dir": os.Getenv("GOPHERMIND_CONFIG_DIR")}[name]
			dir := filepath.Join(base, "dump")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			buf := captureDumpNotice(t)
			t.Setenv(planner.DebugDumpEnv, dir)
			g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "contract"})
			if n := len(dumpFiles(t, dir)); n != 0 {
				t.Errorf("%d files written inside the %s", n, name)
			}
			if !strings.Contains(buf.String(), "ignored") || strings.Contains(buf.String(), "debug dump enabled") {
				t.Errorf("notice %q, want a warning that it is ignored", buf.String())
			}
		})
	}
}
