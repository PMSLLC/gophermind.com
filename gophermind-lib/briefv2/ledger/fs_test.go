package ledger_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/runfs"
)

func newLedger(t *testing.T) (*ledger.FS, string) {
	t.Helper()
	dir := t.TempDir()
	return ledger.NewFS(runfs.Fixed(dir)), dir
}

func call(run, stage, prov, model string, o ledger.Outcome) *ledger.Call {
	return &ledger.Call{
		RunID: run, At: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC), Stage: stage, Scope: "brief", Tier: "strong",
		ChainPos: 1, Provider: prov, ModelRequested: model, ModelServed: model,
		PromptTokens: 100, CompletionTokens: 20, DurationMS: 1500, Outcome: o,
	}
}

func TestRecordAssignsIDAndListReturnsFields(t *testing.T) {
	l, _ := newLedger(t)
	ctx := context.Background()
	c := call("r", "clarify", "mini", "qwen", ledger.OutcomeOK)
	c.NodeID, c.Revision, c.PromptBytes, c.PromptSHA256 = "fn-a", 2, 400, "abc"
	c.TaskType, c.NodeClass = "testwrite", "validation"
	if err := l.Record(ctx, c); err != nil || c.ID == 0 {
		t.Fatalf("Record: id=%d err=%v", c.ID, err)
	}
	got, err := l.List(ctx, "r", ledger.Filter{})
	if err != nil || len(got) != 1 {
		t.Fatalf("List = %d rows, %v", len(got), err)
	}
	g := got[0]
	if g.ID != c.ID || g.Stage != "clarify" || g.NodeID != "fn-a" || g.Revision != 2 || g.Provider != "mini" ||
		g.PromptTokens != 100 || g.CompletionTokens != 20 || g.DurationMS != 1500 || g.Outcome != ledger.OutcomeOK ||
		g.PromptBytes != 400 || g.PromptSHA256 != "abc" || !g.At.Equal(c.At) ||
		g.TaskType != "testwrite" || g.NodeClass != "validation" {
		t.Errorf("row lost data: %+v", g)
	}
}

func TestRecordRejectsIncompleteRows(t *testing.T) {
	l, _ := newLedger(t)
	for _, c := range []*ledger.Call{
		{Stage: "s", Provider: "p", Outcome: ledger.OutcomeOK},
		{RunID: "r", Provider: "p", Outcome: ledger.OutcomeOK},
		{RunID: "r", Stage: "s", Outcome: ledger.OutcomeOK},
		{RunID: "r", Stage: "s", Provider: "p"},
	} {
		if err := l.Record(context.Background(), c); err == nil {
			t.Errorf("Record accepted an incomplete row: %+v", c)
		}
	}
}

func TestAmendChangesTheOutcome(t *testing.T) {
	l, _ := newLedger(t)
	ctx := context.Background()
	c := call("r", "contract", "mini", "qwen", ledger.OutcomeOK)
	if err := l.Record(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := l.Amend(ctx, c.RunID, c.ID, ledger.OutcomeMalformed, "component x is not a feature"); err != nil {
		t.Fatal(err)
	}
	got, _ := l.List(ctx, "r", ledger.Filter{})
	if got[0].Outcome != ledger.OutcomeMalformed || got[0].ErrorKind != "component x is not a feature" {
		t.Errorf("after amend: %+v", got[0])
	}
	if err := l.Amend(ctx, "run-x", 9999, ledger.OutcomeError, ""); err == nil {
		t.Error("amending an unknown row should fail")
	}
}

func TestListFilters(t *testing.T) {
	l, _ := newLedger(t)
	ctx := context.Background()
	for _, c := range []*ledger.Call{
		call("r", "clarify", "mini", "qwen", ledger.OutcomeOK),
		call("r", "testwrite:fn-a", "kilo", "auto", ledger.OutcomeRateLimited),
		call("r", "testwrite:fn-a", "mini", "qwen", ledger.OutcomeOK),
		call("other", "clarify", "mini", "qwen", ledger.OutcomeOK),
	} {
		if strings.HasPrefix(c.Stage, "testwrite") {
			c.NodeID = "fn-a"
		}
		if err := l.Record(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	count := func(f ledger.Filter) int { rows, _ := l.List(ctx, "r", f); return len(rows) }
	if n := count(ledger.Filter{}); n != 3 {
		t.Errorf("all rows of run r = %d, want 3", n)
	}
	if n := count(ledger.Filter{NodeID: "fn-a"}); n != 2 {
		t.Errorf("node filter = %d, want 2", n)
	}
	if n := count(ledger.Filter{Provider: "kilo"}); n != 1 {
		t.Errorf("provider filter = %d, want 1", n)
	}
	if n := count(ledger.Filter{Outcome: ledger.OutcomeOK}); n != 2 {
		t.Errorf("outcome filter = %d, want 2", n)
	}
	if n := count(ledger.Filter{Stage: "clarify"}); n != 1 {
		t.Errorf("stage filter = %d, want 1", n)
	}
}

func TestSummaryMatchesAHandCount(t *testing.T) {
	l, _ := newLedger(t)
	ctx := context.Background()
	rows := []struct {
		prov, model string
		o           ledger.Outcome
		ms          int64
	}{
		{"mini", "qwen", ledger.OutcomeOK, 1000},
		{"mini", "qwen", ledger.OutcomeOK, 3000},
		{"mini", "qwen", ledger.OutcomeMalformed, 500},
		{"kilo", "kilo-auto/free", ledger.OutcomeRateLimited, 100},
	}
	for _, r := range rows {
		c := call("r", "s", r.prov, r.model, r.o)
		c.DurationMS = r.ms
		if err := l.Record(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	// A reply served by a different model than requested is summarized under the served name.
	c := call("r", "s", "kilo", "kilo-auto/free", ledger.OutcomeOK)
	c.ModelServed = "stealth/space-bunny-alpha"
	if err := l.Record(ctx, c); err != nil {
		t.Fatal(err)
	}
	sum, err := l.Summary(ctx, "r")
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]ledger.ModelSummary{}
	for _, s := range sum {
		by[s.Provider+"/"+s.Model] = s
	}
	mini := by["mini/qwen"]
	if mini.Calls != 3 || mini.PromptTokens != 300 || mini.CompletionTokens != 60 || mini.TotalMS != 4500 ||
		mini.Outcomes[ledger.OutcomeOK] != 2 || mini.Outcomes[ledger.OutcomeMalformed] != 1 {
		t.Errorf("mini/qwen = %+v", mini)
	}
	if k := by["kilo/kilo-auto/free"]; k.Calls != 1 || k.Outcomes[ledger.OutcomeRateLimited] != 1 {
		t.Errorf("kilo/kilo-auto/free = %+v", k)
	}
	if k := by["kilo/stealth/space-bunny-alpha"]; k.Calls != 1 {
		t.Errorf("served model not summarized separately: %+v", by)
	}
	if len(sum) != 3 {
		t.Errorf("summary has %d groups, want 3", len(sum))
	}
}

// The same model doing different kinds of work is summarized once per kind,
// so its results can be compared by task type and by class of function.
func TestSummaryGroupsByTaskTypeAndNodeClass(t *testing.T) {
	l, _ := newLedger(t)
	ctx := context.Background()
	rows := []struct {
		task, class string
		o           ledger.Outcome
	}{
		{"contract", "", ledger.OutcomeOK},
		{"testwrite", "pure", ledger.OutcomeOK},
		{"testwrite", "pure", ledger.OutcomeMalformed},
		{"testwrite", "handler", ledger.OutcomeOK},
	}
	for _, r := range rows {
		c := call("r", r.task, "mini", "qwen", r.o)
		c.TaskType, c.NodeClass = r.task, r.class
		if err := l.Record(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	sum, err := l.Summary(ctx, "r")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range sum {
		got = append(got, fmt.Sprintf("%s|%s|%s/%s|%d", s.TaskType, s.NodeClass, s.Provider, s.Model, s.Calls))
	}
	want := []string{"contract||mini/qwen|1", "testwrite|handler|mini/qwen|1", "testwrite|pure|mini/qwen|2"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("summary = %v\nwant     %v", got, want)
	}
	if sum[2].Outcomes[ledger.OutcomeOK] != 1 || sum[2].Outcomes[ledger.OutcomeMalformed] != 1 {
		t.Errorf("testwrite/pure outcomes = %v", sum[2].Outcomes)
	}
	only, err := l.List(ctx, "r", ledger.Filter{TaskType: "testwrite"})
	if err != nil || len(only) != 3 {
		t.Errorf("task type filter = %d rows, %v; want 3", len(only), err)
	}
}

func TestNoPromptOrReplyTextIsEverStored(t *testing.T) {
	l, dir := newLedger(t)
	ctx := context.Background()
	const canary = "CANARY-prompt-text-7f3a91"
	n, sum := ledger.Digest([]byte(canary))
	if n != len(canary) || len(sum) != 64 {
		t.Fatalf("Digest = %d %q", n, sum)
	}
	c := call("r", "clarify", "mini", "qwen", ledger.OutcomeOK)
	c.PromptBytes, c.PromptSHA256, c.ResponseBytes, c.ResponseSHA256 = n, sum, n, sum
	if err := l.Record(ctx, c); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "_state", "calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(raw)), strings.ToLower(canary)) {
		t.Errorf("calls.jsonl contains the prompt text")
	}
	if !strings.Contains(string(raw), sum) || !strings.Contains(string(raw), fmt.Sprintf(`"prompt_bytes":%d`, n)) ||
		!strings.Contains(string(raw), fmt.Sprintf(`"response_bytes":%d`, n)) {
		t.Errorf("calls.jsonl lacks the SHA-256 or sizes: %s", raw)
	}
}

func TestFSIDsAreSequentialAndSurviveAReopen(t *testing.T) {
	dir := t.TempDir()
	l := ledger.NewFS(runfs.Fixed(dir))
	ctx := context.Background()
	mk := func() *ledger.Call {
		return &ledger.Call{RunID: "r", Stage: "clarify", Provider: "mini", Outcome: ledger.OutcomeOK, TaskType: "clarify"}
	}
	a, b := mk(), mk()
	if err := l.Record(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := l.Record(ctx, b); err != nil {
		t.Fatal(err)
	}
	if a.ID != 1 || b.ID != 2 {
		t.Fatalf("ids = %d, %d; want 1, 2", a.ID, b.ID)
	}
	c := mk()
	if err := ledger.NewFS(runfs.Fixed(dir)).Record(ctx, c); err != nil || c.ID != 3 {
		t.Fatalf("after a reopen: id = %d, %v; want 3", c.ID, err)
	}
}

func TestFSConcurrentRecordsGetDistinctIDs(t *testing.T) {
	l := ledger.NewFS(runfs.Fixed(t.TempDir()))
	ctx := context.Background()
	var wg sync.WaitGroup
	ids := make(chan int64, 50)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := &ledger.Call{RunID: "r", Stage: "s", Provider: "p", Outcome: ledger.OutcomeOK}
			if err := l.Record(ctx, c); err != nil {
				t.Error(err)
				return
			}
			ids <- c.ID
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[int64]bool{}
	for id := range ids {
		if seen[id] {
			t.Fatalf("id %d handed out twice", id)
		}
		seen[id] = true
	}
	rows, err := l.List(ctx, "r", ledger.Filter{})
	if err != nil || len(rows) != 50 {
		t.Fatalf("List = %d rows, %v; want 50", len(rows), err)
	}
}

func TestFSAmendIsAFoldedLineNotARewrite(t *testing.T) {
	dir := t.TempDir()
	l := ledger.NewFS(runfs.Fixed(dir))
	ctx := context.Background()
	c := &ledger.Call{RunID: "r", Stage: "s", Provider: "p", Outcome: ledger.OutcomeOK}
	if err := l.Record(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := l.Amend(ctx, "r", c.ID, ledger.OutcomeMalformed, "malformed"); err != nil {
		t.Fatal(err)
	}
	rows, _ := l.List(ctx, "r", ledger.Filter{})
	if len(rows) != 1 || rows[0].Outcome != ledger.OutcomeMalformed || rows[0].ErrorKind != "malformed" {
		t.Fatalf("rows = %+v", rows)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "_state", "calls.jsonl"))
	if n := strings.Count(string(raw), "\n"); n != 2 {
		t.Errorf("calls.jsonl has %d lines, want 2 (the call and its amend): the file is append-only", n)
	}
	if err := l.Amend(ctx, "r", 999, ledger.OutcomeError, ""); err == nil {
		t.Error("amending a call that does not exist must fail")
	}
}

func TestFSReadsDoNotWrite(t *testing.T) {
	dir := t.TempDir()
	l := ledger.NewFS(runfs.Fixed(dir))
	ctx := context.Background()
	if rows, err := l.List(ctx, "r", ledger.Filter{}); err != nil || len(rows) != 0 {
		t.Fatalf("List = %v, %v", rows, err)
	}
	if sum, err := l.Summary(ctx, "r"); err != nil || len(sum) != 0 {
		t.Fatalf("Summary = %v, %v", sum, err)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Errorf("reads created %d entries", len(ents))
	}
}
