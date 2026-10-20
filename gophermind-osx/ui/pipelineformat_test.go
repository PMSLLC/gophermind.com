package ui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/runview"
)

func TestFormatNodeTree_GroupsByComponentWithStatusWaveModelAndAttempts(t *testing.T) {
	nodes := []runview.NodeSummary{
		{ID: "root", Kind: "root", Title: "R"},
		{ID: "greeting", Kind: "component", Title: "Greeting"},
		{ID: "fn-ok", Kind: "function", Parent: "greeting", Status: "verified", Wave: 0, ModelTier: "standard", AttemptCount: 2,
			LastAttempt: &runview.Attempt{Model: "qwen", Verdict: "pass"}},
		{ID: "fn-bad", Kind: "function", Parent: "greeting", Status: "failed", Wave: 1, AttemptCount: 3,
			LastAttempt: &runview.Attempt{Model: "qwen", Verdict: "fail", FailureReason: "test: TestBye failed"}},
		{ID: "fn-esc", Kind: "function", Parent: "greeting", Status: "escalated", Wave: 1},
		{ID: "fn-lost", Kind: "function", Parent: "gone", Status: "pending"},
	}
	lines := FormatNodeTree(nodes)
	text := strings.Join(lines, "\n")
	for _, want := range []string{
		"== Greeting", "[verified] fn-ok (wave 0, standard, 2 attempt(s))", "last: qwen pass",
		"[failed] fn-bad (wave 1, 3 attempt(s))", "last: qwen fail - test: TestBye failed",
		"[escalated] fn-esc", "fn-lost",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("tree lacks %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "fn-ok") > strings.Index(text, "fn-bad") {
		t.Errorf("nodes must keep the order given:\n%s", text)
	}
	if strings.Contains(text, "root") {
		t.Errorf("the root node is not a tree row:\n%s", text)
	}
}

func TestFormatRunHeader(t *testing.T) {
	if got := FormatRunHeader(nil); got != "No run yet" {
		t.Errorf("nil run = %q", got)
	}
	got := FormatRunHeader(&runview.RunSummary{RunID: "gm-2026-10-08-001", Status: "failed", StopReason: "leaf_failed", Waves: 2,
		Counts: runview.Counts{Nodes: 3}, Coverage: runview.CoverageCount{Covered: 1, Total: 2}})
	for _, want := range []string{"gm-2026-10-08-001", "failed (leaf_failed)", "waves 2", "nodes 3", "coverage 1/2"} {
		if !strings.Contains(got, want) {
			t.Errorf("header lacks %q: %q", want, got)
		}
	}
}

func TestFormatReport(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "gophermind-lib", "runview", "testdata", "golden", "report-partial.json"))
	if err != nil {
		t.Fatal(err)
	}
	var r runview.ReportBody
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	got := FormatReport(r)
	for _, want := range []string{"building", "(running)", "requirements 1/1", "implement", "qwen3.6"} {
		if !strings.Contains(got, want) {
			t.Errorf("report lacks %q:\n%s", want, got)
		}
	}
	r.Complete = nil
	r.Status = "failed"
	r.Failures = []string{"fn-bye: test: TestBye"}
	got = FormatReport(r)
	if strings.Contains(got, "(running)") || !strings.Contains(got, "fn-bye: test: TestBye") {
		t.Errorf("final report text wrong:\n%s", got)
	}
}

func TestFormatNodeDetail(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "gophermind-lib", "runview", "testdata", "golden", "node-fn-bye.json"))
	if err != nil {
		t.Fatal(err)
	}
	var nb runview.NodeBody
	if err := json.Unmarshal(raw, &nb); err != nil {
		t.Fatal(err)
	}
	got := FormatNodeDetail(nb, "--- FAIL: TestBye\n    want bye")
	for _, want := range []string{"Function fn-bye", "in_progress", "1. qwen3.6 fail", "saved: attempt 1: reply.txt, check-output.txt, attempt.json", "--- FAIL: TestBye"} {
		if !strings.Contains(got, want) {
			t.Errorf("detail lacks %q:\n%s", want, got)
		}
	}
}

type fakeSource struct {
	body      runview.NodeBody
	artifacts map[string]string
	asked     []string
	err       error
}

func (f *fakeSource) PipelineNode(ctx context.Context, run, id string) (runview.NodeBody, error) {
	return f.body, f.err
}

func (f *fakeSource) PipelineArtifact(ctx context.Context, run, id string, attempt int, file string) (string, error) {
	f.asked = append(f.asked, file)
	if t, ok := f.artifacts[file]; ok {
		return t, nil
	}
	return "", errors.New("missing")
}

func TestLoadNodeDetailFetchesTheNewestCheckOutput(t *testing.T) {
	src := &fakeSource{
		body: runview.NodeBody{
			Node:      json.RawMessage(`{"id":"fn-x","title":"X"}`),
			Artifacts: []runview.ArtifactEntry{{Attempt: 1, Files: []string{"check-output.txt"}}, {Attempt: 2, Files: []string{"reply.txt", "check-output.txt"}}},
		},
		artifacts: map[string]string{"check-output.txt": "boom"},
	}
	got, err := LoadNodeDetail(context.Background(), src, "r", "fn-x")
	if err != nil || !strings.Contains(got, "boom") || len(src.asked) != 1 {
		t.Fatalf("detail = %q, %v, asked %v", got, err, src.asked)
	}
	src.body.Artifacts = nil
	src.asked = nil
	got, err = LoadNodeDetail(context.Background(), src, "r", "fn-x")
	if err != nil || len(src.asked) != 0 || strings.Contains(got, "boom") {
		t.Errorf("with no artifacts nothing is fetched: %q %v %v", got, err, src.asked)
	}
	src.err = errors.New("down")
	if _, err := LoadNodeDetail(context.Background(), src, "r", "fn-x"); err == nil {
		t.Error("a node fetch error must be returned")
	}
}
