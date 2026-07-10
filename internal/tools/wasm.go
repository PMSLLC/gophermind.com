package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"gophermind/internal/safety"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// wasmTimeout bounds a WASM module's execution.
const wasmTimeout = 30 * time.Second

// WASMTool returns a tool that runs an untrusted community tool compiled to a
// WASI WebAssembly module in a wazero sandbox: it has NO filesystem, network, or
// host access — only the args passed on stdin and whatever it writes to stdout.
// This is safe execution of third-party extensions with zero capability grants.
func WASMTool(root string) Tool {
	return Tool{
		Name:        "run_wasm",
		Description: "Run a sandboxed WebAssembly (WASI) tool module: the JSON args are provided on its stdin and its stdout is returned. The module has no filesystem/network access.",
		Schema: object(map[string]any{
			"module": str("Path to a .wasm module, relative to the repo root."),
			"input":  str("Text provided to the module on stdin."),
		}, "module"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Module string `json:"module"`
				Input  string `json:"input"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			full, err := safety.SafeJoin(root, a.Module)
			if err != nil {
				return "", err
			}
			wasmBytes, err := readFileBytes(full)
			if err != nil {
				return "", fmt.Errorf("read module: %w", err)
			}
			return runWASM(ctx, wasmBytes, a.Input)
		},
	}
}

// runWASM instantiates and runs a WASI module with the given stdin, returning
// its stdout. No host capabilities (fs/net/env) are granted.
func runWASM(ctx context.Context, wasmBytes []byte, input string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, wasmTimeout)
	defer cancel()

	r := wazero.NewRuntime(ctx)
	defer r.Close(ctx)
	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	compiled, err := r.CompileModule(ctx, wasmBytes)
	if err != nil {
		return "", fmt.Errorf("invalid wasm module: %w", err)
	}
	if len(compiled.ExportedFunctions()) == 0 {
		return "", fmt.Errorf("module exports no functions")
	}

	var stdout, stderr bytes.Buffer
	cfg := wazero.NewModuleConfig().
		WithStdin(strings.NewReader(input)).
		WithStdout(&stdout).
		WithStderr(&stderr)
		// No WithFS / WithEnv / network: the module is fully sandboxed.

	if _, err := r.InstantiateModule(ctx, compiled, cfg); err != nil {
		// A WASI _start that exits non-zero surfaces here; include stderr.
		return "", fmt.Errorf("wasm run: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return truncate(stdout.String()), nil
}

// readFileBytes reads a file's bytes.
func readFileBytes(path string) ([]byte, error) { return os.ReadFile(path) }
