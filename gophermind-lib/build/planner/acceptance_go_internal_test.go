package planner

import (
	"strings"
	"testing"
)

const goodAcceptSrc = `package acceptance

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func a4get(t *testing.T, path string) string {
	t.Helper()
	base := os.Getenv("GM_ACCEPTANCE_URL")
	if base == "" {
		t.Fatal("GM_ACCEPTANCE_URL is not set")
	}
	resp, err := http.Get(base + path)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	return string(b)
}

func TestA4(t *testing.T) {
	if !strings.Contains(a4get(t, "/hello"), "Hello") {
		t.Fatal("no greeting")
	}
}
`

var a4 = goBullet{Requirement: "A4", Func: "TestA4", File: "acceptance/a4_test.go", Command: GoAcceptanceCommand("TestA4")}

func TestCheckAcceptanceSourceAcceptsTheGoodFile(t *testing.T) {
	out, err := checkAcceptanceSource(goodAcceptSrc, a4)
	if err != nil {
		t.Fatalf("the good file was refused: %v", err)
	}
	if !strings.HasPrefix(out, "//go:build acceptance\n\npackage acceptance\n") {
		t.Errorf("the build tag was not put first:\n%.80s", out)
	}
	// a tag the model wrote is replaced, never doubled
	again, err := checkAcceptanceSource("//go:build integration\n\n"+goodAcceptSrc, a4)
	if err != nil || again != out {
		t.Errorf("a model-written build line was kept (%v)", err)
	}
}

func TestCheckAcceptanceSourceRefusals(t *testing.T) {
	const canary = "REPLYCANARY"
	cases := []struct{ name, from, to string }{
		{"not Go", "package acceptance", "pakage acceptance"},
		{"wrong package", "package acceptance\n", "package other\n"},
		{"third party import", `"strings"`, `"github.com/stretchr/testify/assert"`},
		{"os/exec", `"strings"`, `"os/exec"`},
		{"httptest starts a server", `"strings"`, `"net/http/httptest"`},
		{"syscall", `"strings"`, `"syscall"`},
		{"the module's own package", `"strings"`, `"example.com/greeter/internal/greet"`},
		{"missing test function", "func TestA4(", "func TestOther("},
		{"wrong signature", "func TestA4(t *testing.T)", "func TestA4(t *testing.B)"},
		{"a skip", `t.Fatal("no greeting")`, `t.Skip("later")`},
		{"skip now", `t.Fatal("no greeting")`, `t.SkipNow()`},
		{"does not read the url", `os.Getenv("GM_ACCEPTANCE_URL")`, `os.Getenv("OTHER")`},
		{"a second test", "func a4get(", "func TestA4Extra(t *testing.T) {}\n\nfunc a4get("},
		{"a helper without the prefix", "func a4get(", "func get("},
		{"TestMain", "func a4get(", "func TestMain(m *testing.M) {}\n\nfunc a4get("},
		{"init", "func a4get(", "func init() {}\n\nfunc a4get("},
		{"a global without the prefix", "func a4get(", "var shared = 1\n\nfunc a4get("},
	}
	for _, c := range cases {
		src := strings.Replace(goodAcceptSrc, c.from, c.to, 1)
		if src == goodAcceptSrc {
			t.Fatalf("%s: the edit did nothing", c.name)
		}
		src += "\n// " + canary + "\n"
		_, err := checkAcceptanceSource(src, a4)
		if err == nil {
			t.Errorf("%s: want an error", c.name)
			continue
		}
		if strings.Contains(err.Error(), canary) {
			t.Errorf("%s: the error quotes the reply: %v", c.name, err)
		}
	}
	noFatal := strings.NewReplacer("t.Fatalf(", "t.Logf(", "t.Fatal(", "t.Log(").Replace(goodAcceptSrc)
	if _, err := checkAcceptanceSource(noFatal, a4); err == nil {
		t.Error("a test that cannot fail was accepted")
	}
	big := goodAcceptSrc + "// " + strings.Repeat("x", 70000) + "\n"
	if _, err := checkAcceptanceSource(big, a4); err == nil {
		t.Error("a file over the size cap was accepted")
	}
}

func TestGoBulletsAndRootTests(t *testing.T) {
	reqs := []Requirement{
		{ID: "A1", Kind: ReqAcceptance, Text: "`go build ./...` succeeds."},
		{ID: "A4", Kind: ReqAcceptance, Text: "A round trip returns 200."},
		{ID: "C1", Kind: ReqConstraint, Text: "Narrative constraint."},
	}
	if got := goBullets(reqs, nil); len(got) != 0 {
		t.Errorf("without serve: %v", got)
	}
	serve := &Serve{Command: "greeter", Ready: "/healthz"}
	got := goBullets(reqs, serve)
	if len(got) != 1 || got[0] != a4 {
		t.Fatalf("bullets = %+v", got)
	}
	if got[0].Command != "go test -tags acceptance ./acceptance -run '^TestA4$' -count=1 -v" {
		t.Errorf("command = %q", got[0].Command)
	}
	reply := CoverageReply{Serve: serve, RootTests: []RootTest{
		{Requirement: "A4", Name: "own name", Given: "g", Expect: "e", Command: "curl -s localhost:1"},
		{Requirement: "A4", Name: "second", Command: "echo hi"},
		{Requirement: "A1", Name: "builds", Command: "go build ./..."},
	}}
	out := reply.withGoAcceptance(reqs)
	if len(out.RootTests) != 2 || out.RootTests[0].Command != a4.Command || out.RootTests[0].Name != "own name" || out.RootTests[1].Command != "go build ./..." {
		t.Errorf("root tests = %+v", out.RootTests)
	}
	none := CoverageReply{Serve: serve}.withGoAcceptance(reqs)
	if len(none.RootTests) != 1 || none.RootTests[0].Command != a4.Command {
		t.Errorf("a bullet with no root test got %+v", none.RootTests)
	}
	if r := (CoverageReply{}).withGoAcceptance(reqs); len(r.RootTests) != 0 {
		t.Error("a reply without serve was changed")
	}
}

func TestServeChecks(t *testing.T) {
	for _, s := range []Serve{{"", "/x"}, {"a", ""}, {"a", "healthz"}, {"a\nb", "/x"}, {"a", "/x y"}, {strings.Repeat("a", 401), "/x"}} {
		if err := s.check(); err == nil {
			t.Errorf("%+v was accepted", s)
		}
	}
	if err := (&Serve{Command: "greeter --addr \"$GM_ACCEPTANCE_ADDR\"", Ready: "/healthz"}).check(); err != nil {
		t.Error(err)
	}
}
