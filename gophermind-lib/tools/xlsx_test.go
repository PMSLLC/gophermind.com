package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestWriteXLSXMultiSheet(t *testing.T) {
	dir := t.TempDir()
	tool := WriteXLSX(dir)

	args := `{
		"path": "report.xlsx",
		"sheets": [
			{"name": "Leads", "headers": ["id", "name"], "rows": [[1, "alice"], [2, "bob"]]},
			{"name": "Totals", "rows": [["count", 2]]}
		]
	}`
	out, err := run(t, tool, args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "report.xlsx") {
		t.Errorf("result should mention the file: %q", out)
	}

	f, err := excelize.OpenFile(filepath.Join(dir, "report.xlsx"))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) != 2 || sheets[0] != "Leads" || sheets[1] != "Totals" {
		t.Fatalf("unexpected sheets: %v", sheets)
	}

	if got, _ := f.GetCellValue("Leads", "A1"); got != "id" {
		t.Errorf("Leads A1 = %q, want %q", got, "id")
	}
	if got, _ := f.GetCellValue("Leads", "B1"); got != "name" {
		t.Errorf("Leads B1 = %q, want %q", got, "name")
	}
	if got, _ := f.GetCellValue("Leads", "A2"); got != "1" {
		t.Errorf("Leads A2 = %q, want %q", got, "1")
	}
	if got, _ := f.GetCellValue("Leads", "B2"); got != "alice" {
		t.Errorf("Leads B2 = %q, want %q", got, "alice")
	}
	if got, _ := f.GetCellValue("Leads", "B3"); got != "bob" {
		t.Errorf("Leads B3 = %q, want %q", got, "bob")
	}
	if got, _ := f.GetCellValue("Totals", "A1"); got != "count" {
		t.Errorf("Totals A1 = %q, want %q", got, "count")
	}
	if got, _ := f.GetCellValue("Totals", "B1"); got != "2" {
		t.Errorf("Totals B1 = %q, want %q", got, "2")
	}
}

func TestWriteXLSXRequiresSheets(t *testing.T) {
	dir := t.TempDir()
	if _, err := run(t, WriteXLSX(dir), `{"path":"x.xlsx","sheets":[]}`); err == nil {
		t.Error("empty sheets should error")
	}
}

func TestWriteXLSXRejectsPathEscape(t *testing.T) {
	dir := t.TempDir()
	args := `{"path":"../escape.xlsx","sheets":[{"name":"S","rows":[["x"]]}]}`
	if _, err := run(t, WriteXLSX(dir), args); err == nil {
		t.Error("path escaping repo root should error")
	}
}

func TestWriteXLSXCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	args := `{"path":"reports/q3.xlsx","sheets":[{"name":"S","rows":[["x"]]}]}`
	if _, err := run(t, WriteXLSX(dir), args); err != nil {
		t.Fatalf("expected parent directory to be created, got error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "reports", "q3.xlsx")); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
}

func TestWriteXLSXFilePermissions(t *testing.T) {
	dir := t.TempDir()
	args := `{"path":"report.xlsx","sheets":[{"name":"S","rows":[["x"]]}]}`
	if _, err := run(t, WriteXLSX(dir), args); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "report.xlsx"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("file permissions = %o, want %o", perm, 0o644)
	}
}

func TestWriteXLSXRejectsDuplicateSheetNames(t *testing.T) {
	dir := t.TempDir()
	args := `{
		"path": "report.xlsx",
		"sheets": [
			{"name": "Leads", "rows": [["a"]]},
			{"name": "Leads", "rows": [["b"]]}
		]
	}`
	if _, err := run(t, WriteXLSX(dir), args); err == nil {
		t.Error("duplicate sheet names should error")
	}
	if _, err := os.Stat(filepath.Join(dir, "report.xlsx")); err == nil {
		t.Error("no file should be written when validation fails before SaveAs")
	}
}

func TestWriteXLSXRejectsNonScalarCellValues(t *testing.T) {
	dir := t.TempDir()
	args := `{
		"path": "report.xlsx",
		"sheets": [
			{"name": "Leads", "rows": [[{"a": 1}]]}
		]
	}`
	if _, err := run(t, WriteXLSX(dir), args); err == nil {
		t.Error("object cell value should error")
	}

	args2 := `{
		"path": "report2.xlsx",
		"sheets": [
			{"name": "Leads", "rows": [[[1, 2]]]}
		]
	}`
	if _, err := run(t, WriteXLSX(dir), args2); err == nil {
		t.Error("array cell value should error")
	}
}
