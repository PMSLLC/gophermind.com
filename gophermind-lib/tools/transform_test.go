package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFileT(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

const salesCSV = `region,product,amount
west,apple,10
west,banana,5
east,apple,20
east,banana,7
`

func TestDataTransformFilter(t *testing.T) {
	dir := writeFileT(t, t.TempDir(), "sales.csv", salesCSV)
	out, err := run(t, DataTransform(dir), `{"path":"sales.csv","filter":{"column":"region","op":"==","value":"west"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "apple") || !strings.Contains(out, "banana") {
		t.Errorf("expected west rows:\n%s", out)
	}
	if strings.Contains(out, "20") || strings.Contains(out, "east") {
		t.Errorf("east rows should be filtered out:\n%s", out)
	}
}

func TestDataTransformNumericFilter(t *testing.T) {
	dir := writeFileT(t, t.TempDir(), "sales.csv", salesCSV)
	out, err := run(t, DataTransform(dir), `{"path":"sales.csv","filter":{"column":"amount","op":">","value":"9"}}`)
	if err != nil {
		t.Fatal(err)
	}
	// amounts 10 and 20 pass; 5 and 7 do not.
	if !strings.Contains(out, "10") || !strings.Contains(out, "20") {
		t.Errorf("expected amounts >9:\n%s", out)
	}
	if strings.Contains(out, ",5,") {
		t.Errorf("amount 5 should be excluded:\n%s", out)
	}
}

func TestDataTransformGroupSum(t *testing.T) {
	dir := writeFileT(t, t.TempDir(), "sales.csv", salesCSV)
	out, err := run(t, DataTransform(dir), `{"path":"sales.csv","group_by":"region","agg":{"func":"sum","column":"amount"}}`)
	if err != nil {
		t.Fatal(err)
	}
	// west: 10+5=15, east: 20+7=27
	if !strings.Contains(out, "15") {
		t.Errorf("west sum should be 15:\n%s", out)
	}
	if !strings.Contains(out, "27") {
		t.Errorf("east sum should be 27:\n%s", out)
	}
}

func TestDataTransformGroupCount(t *testing.T) {
	dir := writeFileT(t, t.TempDir(), "sales.csv", salesCSV)
	out, err := run(t, DataTransform(dir), `{"path":"sales.csv","group_by":"region","agg":{"func":"count"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "2") {
		t.Errorf("each region should count 2:\n%s", out)
	}
}

func TestDataTransformJSONL(t *testing.T) {
	jsonl := `{"region":"west","amount":10}
{"region":"east","amount":20}
`
	dir := writeFileT(t, t.TempDir(), "d.jsonl", jsonl)
	out, err := run(t, DataTransform(dir), `{"path":"d.jsonl","group_by":"region","agg":{"func":"sum","column":"amount"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "10") || !strings.Contains(out, "20") {
		t.Errorf("jsonl group sum wrong:\n%s", out)
	}
}

func TestDataTransformBadColumn(t *testing.T) {
	dir := writeFileT(t, t.TempDir(), "sales.csv", salesCSV)
	if _, err := run(t, DataTransform(dir), `{"path":"sales.csv","filter":{"column":"nope","op":"==","value":"x"}}`); err == nil {
		t.Error("unknown filter column should error")
	}
}
