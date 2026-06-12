package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gophermind/internal/llm"
	"gophermind/internal/tools"
)

// TestExportJSONLRoundTrips checks the exporter emits one valid-JSON line per
// message, in order, and that each line unmarshals back to an equal Message.
func TestExportJSONLRoundTrips(t *testing.T) {
	a := &Agent{msgs: []llm.Message{
		{Role: "system", Content: "you are a helpful agent"},
		{Role: "user", Content: "edit the file\nwith a newline"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{
			{ID: "call_1", Type: "function", Function: llm.FunctionCall{Name: "write_file", Arguments: `{"path":"x.txt"}`}},
		}},
		{Role: "tool", ToolCallID: "call_1", Name: "write_file", Content: "ok"},
		{Role: "assistant", Content: "done"},
	}}

	var buf bytes.Buffer
	if err := a.ExportJSONL(&buf); err != nil {
		t.Fatalf("ExportJSONL: %v", err)
	}

	// Trailing newline after the last record means SplitAfter-style trimming.
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != len(a.msgs) {
		t.Fatalf("got %d lines, want %d (one per message)", len(lines), len(a.msgs))
	}

	for i, line := range lines {
		// A raw embedded newline would have split one message across two lines;
		// scanning above already guards that, but assert each line is valid JSON
		// and round-trips to the original message.
		var got llm.Message
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Fatalf("line %d not valid JSON: %v\n%s", i, err, line)
		}
		want := a.msgs[i]
		if got.Role != want.Role || got.Content != want.Content ||
			got.ToolCallID != want.ToolCallID || got.Name != want.Name ||
			len(got.ToolCalls) != len(want.ToolCalls) {
			t.Errorf("line %d round-trip mismatch:\n got %+v\nwant %+v", i, got, want)
		}
	}
}

// TestExportJSONLNoCredentialLeak confirms the dump contains only message
// fields — never an API key or Authorization header, even after a real
// (scripted) session against an authenticated endpoint.
func TestExportJSONLNoCredentialLeak(t *testing.T) {
	const secret = "sk-supersecret-key-value"
	sp := &scriptedProvider{responses: []string{finalResp("hello back")}}
	srv := httptest.NewServer(sp.handler(t))
	t.Cleanup(srv.Close)
	// A client carrying a real API key: it must never surface in the transcript.
	client := llm.New(srv.URL, secret, "m", 5*time.Second, false)
	reg := tools.NewRegistry(tools.ReadFile(t.TempDir()))
	a := New(client, reg, 25, nil, nil)

	if _, err := a.Send(context.Background(), "hi"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var buf bytes.Buffer
	if err := a.ExportJSONL(&buf); err != nil {
		t.Fatalf("ExportJSONL: %v", err)
	}
	if strings.Contains(buf.String(), secret) {
		t.Fatal("transcript leaked the API key")
	}
	// Sanity: the user prompt IS present (it is part of the history).
	if !strings.Contains(buf.String(), "hi") {
		t.Fatal("transcript missing the user prompt")
	}
}

// TestExportJSONLOrderingAndCount verifies the line count and ordering match
// the message history exactly after a multi-turn tool-using session.
func TestExportJSONLOrderingAndCount(t *testing.T) {
	root := t.TempDir()
	sp := &scriptedProvider{responses: []string{
		toolCallResp("call_1", "write_file", `{"path":"x.txt","content":"hi"}`),
		finalResp("complete"),
	}}
	a := newTestAgent(t, sp, root)
	if _, err := a.Send(context.Background(), "do the thing"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var buf bytes.Buffer
	if err := a.ExportJSONL(&buf); err != nil {
		t.Fatalf("ExportJSONL: %v", err)
	}

	var n int
	sc := bufio.NewScanner(&buf)
	var roles []string
	for sc.Scan() {
		var m llm.Message
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("line %d invalid: %v", n, err)
		}
		roles = append(roles, m.Role)
		n++
	}
	if n != len(a.msgs) {
		t.Fatalf("exported %d lines, want %d messages", n, len(a.msgs))
	}
	// Expected shape: system, user, assistant(tool call), tool, assistant(final).
	want := []string{"system", "user", "assistant", "tool", "assistant"}
	if strings.Join(roles, ",") != strings.Join(want, ",") {
		t.Errorf("role ordering = %v, want %v", roles, want)
	}
}

// TestWriteTranscriptFileAndPerms checks the on-disk dump matches the history,
// is created 0600, and that a created parent dir is 0700.
func TestWriteTranscriptFileAndPerms(t *testing.T) {
	root := t.TempDir()
	sp := &scriptedProvider{responses: []string{finalResp("hi back")}}
	a := newTestAgent(t, sp, root)
	if _, err := a.Send(context.Background(), "hello"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	dir := filepath.Join(root, "transcripts")
	path := filepath.Join(dir, "session.jsonl")
	if err := a.WriteTranscript(path); err != nil {
		t.Fatalf("WriteTranscript: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("file perm = %o, want 600", perm)
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir perm = %o, want 700", perm)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != len(a.msgs) {
		t.Fatalf("file has %d lines, want %d messages", len(lines), len(a.msgs))
	}
	for i, line := range lines {
		var m llm.Message
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("line %d invalid JSON: %v", i, err)
		}
	}
}

// TestWriteTranscriptEmptyPath rejects an empty path rather than crashing.
func TestWriteTranscriptEmptyPath(t *testing.T) {
	a := &Agent{msgs: []llm.Message{{Role: "system", Content: "x"}}}
	if err := a.WriteTranscript("  "); err == nil {
		t.Fatal("expected error for empty path")
	}
}
