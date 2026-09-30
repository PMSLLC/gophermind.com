package planner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDerivedTestNames(t *testing.T) {
	cases := []struct{ id, file, wantFunc, wantPath, wantCmd string }{
		{"fn-validate-email", "internal/validation/email.go", "TestValidateEmail", "internal/validation/fn_validate_email_test.go", "go test ./internal/validation -run ^TestValidateEmail$"},
		{"fn-name-error-error", "internal/greet/errors.go", "TestNameErrorError", "internal/greet/fn_name_error_error_test.go", "go test ./internal/greet -run ^TestNameErrorError$"},
		{"fn-main", "main.go", "TestMain", "fn_main_test.go", "go test . -run ^TestMain$"},
		{"fn-2fa-check", "internal/auth/twofa.go", "Test2faCheck", "internal/auth/fn_2fa_check_test.go", "go test ./internal/auth -run ^Test2faCheck$"},
	}
	for _, c := range cases {
		if got := testFuncName(c.id); got != c.wantFunc {
			t.Errorf("testFuncName(%s) = %s, want %s", c.id, got, c.wantFunc)
		}
		if got := testFilePath(c.id, c.file); got != c.wantPath {
			t.Errorf("testFilePath(%s) = %s, want %s", c.id, got, c.wantPath)
		}
		if got := testCommand(c.file, c.wantFunc); got != c.wantCmd {
			t.Errorf("testCommand(%s) = %q, want %q", c.id, got, c.wantCmd)
		}
	}
}

func TestSafeTestPath(t *testing.T) {
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "internal", "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "internal", "escape")); err != nil {
		t.Skipf("cannot make a symlink here: %v", err)
	}

	good := []string{"a_test.go", "internal/real/x_test.go", "internal/not/yet/made/x_test.go"}
	for _, rel := range good {
		abs, err := safeTestPath(repo, rel)
		if err != nil || abs != filepath.Join(repo, filepath.FromSlash(rel)) {
			t.Errorf("safeTestPath(%q) = %q, %v", rel, abs, err)
		}
	}
	if exists(filepath.Join(repo, "internal", "not")) {
		t.Error("safeTestPath created a directory; it must only answer")
	}
	bad := map[string]string{
		"../x_test.go":                     "not a clean path",
		"internal/../../x_test.go":         "not a clean path",
		"/etc/x_test.go":                   "not a clean path",
		`internal\x_test.go`:               "not a clean path",
		"":                                 "not a clean path",
		"internal/real/x.go":               "does not end in _test.go",
		"internal/escape/x_test.go":        "resolves outside the repository",
		"internal/escape/deeper/x_test.go": "resolves outside the repository",
	}
	for rel, want := range bad {
		if _, err := safeTestPath(repo, rel); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("safeTestPath(%q) = %v, want an error containing %q", rel, err, want)
		}
	}
}

const okTestSource = `package greet

import (
	"strings"
	"testing"

	"example.com/greeter/internal/names"
)

func TestGreet(t *testing.T) {
	_ = strings.TrimSpace(names.Default)
}
`

func TestCheckTestSource(t *testing.T) {
	if err := checkTestSource(okTestSource, "greet", "TestGreet", "example.com/greeter"); err != nil {
		t.Fatalf("the good file was refused: %v", err)
	}
	external := strings.Replace(okTestSource, "package greet\n", "package greet_test\n", 1)
	if err := checkTestSource(external, "greet", "TestGreet", "example.com/greeter"); err != nil {
		t.Errorf("an external test package was refused: %v", err)
	}
	bad := []struct{ name, edit, with, want string }{
		{"not Go", "package greet", "pakage greet", "not valid Go"},
		{"another package", "package greet\n", "package other\n", "is not in package greet"},
		{"a third-party import", `"strings"`, `"github.com/stretchr/testify/assert"`, "imports a package (34 bytes) outside"},
		{"another module that shares the prefix", `example.com/greeter/internal/names`, `example.com/greeter2/internal/names`, "only the standard library and this module"},
		{"an extended library import", `"strings"`, `"golang.org/x/text/cases"`, "imports a package (23 bytes) outside"},
		{"the test function is missing", "func TestGreet(", "func TestOther(", "has no func TestGreet(t *testing.T)"},
		{"the test function has the wrong shape", "func TestGreet(t *testing.T)", "func TestGreet(t *testing.B)", "not as func TestGreet(t *testing.T)"},
		{"the name is only a method", "func TestGreet(t *testing.T)", "func (x X) TestGreet(t *testing.T)", "has no func TestGreet"},
	}
	for _, b := range bad {
		t.Run(b.name, func(t *testing.T) {
			src := strings.Replace(okTestSource, b.edit, b.with, 1)
			if err := checkTestSource(src, "greet", "TestGreet", "example.com/greeter"); err == nil || !strings.Contains(err.Error(), b.want) {
				t.Errorf("err = %v, want it to contain %q", err, b.want)
			}
		})
	}
}

// At least the happy path and one test per error condition: fewer tests than
// errors + 1 is refused.
func TestTestwriterRepliesNeedATestPerErrorCondition(t *testing.T) {
	ct := map[string]any{"file": "internal/greet/greet.go", "errors": []any{
		map[string]any{"when": "name is empty", "returns": "*NameError"},
		map[string]any{"when": "name is too long", "returns": "*NameError"},
	}}
	src := strings.ReplaceAll(okTestSource, "\n\t\"example.com/greeter/internal/names\"\n", "")
	src = strings.Replace(src, "names.Default", `"x"`, 1)
	test := func(name string) string {
		return `{"name": "` + name + `", "given": "g", "expect": "e", "level": "acceptance", "command": "rm -rf /"}`
	}
	reply := func(tests ...string) string {
		quoted, _ := jsonString(src)
		return `{"tests": [` + strings.Join(tests, ",") + `], "test_file": ` + quoted + `}`
	}

	_, _, err := parseTestwrite(reply(test("a"), test("b")), ct, "greet", "TestGreet", "example.com/greeter")
	if err == nil || !strings.Contains(err.Error(), "has 2 tests; the contract lists 2 error conditions, so at least 3 are needed") {
		t.Fatalf("two tests for two error conditions: err = %v", err)
	}
	tests, got, err := parseTestwrite(reply(test("a"), test("b"), test("c")), ct, "greet", "TestGreet", "example.com/greeter")
	if err != nil {
		t.Fatalf("three tests were refused: %v", err)
	}
	if got != src || len(tests) != 3 {
		t.Errorf("parseTestwrite returned %d tests", len(tests))
	}
	// Level and command are the harness's, whatever the model wrote.
	for _, tc := range tests {
		if tc["level"] != "unit" || tc["command"] != "go test ./internal/greet -run ^TestGreet$" {
			t.Errorf("test %v = level %v, command %v", tc["name"], tc["level"], tc["command"])
		}
	}
	for name, bad := range map[string]string{
		"no name":   `{"name": " ", "given": "g", "expect": "e"}`,
		"no given":  `{"name": "a", "expect": "e"}`,
		"no expect": `{"name": "a", "given": "g", "expect": ""}`,
	} {
		if _, _, err := parseTestwrite(reply(test("a"), test("b"), bad), ct, "greet", "TestGreet", "example.com/greeter"); err == nil || !strings.Contains(err.Error(), "needs a name, a given and an expect") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, _, err := parseTestwrite("not json", ct, "greet", "TestGreet", "example.com/greeter"); err == nil {
		t.Error("a reply that is not JSON was accepted")
	}
}

func jsonString(s string) (string, error) {
	raw, err := json.Marshal(s)
	return string(raw), err
}

// No error a Test-writer parse path returns may quote the reply. Every case
// must fail, and the canary planted in it must not appear in the error.
func TestTestwriterErrorsNeverQuoteTheReply(t *testing.T) {
	const canary = "CANARY-4c81e0"
	ct := map[string]any{"file": "internal/greet/greet.go", "errors": []any{map[string]any{"when": "empty", "returns": "*NameError"}}}
	tests := `[{"name": "a", "given": "g", "expect": "e"}, {"name": "b", "given": "g", "expect": "e"}]`
	src := func(edit func(string) string) string {
		s, _ := jsonString(edit(okTestSource))
		return s
	}
	same := func(s string) string { return s }
	withFile := func(file string) string { return `{"tests": ` + tests + `, "test_file": ` + file + `}` }
	sub := func(from, to string) string {
		return withFile(src(func(s string) string { return strings.Replace(s, from, to, 1) }))
	}
	replies := map[string]string{
		"prose":                 "prose " + canary,
		"an object of canaries": `{"` + canary + `": 1}`,
		"truncated":             `{"tests": [{"name": "` + canary,
		"tests of wrong type":   `{"tests": "` + canary + `", "test_file": "x"}`,
		"test_file wrong type":  `{"tests": ` + tests + `, "test_file": {"` + canary + `": 1}}`,
		"an array":              `["` + canary + `"]`,
		"no name":               `{"tests": [{"name": " ", "given": "` + canary + `", "expect": "e"}, {"name": "b", "given": "g", "expect": "e"}], "test_file": ` + src(same) + `}`,
		"not Go":                withFile(`"` + canary + ` {"`),
		"not Go, valid start":   sub("func TestGreet(t *testing.T) {", "func TestGreet(t *testing.T) { "+canary+" +"),
		"another package":       sub("package greet", "package "+canary),
		"third-party import":    sub(`"strings"`, `"github.com/`+canary+`/x"`),
		"import of a lookalike": sub(`example.com/greeter/internal/names`, `example.com/greeter`+canary+`/internal/names`),
		"function missing":      sub("func TestGreet(", "func Test"+canary+"("),
		"function wrong shape":  sub("func TestGreet(t *testing.T)", "func TestGreet("+canary+" *testing.B)"),
	}
	for name, reply := range replies {
		t.Run(name, func(t *testing.T) {
			_, _, err := parseTestwrite(reply, ct, "greet", "TestGreet", "example.com/greeter")
			if err == nil {
				t.Fatal("the reply was accepted")
			}
			if strings.Contains(err.Error(), canary) {
				t.Errorf("the error quotes the reply: %v", err)
			}
		})
	}

	// A path taken from a contract is reply text too.
	repo := t.TempDir()
	for _, rel := range []string{"../" + canary + "_test.go", "/" + canary + "_test.go", canary + "/x.go", canary + `\x_test.go`} {
		if _, err := safeTestPath(repo, rel); err == nil || strings.Contains(err.Error(), canary) {
			t.Errorf("safeTestPath(%q) = %v", rel, err)
		}
	}
}

func TestCheckTestSourceImportsAreStdOrOwnModule(t *testing.T) {
	const mod = "example.com/greeter"
	src := func(imp string) string {
		return "package greet\n\nimport (\n\t\"testing\"\n\t_ \"" + imp + "\"\n)\n\nfunc TestGreet(t *testing.T) {}\n"
	}
	for _, ok := range []string{"fmt", "net/http/httptest", "testing", "encoding/json", mod, mod + "/internal/names"} {
		if err := checkTestSource(src(ok), "greet", "TestGreet", mod); err != nil {
			t.Errorf("%s was refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"internal/foo", "foo/bar", "foo", "vendor/x", "internal", "myreplace", "example.com/other"} {
		if err := checkTestSource(src(bad), "greet", "TestGreet", mod); err == nil {
			t.Errorf("%s was accepted", bad)
		}
	}
}

func TestSafeTestPathRefusesASymlinkedParentInsideTheRepo(t *testing.T) {
	repo, _ := filepath.EvalSymlinks(t.TempDir())
	if err := os.MkdirAll(filepath.Join(repo, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repo, "real"), filepath.Join(repo, "link")); err != nil {
		t.Skip(err)
	}
	if _, err := safeTestPath(repo, "link/x_test.go"); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Errorf("err = %v", err)
	}
}
