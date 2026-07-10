package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetCSVCell(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "d.csv"), []byte("id,name\n1,alice\n2,bob\n"), 0o644)
	tool := SetCSVCell(dir)

	// Set row 2 (alice), col 2 (name) -> "ALICE" (row 1 is the header).
	out, err := run(t, tool, `{"path":"d.csv","row":2,"col":2,"value":"ALICE"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "d.csv") {
		t.Errorf("result should mention the file: %q", out)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "d.csv"))
	if !strings.Contains(string(data), "1,ALICE") {
		t.Errorf("cell not updated:\n%s", data)
	}
	if !strings.Contains(string(data), "2,bob") {
		t.Errorf("other rows should be untouched:\n%s", data)
	}
}

func TestSetCSVCellOutOfBounds(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "d.csv"), []byte("a,b\n1,2\n"), 0o644)
	tool := SetCSVCell(dir)
	if _, err := run(t, tool, `{"path":"d.csv","row":99,"col":1,"value":"x"}`); err == nil {
		t.Error("row out of bounds should error")
	}
	if _, err := run(t, tool, `{"path":"d.csv","row":1,"col":99,"value":"x"}`); err == nil {
		t.Error("col out of bounds should error")
	}
	if _, err := run(t, tool, `{"path":"d.csv","row":0,"col":1,"value":"x"}`); err == nil {
		t.Error("row < 1 should error")
	}
}

func TestSetCSVCellMissingFile(t *testing.T) {
	if _, err := run(t, SetCSVCell(t.TempDir()), `{"path":"nope.csv","row":1,"col":1,"value":"x"}`); err == nil {
		t.Error("missing file should error")
	}
}
