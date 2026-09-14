package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/parquet-go/parquet-go"
)

type pqRow struct {
	Name string `parquet:"name"`
	Age  int64  `parquet:"age"`
}

func writeParquet(t *testing.T, dir string, rows []pqRow) string {
	t.Helper()
	p := filepath.Join(dir, "data.parquet")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := parquet.NewGenericWriter[pqRow](f)
	if _, err := w.Write(rows); err != nil {
		t.Fatal(err)
	}
	w.Close()
	return dir
}

func TestReadParquet(t *testing.T) {
	dir := writeParquet(t, t.TempDir(), []pqRow{{"alice", 30}, {"bob", 25}})
	out, err := run(t, ReadParquet(dir), `{"path":"data.parquet"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"parquet", "name", "age", "alice", "bob", "30", "25"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestReadParquetMissing(t *testing.T) {
	if _, err := run(t, ReadParquet(t.TempDir()), `{"path":"nope.parquet"}`); err == nil {
		t.Error("missing file should error")
	}
}

func TestReadParquetNotParquet(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "x.parquet"), []byte("not parquet"), 0o644)
	if _, err := run(t, ReadParquet(dir), `{"path":"x.parquet"}`); err == nil {
		t.Error("invalid parquet should error")
	}
}
