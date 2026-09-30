package packer

import (
	"reflect"
	"strings"
	"testing"
)

func TestImportPolicy(t *testing.T) {
	p := ImportPolicy{Module: "example.com/greeter", Deps: []string{"github.com/x/y"}}
	allowed := []string{"fmt", "net/http", "encoding/json", "example.com/greeter", "example.com/greeter/internal/store", "github.com/x/y", "github.com/x/y/sub"}
	denied := []string{"os/exec", "syscall", "unsafe", "plugin", "runtime/cgo", "debug/elf", "internal/abi", "golang.org/x/sys/unix", "github.com/x/yy", "github.com/other/z", "example.com/greeterx", "C",
		"net/http/internal", "vendor/golang.org/x/net", "", "./local", "notstd"}
	for _, a := range allowed {
		if got := p.Check([]string{a}); len(got) != 0 {
			t.Errorf("%q should be allowed, got %v", a, got)
		}
	}
	for _, d := range denied {
		if got := p.Check([]string{d}); !reflect.DeepEqual(got, []string{d}) {
			t.Errorf("%q should be denied, got %v", d, got)
		}
	}
	got := p.Check([]string{"syscall", "fmt", "C", "syscall", "unsafe", "github.com/other/z"})
	want := []string{"C", "github.com/other/z", "syscall", "unsafe"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sorted dedup: got %v want %v", got, want)
	}
	d := ImportPolicy{Module: "greeter"}
	if len(d.Check([]string{"greeter/store", "greeter"})) != 0 {
		t.Error("dotless own module denied")
	}
	if got := d.Check([]string{"greeter2"}); len(got) != 1 {
		t.Errorf("greeter2 must be denied: %v", got)
	}
}

func TestImportPolicyAllowedList(t *testing.T) {
	p := ImportPolicy{Module: "example.com/greeter", Deps: []string{"github.com/x/y", "github.com/a/b"}}
	s := p.AllowedList()
	for _, w := range []string{"example.com/greeter/...", "github.com/x/y/...", "github.com/a/b/...", "os/exec", "syscall", "unsafe", "plugin", "debug/*", "runtime/cgo", "standard library"} {
		if !strings.Contains(s, w) {
			t.Errorf("allowed list missing %q: %s", w, s)
		}
	}
	if strings.Contains(s, "CANARY") {
		t.Error("canary")
	}
}
