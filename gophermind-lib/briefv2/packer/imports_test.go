package packer

import (
	"os"
	"os/exec"
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

func TestImportPathSyntax(t *testing.T) {
	p := ImportPolicy{Module: "example.com/greeter", Deps: []string{"github.com/x/y"}}
	bad := []string{"fmt/../os/exec", "example.com/greeter/../../evil", "fmt/x", "fmt//x", "/fmt", "fmt/", "./fmt", "fmt/./x",
		"a\\b", "fmt x", "fmt\n", "github.com/x/y/internal/z", "github.com/x/y/../../q", "net/http/nosuch", "os/nosuch", "example.com/greeter/x y"}
	for _, b := range bad {
		if got := p.Check([]string{b}); len(got) != 1 {
			t.Errorf("%q must be refused", b)
		}
	}
	for _, g := range []string{"uuid", "encoding/json", "net/http", "example.com/greeter/internal/x", "github.com/x/y/sub"} {
		if got := p.Check([]string{g}); len(got) != 0 {
			t.Errorf("%q must be allowed: %v", g, got)
		}
	}
}

func TestStdListMatchesToolchain(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go not on PATH")
	}
	out, err := exec.Command(goBin, "list", "std").Output()
	if err != nil {
		t.Skip("go list std failed")
	}
	want := map[string]bool{}
	for _, l := range strings.Fields(string(out)) {
		if !strings.Contains("/"+l+"/", "/internal/") && !strings.Contains("/"+l+"/", "/vendor/") {
			want[l] = true
		}
	}
	for l := range want {
		if !stdPackages[l] {
			t.Errorf("stdlist.txt lacks %s; regenerate with go list std", l)
		}
	}
	ver, err := exec.Command(goBin, "version").Output()
	recorded, rerr := os.ReadFile("stdlist.version")
	if err == nil && rerr == nil {
		if f := strings.Fields(string(ver)); len(f) >= 3 && f[2] == strings.TrimSpace(string(recorded)) {
			for l := range stdPackages {
				if !want[l] {
					t.Errorf("stdlist.txt has %s which go list std does not; regenerate", l)
				}
			}
		}
	}
	if !stdPackages["uuid"] {
		t.Error("uuid missing")
	}
}
