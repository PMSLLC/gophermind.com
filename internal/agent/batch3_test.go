package agent

import (
	"strings"
	"testing"
	"time"

	"gophermind/internal/llm"
)

func TestParsePlan(t *testing.T) {
	out, err := parsePlan(`{"plan":[{"step":"do x"},{"step":"do y"}],"rationale":"because"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1. do x", "2. do y", "Rationale: because"} {
		if !strings.Contains(out, want) {
			t.Errorf("plan missing %q in:\n%s", want, out)
		}
	}
}

func TestSummarizeResult(t *testing.T) {
	if s, tr := SummarizeResult("short", 100); tr || s != "short" {
		t.Errorf("short text should pass through: %q %v", s, tr)
	}
	long := strings.Repeat("line\n", 100)
	s, tr := SummarizeResult(long, 20)
	if !tr || !strings.Contains(s, "truncated") {
		t.Errorf("long text should truncate: %q %v", s, tr)
	}
}

func TestGuardrailsCheck(t *testing.T) {
	g := Guardrails{MaxTokens: 100}
	if _, stop := g.check(UsageSnapshot{TotalTokens: 50}, 0); stop {
		t.Error("under token ceiling should not stop")
	}
	if msg, stop := g.check(UsageSnapshot{TotalTokens: 200}, 0); !stop || !strings.Contains(msg, "token ceiling") {
		t.Errorf("over token ceiling should stop: %q %v", msg, stop)
	}
	gd := Guardrails{MaxDuration: time.Second}
	if _, stop := gd.check(UsageSnapshot{}, 2*time.Second); !stop {
		t.Error("over duration ceiling should stop")
	}
}

func TestInstructionPriority(t *testing.T) {
	if got := InstructionPriority("sys", "proj", "user"); got != "user" {
		t.Errorf("user should win, got %q", got)
	}
	if got := InstructionPriority("sys", "proj", ""); got != "proj" {
		t.Errorf("project next, got %q", got)
	}
	if got := InstructionPriority("sys", "", ""); got != "sys" {
		t.Errorf("system fallback, got %q", got)
	}
}

func TestSelectivePruning(t *testing.T) {
	msgs := []llm.Message{{Content: "0"}, {Content: "1"}, {Content: "2"}}
	got := SelectivePruning(msgs, []int{1})
	if len(got) != 2 || got[0].Content != "0" || got[1].Content != "2" {
		t.Errorf("pruning index 1 = %+v", got)
	}
}

func TestEstimateBudget(t *testing.T) {
	msgs := []llm.Message{
		{Role: "system", Content: strings.Repeat("a", 40)},
		{Role: "user", Content: strings.Repeat("b", 40)},
		{Role: "tool", Content: strings.Repeat("c", 40)},
	}
	b := EstimateBudget(msgs, nil)
	if b.SystemTokens == 0 || b.HistoryTokens == 0 || b.ToolOutputTokens == 0 {
		t.Errorf("budget categories should be non-zero: %+v", b)
	}
}

func TestReplayRoundTrip(t *testing.T) {
	rec := NewReplayRecorder()
	rec.Record(Event{Type: "token", Text: "x"})
	rec.Record(Event{Type: "tool_call", Name: "read_file"})
	var b strings.Builder
	if err := rec.Write(&b); err != nil {
		t.Fatal(err)
	}
	events, err := ReplayReader(strings.NewReader(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Errorf("want 2 replay events, got %d", len(events))
	}
}

func TestBudgetState(t *testing.T) {
	bs := &budgetState{count: 8, limit: 10}
	if !bs.approaching() || bs.exhausted() {
		t.Error("8/10 should be approaching but not exhausted")
	}
	bs.count = 10
	if !bs.exhausted() {
		t.Error("10/10 should be exhausted")
	}
}

func TestCheckpointSnapshotRestore(t *testing.T) {
	a := New(nil, nil, 1, nil, nil)
	a.LoadHistory(strings.NewReader(
		`{"role":"system","content":"s"}` + "\n" + `{"role":"user","content":"u1"}` + "\n"))
	a.Snapshot("cp")
	// grow the conversation
	a.LoadHistory(strings.NewReader(
		`{"role":"system","content":"s"}` + "\n" + `{"role":"user","content":"u1"}` + "\n" + `{"role":"user","content":"u2"}` + "\n"))
	restored, ok := a.Restore("cp")
	if !ok {
		t.Fatal("checkpoint not found")
	}
	if len(restored) != 2 {
		t.Errorf("restored len = %d, want 2 (snapshot point)", len(restored))
	}
	if names := a.ListCheckpoints(); len(names) != 1 || names[0] != "cp" {
		t.Errorf("checkpoint names = %v", names)
	}
}

func TestReflectOnFailure(t *testing.T) {
	if out := NewReflection(false).ReflectOnFailure("read_file", "{}", "boom"); out != "" {
		t.Errorf("disabled reflection should be empty, got %q", out)
	}
	out := NewReflection(true).ReflectOnFailure("read_file", "{}", "boom")
	if out == "" {
		t.Error("enabled reflection should produce guidance")
	}
}

func TestDiffAccumulator(t *testing.T) {
	da := newDiffAccumulator()
	da.Add("f.go", "old", "new")
	if !strings.Contains(da.Preview(), "f.go") {
		t.Errorf("diff preview should mention the file: %q", da.Preview())
	}
}
