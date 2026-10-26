package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBriefInitRepoMakesARepoWithOneCommit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gm-2026-10-26-001")
	var errw bytes.Buffer
	if rc := briefInitRepo([]string{dir}, &errw); rc != 0 {
		t.Fatalf("rc %d: %s", rc, errw.String())
	}
	out, err := exec.Command("git", "-C", dir, "log", "--format=%s", "main").Output()
	if err != nil || strings.TrimSpace(string(out)) != "init" {
		t.Fatalf("log %q: %v", out, err)
	}
	if st, _ := exec.Command("git", "-C", dir, "status", "--short").Output(); len(st) != 0 {
		t.Errorf("new repo not clean: %s", st)
	}
	if rc := briefInitRepo([]string{dir}, &errw); rc != 1 || !strings.Contains(errw.String(), "already exists") {
		t.Errorf("second call rc %d: %s", rc, errw.String())
	}
	if rc := briefInitRepo(nil, &errw); rc != 1 {
		t.Errorf("no args rc %d", rc)
	}
}
