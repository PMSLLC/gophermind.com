package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Location is a resolved definition location.
type Location struct {
	File string
	Line int
}

// Definition launches the LSP server (command argv), initializes it for rootDir,
// opens the file, and requests the definition at (line, col) (0-based). It
// returns the resulting locations. This is a one-shot client: it starts and
// stops the server per call, which is simple and adequate for CLI use.
func Definition(ctx context.Context, argv []string, rootDir, file string, line, col int) ([]Location, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("no LSP command configured")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer cmd.Process.Kill()
	r := bufio.NewReader(stdout)

	rootURI := "file://" + rootDir
	fileURI := "file://" + filepath.Join(rootDir, file)

	// initialize -> initialized -> didOpen -> definition.
	_ = WriteMessage(stdin, rpc(1, "initialize", map[string]any{"processId": nil, "rootUri": rootURI, "capabilities": map[string]any{}}))
	if _, err := awaitID(r, 1); err != nil {
		return nil, err
	}
	_ = WriteMessage(stdin, notify("initialized", map[string]any{}))

	src, _ := readFile(filepath.Join(rootDir, file))
	_ = WriteMessage(stdin, notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{"uri": fileURI, "languageId": langOf(file), "version": 1, "text": src},
	}))
	_ = WriteMessage(stdin, rpc(2, "textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": fileURI},
		"position":     map[string]any{"line": line, "character": col},
	}))
	resp, err := awaitID(r, 2)
	if err != nil {
		return nil, err
	}
	return parseLocations(resp), nil
}

func rpc(id int, method string, params any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
}
func notify(method string, params any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
}

// awaitID reads messages until one with the given response id arrives.
func awaitID(r *bufio.Reader, id int) (json.RawMessage, error) {
	for i := 0; i < 100; i++ {
		body, err := ReadMessage(r)
		if err != nil {
			return nil, err
		}
		var m struct {
			ID     *int            `json:"id"`
			Result json.RawMessage `json:"result"`
		}
		if json.Unmarshal(body, &m) == nil && m.ID != nil && *m.ID == id {
			return m.Result, nil
		}
	}
	return nil, fmt.Errorf("no response for id %d", id)
}

// parseLocations handles both a single Location and an array of them.
func parseLocations(result json.RawMessage) []Location {
	type loc struct {
		URI   string `json:"uri"`
		Range struct {
			Start struct {
				Line int `json:"line"`
			} `json:"start"`
		} `json:"range"`
	}
	var arr []loc
	if json.Unmarshal(result, &arr) != nil {
		var single loc
		if json.Unmarshal(result, &single) != nil {
			return nil
		}
		arr = []loc{single}
	}
	out := make([]Location, 0, len(arr))
	for _, l := range arr {
		out = append(out, Location{File: strings.TrimPrefix(l.URI, "file://"), Line: l.Range.Start.Line + 1})
	}
	return out
}

func langOf(file string) string {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".py":
		return "python"
	default:
		return "plaintext"
	}
}

// readFile is split out so client.go has no direct os import churn.
func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}
