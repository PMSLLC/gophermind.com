package planner

import (
	"reflect"
	"strings"
	"testing"
)

func TestCompileDiagsKeepsOnlyTheTestFileInOrder(t *testing.T) {
	out := "# m/p\np/zz_other0_test.go:3:1: other\n./p/fn_x_test.go:9:2: declared and not used: x\np/fn_x_test.go:9:9: declared and not used: y\np/fn_x_test.go:9:12: declared and not used: y\np/fn_x_test.go:4:2: \"fmt\" imported and not used\n"
	got := compileDiags("fn_x_test.go", out)
	want := []compileDiag{{9, 2, "declared and not used: x"}, {9, 9, "declared and not used: y"}, {4, 2, `"fmt" imported and not used`}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestRefuseCompileIsUnchangedByTheSplit(t *testing.T) {
	c := compileCheckContract()
	src := "package stats\n\nfunc f() {\n\tif next, ok := m.redirects[req.URL.String()]; ok {\n\t}\n}\n"
	out := "./fn_x_test.go:4:5: declared and not used: next\n"
	want := "test file does not compile against the contract: " +
		compileMessage(c, "internal/stats", src, "\tif next, ok := m.redirects[req.URL.String()]; ok {", 4, "declared and not used: next")
	err := refuseCompile(c, "internal/stats", "fn_x_test.go", src, out)
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
	if !strings.Contains(want, "unused variable at line 4") {
		t.Fatalf("pinned text changed: %q", want)
	}
	if err := refuseCompile(c, "internal/stats", "fn_x_test.go", src, "no required module provides x\n"+out); err != nil {
		t.Fatalf("module lookup must stay nil, got %v", err)
	}
}
