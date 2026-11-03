package ledger_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/ledger"
)

func sampleText(id int64) ledger.Text {
	return ledger.Text{ID: id, Stage: "implement:fn-a", NodeID: "fn-a", Provider: "mini", Model: "gemma",
		Messages: []ledger.TextMessage{{Role: "system", Content: "be brief"}, {Role: "user", Content: "hello"}},
		Response: "world"}
}

func enableVerbose(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "_state"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_state", "verbose"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRecordTextOffWritesNothing(t *testing.T) {
	l, dir := newLedger(t)
	if err := l.RecordText(context.Background(), "r", sampleText(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "_state", "llm")); !os.IsNotExist(err) {
		t.Fatalf("llm folder exists without the verbose marker: %v", err)
	}
	if ids, err := l.ListText("r"); err != nil || len(ids) != 0 {
		t.Fatalf("ListText = %v, %v", ids, err)
	}
}

func TestRecordTextOnWritesTheFileAndRoundTrips(t *testing.T) {
	l, dir := newLedger(t)
	enableVerbose(t, dir)
	want := sampleText(7)
	if err := l.RecordText(context.Background(), "r", want); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "_state", "llm", "7.json")); err != nil {
		t.Fatal(err)
	}
	got, err := l.ReadText("r", 7)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, %v", got, err)
	}
	if err := l.RecordText(context.Background(), "r", sampleText(3)); err != nil {
		t.Fatal(err)
	}
	ids, err := l.ListText("r")
	if err != nil || !reflect.DeepEqual(ids, []int64{3, 7}) {
		t.Fatalf("ListText = %v, %v", ids, err)
	}
}

func TestRecordTextRedactsSecrets(t *testing.T) {
	l, dir := newLedger(t)
	enableVerbose(t, dir)
	const key = "sk-abcdefghijklmnopqrstuvwxyz0123"
	tx := sampleText(1)
	tx.Messages[1].Content = "use " + key + " and mail a@b.com"
	tx.Response = "the key is " + key
	if err := l.RecordText(context.Background(), "r", tx); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "_state", "llm", "1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), key) || strings.Contains(string(b), "a@b.com") {
		t.Fatalf("secret on disk: %s", b)
	}
	if !strings.Contains(string(b), "[REDACTED]") {
		t.Fatalf("no placeholder: %s", b)
	}
}

func TestRecordTextRejectsBadIDs(t *testing.T) {
	l, dir := newLedger(t)
	enableVerbose(t, dir)
	if err := l.RecordText(context.Background(), "r", sampleText(0)); err == nil {
		t.Error("id 0 accepted")
	}
	for _, id := range []int64{0, -1} {
		if _, err := l.ReadText("r", id); err == nil {
			t.Errorf("ReadText(%d) accepted", id)
		}
	}
}
