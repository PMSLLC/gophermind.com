package tools

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gophermind/internal/safety"
)

// previewRows caps how many sample rows the inspector prints.
const previewRows = 5

// InspectData returns a read-only tool that reports the schema (columns/keys and
// inferred types) and a small row preview of a CSV, JSON, or JSONL file, so the
// model can understand a data file without the whole thing being dumped into
// context.
func InspectData(root string) Tool {
	return Tool{
		Name:        "inspect_data",
		Description: "Inspect a CSV, JSON, or JSONL data file: reports the detected format, row count, columns/keys with inferred types, and a small row preview. Read-only.",
		Schema:      object(map[string]any{"path": str("Path to a .csv, .json, or .jsonl file, relative to the repo root.")}, "path"),
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			full, err := safety.SafeJoin(root, a.Path)
			if err != nil {
				return "", err
			}
			data, err := os.ReadFile(full)
			if err != nil {
				return "", fmt.Errorf("read %s: %w", a.Path, err)
			}
			switch strings.ToLower(filepath.Ext(a.Path)) {
			case ".csv":
				return inspectCSV(data)
			case ".json":
				return inspectJSON(data)
			case ".jsonl", ".ndjson":
				return inspectJSONL(data)
			default:
				return "", fmt.Errorf("unsupported file type %q: use .csv, .json, or .jsonl", filepath.Ext(a.Path))
			}
		},
	}
}

func inspectCSV(data []byte) (string, error) {
	r := csv.NewReader(strings.NewReader(string(data)))
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		return "", fmt.Errorf("parse csv: %w", err)
	}
	if len(records) == 0 {
		return "format: csv\nrows: 0\n(empty)", nil
	}
	header := records[0]
	rows := records[1:]

	var b strings.Builder
	fmt.Fprintf(&b, "format: csv\nrows: %d\ncolumns (%d):\n", len(rows), len(header))
	for i, col := range header {
		fmt.Fprintf(&b, "  - %s (%s)\n", col, inferColumnType(rows, i))
	}
	b.WriteString("preview:\n")
	b.WriteString("  " + strings.Join(header, " | ") + "\n")
	for i, row := range rows {
		if i >= previewRows {
			break
		}
		b.WriteString("  " + strings.Join(row, " | ") + "\n")
	}
	return truncate(b.String()), nil
}

// inferColumnType guesses a column's type from its values: int, float, bool, or
// string (the fallback).
func inferColumnType(rows [][]string, col int) string {
	allInt, allFloat, allBool, seen := true, true, true, false
	for _, r := range rows {
		if col >= len(r) {
			continue
		}
		v := strings.TrimSpace(r[col])
		if v == "" {
			continue
		}
		seen = true
		if _, err := strconv.Atoi(v); err != nil {
			allInt = false
		}
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			allFloat = false
		}
		if v != "true" && v != "false" {
			allBool = false
		}
	}
	switch {
	case !seen:
		return "empty"
	case allBool:
		return "bool"
	case allInt:
		return "int"
	case allFloat:
		return "float"
	default:
		return "string"
	}
}

func inspectJSON(data []byte) (string, error) {
	var arr []map[string]any
	if err := json.Unmarshal(data, &arr); err != nil {
		return "", fmt.Errorf("expected a JSON array of objects: %w", err)
	}
	return renderRecords("json", arr), nil
}

func inspectJSONL(data []byte) (string, error) {
	var recs []map[string]any
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			return "", fmt.Errorf("parse jsonl line: %w", err)
		}
		recs = append(recs, m)
	}
	return renderRecords("jsonl", recs), nil
}

// renderRecords summarizes a slice of object records: union of keys with inferred
// types and a small preview.
func renderRecords(format string, recs []map[string]any) string {
	types := map[string]string{}
	var keys []string
	for _, r := range recs {
		for k, v := range r {
			if _, ok := types[k]; !ok {
				keys = append(keys, k)
			}
			types[k] = jsonType(v)
		}
	}
	sort.Strings(keys)

	var b strings.Builder
	fmt.Fprintf(&b, "format: %s\nrows: %d\nkeys (%d):\n", format, len(recs), len(keys))
	for _, k := range keys {
		fmt.Fprintf(&b, "  - %s (%s)\n", k, types[k])
	}
	b.WriteString("preview:\n")
	for i, r := range recs {
		if i >= previewRows {
			break
		}
		line, _ := json.Marshal(r)
		b.WriteString("  " + string(line) + "\n")
	}
	return truncate(b.String())
}

func jsonType(v any) string {
	switch v.(type) {
	case bool:
		return "bool"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case nil:
		return "null"
	default:
		return "unknown"
	}
}
