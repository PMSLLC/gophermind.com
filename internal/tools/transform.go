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

// transformMaxRows caps rows returned by data_transform.
const transformMaxRows = 200

// DataTransform returns a read-only tool that filters, groups, and aggregates
// rows of a CSV or JSONL file — dataframe-style analysis without writing a
// script. It reads the whole file into memory; output is a compact table.
func DataTransform(root string) Tool {
	return Tool{
		Name:        "data_transform",
		Description: "Filter, group, and aggregate rows of a CSV or JSONL file. Supports a single filter (column/op/value), group_by a column, and an aggregate (count/sum/avg/min/max). Read-only.",
		Schema: object(map[string]any{
			"path": str("Path to a .csv or .jsonl file, relative to the repo root."),
			"filter": map[string]any{"type": "object", "description": "Optional row filter.", "properties": map[string]any{
				"column": str("Column to test."),
				"op":     str("Comparison: ==, !=, >, <, >=, <=, contains."),
				"value":  str("Value to compare against (numeric compares when both parse as numbers)."),
			}},
			"group_by": str("Optional column to group rows by."),
			"agg": map[string]any{"type": "object", "description": "Optional aggregate applied per group (or over all rows).", "properties": map[string]any{
				"func":   str("count, sum, avg, min, or max."),
				"column": str("Column to aggregate (not needed for count)."),
			}},
		}, "path"),
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Path   string `json:"path"`
				Filter *struct {
					Column string `json:"column"`
					Op     string `json:"op"`
					Value  string `json:"value"`
				} `json:"filter"`
				GroupBy string `json:"group_by"`
				Agg     *struct {
					Func   string `json:"func"`
					Column string `json:"column"`
				} `json:"agg"`
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

			cols, rows, err := loadRows(a.Path, data)
			if err != nil {
				return "", err
			}
			colSet := map[string]bool{}
			for _, c := range cols {
				colSet[c] = true
			}

			if a.Filter != nil {
				if !colSet[a.Filter.Column] {
					return "", fmt.Errorf("unknown filter column %q", a.Filter.Column)
				}
				rows, err = filterRows(rows, a.Filter.Column, a.Filter.Op, a.Filter.Value)
				if err != nil {
					return "", err
				}
			}

			if a.GroupBy != "" || a.Agg != nil {
				if a.GroupBy != "" && !colSet[a.GroupBy] {
					return "", fmt.Errorf("unknown group_by column %q", a.GroupBy)
				}
				if a.Agg == nil {
					return "", fmt.Errorf("group_by requires an agg")
				}
				if a.Agg.Func != "count" && !colSet[a.Agg.Column] {
					return "", fmt.Errorf("unknown agg column %q", a.Agg.Column)
				}
				return aggregate(rows, a.GroupBy, a.Agg.Func, a.Agg.Column)
			}

			return renderRows(cols, rows), nil
		},
	}
}

// loadRows reads a CSV or JSONL file into column names and string-valued rows.
func loadRows(path string, data []byte) ([]string, []map[string]string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".csv":
		r := csv.NewReader(strings.NewReader(string(data)))
		recs, err := r.ReadAll()
		if err != nil {
			return nil, nil, fmt.Errorf("parse csv: %w", err)
		}
		if len(recs) == 0 {
			return nil, nil, nil
		}
		header := recs[0]
		var rows []map[string]string
		for _, rec := range recs[1:] {
			m := make(map[string]string, len(header))
			for i, col := range header {
				if i < len(rec) {
					m[col] = rec[i]
				}
			}
			rows = append(rows, m)
		}
		return header, rows, nil
	case ".jsonl", ".ndjson":
		var rows []map[string]string
		seen := map[string]bool{}
		var cols []string
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var obj map[string]any
			if err := json.Unmarshal([]byte(line), &obj); err != nil {
				return nil, nil, fmt.Errorf("parse jsonl: %w", err)
			}
			m := make(map[string]string, len(obj))
			for k, v := range obj {
				m[k] = fmt.Sprintf("%v", v)
				if !seen[k] {
					seen[k] = true
					cols = append(cols, k)
				}
			}
			rows = append(rows, m)
		}
		sort.Strings(cols)
		return cols, rows, nil
	default:
		return nil, nil, fmt.Errorf("unsupported file type %q: use .csv or .jsonl", filepath.Ext(path))
	}
}

// filterRows keeps rows where column op value holds.
func filterRows(rows []map[string]string, column, op, value string) ([]map[string]string, error) {
	var out []map[string]string
	for _, r := range rows {
		ok, err := compare(r[column], op, value)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, r)
		}
	}
	return out, nil
}

// compare evaluates "a op b". Numeric comparison is used when both sides parse
// as float64; otherwise string comparison (contains/==/!=).
func compare(a, op, b string) (bool, error) {
	af, aErr := strconv.ParseFloat(a, 64)
	bf, bErr := strconv.ParseFloat(b, 64)
	numeric := aErr == nil && bErr == nil
	switch op {
	case "==":
		return a == b, nil
	case "!=":
		return a != b, nil
	case "contains":
		return strings.Contains(a, b), nil
	case ">", "<", ">=", "<=":
		if !numeric {
			return false, fmt.Errorf("op %q needs numeric operands (got %q, %q)", op, a, b)
		}
		switch op {
		case ">":
			return af > bf, nil
		case "<":
			return af < bf, nil
		case ">=":
			return af >= bf, nil
		default:
			return af <= bf, nil
		}
	default:
		return false, fmt.Errorf("unknown op %q", op)
	}
}

// aggregate groups rows by groupBy (or a single group when empty) and applies
// fn over aggCol, rendering a compact two-column table.
func aggregate(rows []map[string]string, groupBy, fn, aggCol string) (string, error) {
	groups := map[string][]map[string]string{}
	var order []string
	for _, r := range rows {
		key := ""
		if groupBy != "" {
			key = r[groupBy]
		}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], r)
	}
	sort.Strings(order)

	var b strings.Builder
	head := fn
	if aggCol != "" {
		head = fn + "(" + aggCol + ")"
	}
	if groupBy != "" {
		fmt.Fprintf(&b, "%s | %s\n", groupBy, head)
	} else {
		fmt.Fprintf(&b, "%s\n", head)
	}
	for _, key := range order {
		val, err := applyAgg(groups[key], fn, aggCol)
		if err != nil {
			return "", err
		}
		if groupBy != "" {
			fmt.Fprintf(&b, "%s | %s\n", key, val)
		} else {
			fmt.Fprintf(&b, "%s\n", val)
		}
	}
	return b.String(), nil
}

// applyAgg computes one aggregate value over a group.
func applyAgg(group []map[string]string, fn, col string) (string, error) {
	if fn == "count" {
		return strconv.Itoa(len(group)), nil
	}
	var nums []float64
	for _, r := range group {
		f, err := strconv.ParseFloat(r[col], 64)
		if err != nil {
			return "", fmt.Errorf("agg %s needs numeric column %q (got %q)", fn, col, r[col])
		}
		nums = append(nums, f)
	}
	if len(nums) == 0 {
		return "0", nil
	}
	var res float64
	switch fn {
	case "sum", "avg":
		for _, n := range nums {
			res += n
		}
		if fn == "avg" {
			res /= float64(len(nums))
		}
	case "min":
		res = nums[0]
		for _, n := range nums {
			if n < res {
				res = n
			}
		}
	case "max":
		res = nums[0]
		for _, n := range nums {
			if n > res {
				res = n
			}
		}
	default:
		return "", fmt.Errorf("unknown agg func %q", fn)
	}
	return strconv.FormatFloat(res, 'g', -1, 64), nil
}

// renderRows renders rows as a compact CSV-ish table capped at transformMaxRows.
func renderRows(cols []string, rows []map[string]string) string {
	var b strings.Builder
	b.WriteString(strings.Join(cols, ",") + "\n")
	for i, r := range rows {
		if i >= transformMaxRows {
			fmt.Fprintf(&b, "… [capped at %d rows]\n", transformMaxRows)
			break
		}
		cells := make([]string, len(cols))
		for j, c := range cols {
			cells[j] = r[c]
		}
		b.WriteString(strings.Join(cells, ",") + "\n")
	}
	if len(rows) == 0 {
		b.WriteString("(no rows)\n")
	}
	return b.String()
}
