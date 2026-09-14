package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWASMRejectsInvalid(t *testing.T) {
	if _, err := runWASM(context.Background(), []byte("not wasm at all"), ""); err == nil {
		t.Error("invalid wasm bytes should error")
	}
}

func TestRunWASMEmptyModule(t *testing.T) {
	// A valid but empty module (magic + version) exports nothing.
	empty := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	_, err := runWASM(context.Background(), empty, "")
	if err == nil || !strings.Contains(err.Error(), "no functions") {
		t.Errorf("empty module should report no exported functions, got %v", err)
	}
}

func TestWASMToolMissingModule(t *testing.T) {
	if _, err := run(t, WASMTool(t.TempDir()), `{"module":"nope.wasm"}`); err == nil {
		t.Error("missing module should error")
	}
}

func TestWASMToolContainsPath(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "m.wasm"), []byte{0, 1, 2}, 0o644)
	if _, err := run(t, WASMTool(dir), `{"module":"../../etc/x.wasm"}`); err == nil {
		t.Error("module path escaping the root should be rejected")
	}
}
