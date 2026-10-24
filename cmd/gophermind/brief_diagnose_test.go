package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func diagnoseRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	dir := filepath.Join(repo, ".gophermind", "gm-2026-10-24-009")
	if err := os.MkdirAll(filepath.Join(dir, "_state"), 0o700); err != nil {
		t.Fatal(err)
	}
	line := `{"kind":"call","id":1,"run_id":"gm-2026-10-24-009","at":"2026-10-24T10:00:10Z","stage":"clarify","task_type":"clarify","provider":"mini","model_requested":"m1","model_served":"m1","prompt_tokens":10,"completion_tokens":5,"duration_ms":1000,"outcome":"ok"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "_state", "calls.jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestBriefDiagnoseWithRepo(t *testing.T) {
	repo := diagnoseRepo(t)
	var out, errw bytes.Buffer
	if code := runBrief([]string{"diagnose", "gm-2026-10-24-009", "--repo", repo}, nil, &out, &errw); code != 0 {
		t.Fatalf("exit %d: %s", code, errw.String())
	}
	for _, want := range []string{"run gm-2026-10-24-009", "PHASES", "clarify", "PROBLEMS", "LIKELY CAUSE"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	if code := runBrief([]string{"diagnose", "--json", "--repo", repo, "gm-2026-10-24-009"}, nil, &out, &errw); code != 0 {
		t.Fatalf("json exit %d: %s", code, errw.String())
	}
	var d map[string]any
	if err := json.Unmarshal(out.Bytes(), &d); err != nil || d["run_id"] != "gm-2026-10-24-009" || d["phases"] == nil {
		t.Errorf("json = %v, %v", d, err)
	}
}

func TestBriefDiagnoseErrors(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	for _, args := range [][]string{
		{"diagnose"},
		{"diagnose", "gm-2026-10-24-009"},                        // no record, no --repo
		{"diagnose", "../x", "--repo", t.TempDir()},              // not a run id
		{"diagnose", "gm-2026-10-24-009", "--repo", t.TempDir()}, // no folder
	} {
		var out, errw bytes.Buffer
		if code := runBrief(args, nil, &out, &errw); code == 0 {
			t.Errorf("%v: exit 0, want an error", args)
		}
	}
}
