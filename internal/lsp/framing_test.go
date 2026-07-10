package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestWriteReadRoundtrip(t *testing.T) {
	var buf bytes.Buffer
	msg := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize"}
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatal(err)
	}
	// The wire format must carry a Content-Length header.
	if !strings.HasPrefix(buf.String(), "Content-Length: ") {
		t.Errorf("missing Content-Length header:\n%q", buf.String())
	}
	body, err := ReadMessage(bufio.NewReader(&buf))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	json.Unmarshal(body, &got)
	if got["method"] != "initialize" {
		t.Errorf("roundtrip lost data: %v", got)
	}
}

func TestReadMultipleMessages(t *testing.T) {
	var buf bytes.Buffer
	WriteMessage(&buf, map[string]any{"id": 1})
	WriteMessage(&buf, map[string]any{"id": 2})
	r := bufio.NewReader(&buf)
	for i := 1; i <= 2; i++ {
		body, err := ReadMessage(r)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		json.Unmarshal(body, &m)
		if int(m["id"].(float64)) != i {
			t.Errorf("message %d has id %v", i, m["id"])
		}
	}
}

func TestReadMissingHeader(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("\r\n{}"))
	if _, err := ReadMessage(r); err == nil {
		t.Error("missing Content-Length should error")
	}
}

func TestDefinitionRejectsTraversal(t *testing.T) {
	// The path check happens before any subprocess work, so a bogus argv is fine.
	if _, err := Definition(contextTODO(), []string{"true"}, "/repo", "../../etc/passwd", 0, 0); err == nil {
		t.Error("a file escaping the workspace root must be rejected")
	}
}

func contextTODO() context.Context { return context.Background() }
