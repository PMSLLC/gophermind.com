package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectCSV(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "data.csv"), []byte("id,name,active\n1,alice,true\n2,bob,false\n3,carol,true\n"), 0o644)
	out, err := run(t, InspectData(dir), `{"path":"data.csv"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"csv", "rows: 3", "id", "name", "active", "alice"} {
		if !strings.Contains(out, want) {
			t.Errorf("csv inspect missing %q; got:\n%s", want, out)
		}
	}
}

func TestInspectJSONArray(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.json"), []byte(`[{"id":1,"name":"x"},{"id":2,"name":"y"}]`), 0o644)
	out, err := run(t, InspectData(dir), `{"path":"a.json"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "json") || !strings.Contains(out, "id") || !strings.Contains(out, "name") {
		t.Errorf("json inspect missing keys; got:\n%s", out)
	}
	if !strings.Contains(out, "rows: 2") {
		t.Errorf("json inspect wrong row count; got:\n%s", out)
	}
}

func TestInspectJSONL(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.jsonl"), []byte(`{"k":1}`+"\n"+`{"k":2}`+"\n"+`{"k":3}`+"\n"), 0o644)
	out, err := run(t, InspectData(dir), `{"path":"a.jsonl"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "jsonl") || !strings.Contains(out, "rows: 3") || !strings.Contains(out, "k") {
		t.Errorf("jsonl inspect wrong; got:\n%s", out)
	}
}

func TestInspectRejectsUnknownAndMissing(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "x.bin"), []byte("garbage"), 0o644)
	if _, err := run(t, InspectData(dir), `{"path":"x.bin"}`); err == nil {
		t.Error("unknown extension should error")
	}
	if _, err := run(t, InspectData(dir), `{"path":"nope.csv"}`); err == nil {
		t.Error("missing file should error")
	}
}
