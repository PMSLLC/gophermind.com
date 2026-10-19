package planner

import (
	"encoding/json"
	"strings"
	"testing"
)

func mainDoc(t *testing.T, pkg string, sigs ...string) map[string]any {
	t.Helper()
	fns := []map[string]any{}
	for i, s := range sigs {
		fns = append(fns, map[string]any{"id": "fn-f" + string(rune('a'+i)), "component": "entry-point", "package": pkg, "file": "x.go", "doc": "d", "uses": []any{}, "signature": s})
	}
	raw, _ := json.Marshal(map[string]any{
		"module":     "csvstat",
		"components": []map[string]any{{"id": "entry-point", "package": pkg, "exports": []any{}}},
		"functions":  fns,
	})
	var d map[string]any
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestMainPackageWithoutMainIsADefect(t *testing.T) {
	d := mainDoc(t, "cmd/csvstat", "func run(args []string) int", "func parseFlags(args []string) error")
	got := mainDefects(d)
	if len(got) != 1 || got[0] != "cmd/csvstat" {
		t.Fatalf("defects = %v", got)
	}
	msg := mainDefectText("cmd/csvstat")
	for _, want := range []string{"cmd/csvstat", "func main()", "no function main"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
}

func TestMainPackageWithMainIsFine(t *testing.T) {
	d := mainDoc(t, "cmd/csvstat", "func run(args []string) int", "func main()")
	if got := mainDefects(d); len(got) != 0 {
		t.Fatalf("defects = %v", got)
	}
}

func TestMainWithWrongSignatureIsADefect(t *testing.T) {
	for _, sig := range []string{"func main(args []string)", "func main() int", "func (s S) main()"} {
		d := mainDoc(t, "cmd/csvstat", "func run() int", sig)
		if got := mainDefects(d); len(got) != 1 {
			t.Errorf("%q: defects = %v", sig, got)
		}
	}
}

func TestNonMainPackageIsUnaffected(t *testing.T) {
	d := mainDoc(t, "internal/stats", "func Summarize() int")
	if got := mainDefects(d); len(got) != 0 {
		t.Fatalf("defects = %v", got)
	}
}

func TestMainPackageWithoutFunctionsIsUnaffected(t *testing.T) {
	d := mainDoc(t, "cmd/csvstat")
	if got := mainDefects(d); len(got) != 0 {
		t.Fatalf("defects = %v", got)
	}
}

func TestModuleRootMainPackage(t *testing.T) {
	d := mainDoc(t, ".", "func run() int")
	got := mainDefects(d)
	if len(got) != 1 || got[0] != "." {
		t.Fatalf("defects = %v", got)
	}
	if mainDefects(mainDoc(t, ".", "func run() int", "func main()")) != nil {
		t.Error("root with main reported")
	}
}

func TestMainDefectTextHasNoReplyText(t *testing.T) {
	d := mainDoc(t, "cmd/csvstat", "func run(args []string) int /* SECRET REPLY */")
	for _, p := range mainDefects(d) {
		if strings.Contains(mainDefectText(p), "SECRET") {
			t.Error("reply text in message")
		}
	}
}
