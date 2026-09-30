package ledger_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/db"
	"gophermind/gophermind-lib/briefv2/ledger"
)

func newLedger(t *testing.T) (*ledger.SQLite, *sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bb.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return ledger.NewSQLite(d), d, path
}

func call(run, stage, prov, model string, o ledger.Outcome) *ledger.Call {
	return &ledger.Call{
		RunID: run, At: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC), Stage: stage, Scope: "brief", Tier: "strong",
		ChainPos: 1, Provider: prov, ModelRequested: model, ModelServed: model,
		PromptTokens: 100, CompletionTokens: 20, DurationMS: 1500, Outcome: o,
	}
}

func TestRecordAssignsIDAndListReturnsFields(t *testing.T) {
	l, _, _ := newLedger(t)
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
	l, _, _ := newLedger(t)
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
	l, _, _ := newLedger(t)
	ctx := context.Background()
	c := call("r", "contract", "mini", "qwen", ledger.OutcomeOK)
	if err := l.Record(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := l.Amend(ctx, c.ID, ledger.OutcomeMalformed, "component x is not a feature"); err != nil {
		t.Fatal(err)
	}
	got, _ := l.List(ctx, "r", ledger.Filter{})
	if got[0].Outcome != ledger.OutcomeMalformed || got[0].ErrorKind != "component x is not a feature" {
		t.Errorf("after amend: %+v", got[0])
	}
	if err := l.Amend(ctx, 9999, ledger.OutcomeError, ""); err == nil {
		t.Error("amending an unknown row should fail")
	}
}

func TestListFilters(t *testing.T) {
	l, _, _ := newLedger(t)
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
	l, _, _ := newLedger(t)
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
	l, _, _ := newLedger(t)
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
	l, d, path := newLedger(t)
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
	rs, err := d.Query(`SELECT * FROM calls`)
	if err != nil {
		t.Fatal(err)
	}
	cols, _ := rs.Columns()
	for rs.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rs.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		for i, v := range vals {
			if strings.Contains(strings.ToLower(toString(v)), strings.ToLower(canary)) {
				t.Errorf("column %s contains the prompt text", cols[i])
			}
		}
	}
	rs.Close()
	d.Close() // checkpoints the write-ahead log into the main file
	for _, f := range []string{path, path + "-wal"} {
		if b, err := os.ReadFile(f); err == nil && strings.Contains(string(b), canary) {
			t.Errorf("%s contains the prompt text", f)
		}
	}
}

func toString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(x)
	case string:
		return x
	default:
		return ""
	}
}
