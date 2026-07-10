package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestDryRunWrapsGatedTools(t *testing.T) {
	executed := false
	writeTool := Tool{
		Name: "write_file",
		Run: func(_ context.Context, _ json.RawMessage) (string, error) {
			executed = true
			return "wrote", nil
		},
	}
	readTool := Tool{
		Name: "read_file",
		Run: func(_ context.Context, _ json.RawMessage) (string, error) {
			return "contents", nil
		},
	}

	wrapped := WrapDryRun([]Tool{writeTool, readTool})

	// The gated write tool must not execute; it reports intent instead.
	var write, read Tool
	for _, w := range wrapped {
		switch w.Name {
		case "write_file":
			write = w
		case "read_file":
			read = w
		}
	}
	out, err := write.Run(context.Background(), json.RawMessage(`{"path":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if executed {
		t.Error("gated tool should NOT execute in dry-run mode")
	}
	if !strings.Contains(strings.ToLower(out), "dry-run") || !strings.Contains(out, "write_file") {
		t.Errorf("dry-run output should report the intended call: %q", out)
	}

	// Read-only tools still run normally.
	out, err = read.Run(context.Background(), nil)
	if err != nil || out != "contents" {
		t.Errorf("read-only tool should run normally, got %q err=%v", out, err)
	}
}
