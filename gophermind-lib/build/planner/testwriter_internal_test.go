package planner

import (
	"encoding/json"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/pathsafe"
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
	pc := map[string]string{"a": `"polarity": "success", "covers": "happy"`, "b": `"polarity": "negative", "covers": "error:1"`, "c": `"polarity": "negative", "covers": "error:2"`}
	test := func(name string) string {
		return `{"name": "` + name + `", "given": "g", "expect": "e", "level": "acceptance", "command": "rm -rf /", ` + pc[name] + `}`
	}
	reply := func(tests ...string) string {
		quoted, _ := jsonString(src)
		return `{"tests": [` + strings.Join(tests, ",") + `], "test_file": ` + quoted + `}`
	}

	_, _, err := parseTestwrite(reply(test("a"), test("b")), ct, "greet", "TestGreet", "example.com/greeter", nil)
	if err == nil || !strings.Contains(err.Error(), "has 2 tests; the contract lists 2 error conditions, so at least 3 are needed") {
		t.Fatalf("two tests for two error conditions: err = %v", err)
	}
	tests, got, err := parseTestwrite(reply(test("a"), test("b"), test("c")), ct, "greet", "TestGreet", "example.com/greeter", nil)
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
	for name, bad := range map[string][2]string{
		"no name":   {`{"name": " ", "given": "g", "expect": "e"}`, "test 3 needs a name"},
		"no given":  {`{"name": "a", "expect": "e"}`, "test 3: given is missing"},
		"no expect": {`{"name": "a", "given": "g"}`, "test 3: expect is missing"},
	} {
		if _, _, err := parseTestwrite(reply(test("a"), test("b"), bad[0]), ct, "greet", "TestGreet", "example.com/greeter", nil); err == nil || err.Error() != bad[1] {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, _, err := parseTestwrite("not json", ct, "greet", "TestGreet", "example.com/greeter", nil); err == nil {
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
			_, _, err := parseTestwrite(reply, ct, "greet", "TestGreet", "example.com/greeter", nil)
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
		if _, err := pathsafe.ResolveTest(repo, rel); err == nil || strings.Contains(err.Error(), canary) {
			t.Errorf("pathsafe.ResolveTest(%q) = %v", rel, err)
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

func TestCheckTestSourcePackageClause(t *testing.T) {
	const mod = "example.com/greeter"
	cases := []struct {
		pkg, clause string
		ok          bool
	}{
		{"internal/stats", "stats", true},
		{"internal/stats", "stats_test", true},
		{"internal/stats", "other", false},
		{"greet", "greet", true},
		{"greet", "greet_test", true},
		{"greet", "other", false},
		{"cmd/csvstat", "main", true},
		{"cmd/csvstat", "main_test", true},
		{"cmd/csvstat", "csvstat", true},
		{".", "main", true},
		{"", "main", true},
		{"internal/stats", "main", false},
		{"greet", "main", false},
	}
	for _, c := range cases {
		src := strings.Replace(okTestSource, "package greet\n", "package "+c.clause+"\n", 1)
		err := checkTestSource(src, c.pkg, "TestGreet", mod)
		if c.ok && err != nil {
			t.Errorf("package %q, clause %q was refused: %v", c.pkg, c.clause, err)
		}
		if !c.ok && (err == nil || !strings.Contains(err.Error(), "is not in package")) {
			t.Errorf("package %q, clause %q: err = %v, want a package refusal", c.pkg, c.clause, err)
		}
	}
}

func TestPackageClause(t *testing.T) {
	for pkg, want := range map[string]string{
		"internal/stats": "stats", "internal/render": "render", "greet": "greet",
		"cmd/csvstat": "main", "cmd": "main", ".": "main", "": "main", "./": "main",
		"cmd/csvstat/sub": "main", "pkg/cmdx": "cmdx",
	} {
		if got := PackageClause(pkg); got != want {
			t.Errorf("PackageClause(%q) = %q, want %q", pkg, got, want)
		}
	}
}

// A model writing a test for a function that returns a bool or a number puts
// the bare value in given or expect. A boolean or number is kept as its text;
// null and an empty object or array are refused, blank text is kept as written; a non-empty object or array is kept as compact JSON text, naming the test and the field.
func TestTestwriterAcceptsScalarGivenAndExpect(t *testing.T) {
	ct := map[string]any{"file": "internal/greet/greet.go"}
	src := strings.ReplaceAll(okTestSource, "\n\t\"example.com/greeter/internal/names\"\n", "")
	src = strings.Replace(src, "names.Default", `"x"`, 1)
	quoted, _ := jsonString(src)
	reply := func(given, expect string) string {
		return `{"tests": [{"name": "a", "given": ` + given + `, "expect": ` + expect + `, "polarity": "success", "covers": "happy"}], "test_file": ` + quoted + `}`
	}
	accepted := []struct{ given, expect, wantGiven, wantExpect string }{
		{`"g"`, `true`, "g", "true"},
		{`"g"`, `false`, "g", "false"},
		{`"g"`, `3`, "g", "3"},
		{`"g"`, `2.5`, "g", "2.5"},
		{`1`, `"e"`, "1", "e"},
		{`"g"`, `9007199254740993`, "g", "9007199254740993"},
		{`"g"`, `"e"`, "g", "e"},
		{`"g"`, `""`, "g", ""},
		{`"g"`, `"  "`, "g", "  "},
		{`""`, `"e"`, "", "e"},
		{`" "`, `""`, " ", ""},
		{`"g"`, `{"a": 1}`, "g", `{"a":1}`},
		{`"g"`, `[true]`, "g", `[true]`},
		{`{"b": 9007199254740993, "a": "x\ny"}`, `[1, 2.50]`, `{"a":"x\ny","b":9007199254740993}`, `[1,2.50]`},
	}
	for _, c := range accepted {
		tests, _, err := parseTestwrite(reply(c.given, c.expect), ct, "greet", "TestGreet", "example.com/greeter", nil)
		if err != nil {
			t.Errorf("given %s expect %s: refused: %v", c.given, c.expect, err)
			continue
		}
		if tests[0]["given"] != c.wantGiven || tests[0]["expect"] != c.wantExpect {
			t.Errorf("given %s expect %s: stored %q and %q, want %q and %q", c.given, c.expect, tests[0]["given"], tests[0]["expect"], c.wantGiven, c.wantExpect)
		}
	}
	refused := []struct{ given, expect, want string }{
		{`"g"`, `null`, "test 1: expect must be text"},
		{`"g"`, `{}`, "test 1: expect must be text"},
		{`"g"`, `[]`, "test 1: expect must be text"},
		{`null`, `"e"`, "test 1: given must be text"},
		{`{}`, `"e"`, "test 1: given must be text"},
		{`[]`, `"e"`, "test 1: given must be text"},
	}
	for _, c := range refused {
		_, _, err := parseTestwrite(reply(c.given, c.expect), ct, "greet", "TestGreet", "example.com/greeter", nil)
		if err == nil || err.Error() != c.want {
			t.Errorf("given %s expect %s: err = %v, want %q", c.given, c.expect, err, c.want)
		}
	}
	noName := `{"tests": [{"name": " ", "given": "g", "expect": "e"}], "test_file": ` + quoted + `}`
	if _, _, err := parseTestwrite(noName, ct, "greet", "TestGreet", "example.com/greeter", nil); err == nil || err.Error() != "test 1 needs a name" {
		t.Errorf("blank name: err = %v", err)
	}
}

// A function with several arguments: the model wrote given and expect as JSON
// objects of argument values. The tests list is for people; test_file is the
// executable test, so the objects are kept as compact JSON text.
func TestTestwriterAcceptsObjectGivenAndExpect(t *testing.T) {
	ct := map[string]any{"file": "internal/greet/greet.go"}
	src := strings.ReplaceAll(okTestSource, "\n\t\"example.com/greeter/internal/names\"\n", "")
	src = strings.Replace(src, "names.Default", `"x"`, 1)
	quoted, _ := jsonString(src)
	text := `{"tests": [{"name": "reads rows", "given": {"r": "name,age\nAlice,30", "maxRows": 0}, "expect": {"rows": [["Alice", "30"]], "err": null}, "polarity": "success", "covers": "happy"}], "test_file": ` + quoted + `}`
	tests, _, err := parseTestwrite(text, ct, "greet", "TestGreet", "example.com/greeter", nil)
	if err != nil {
		t.Fatalf("object given and expect were refused: %v", err)
	}
	if want := `{"maxRows":0,"r":"name,age\nAlice,30"}`; tests[0]["given"] != want {
		t.Errorf("given = %q, want %q", tests[0]["given"], want)
	}
	if want := `{"err":null,"rows":[["Alice","30"]]}`; tests[0]["expect"] != want {
		t.Errorf("expect = %q, want %q", tests[0]["expect"], want)
	}
}

// The real reply that excluded the only model: a boundary test of a render
// function whose expected output is empty because nothing is printed.
func TestTestwriterAcceptsEmptyExpectBoundaryTest(t *testing.T) {
	ct := map[string]any{"file": "internal/greet/greet.go"}
	src := strings.ReplaceAll(okTestSource, "\n\t\"example.com/greeter/internal/names\"\n", "")
	src = strings.Replace(src, "names.Default", `"x"`, 1)
	quoted, _ := jsonString(src)
	text := `{"tests": [{"name":"happy","given":{"summaries":["a"]},"expect":"a","polarity":"success","covers":"happy"},{"name":"boundary_empty_slice","given":{"summaries":[]},"expect":"","polarity":"boundary","covers":"input:summaries"}], "test_file": ` + quoted + `}`
	ct["errors"] = []any{}
	tests, _, err := parseTestwrite(text, ct, "greet", "TestGreet", "example.com/greeter", nil)
	if err != nil {
		t.Fatalf("empty expect refused: %v", err)
	}
	if tests[1]["expect"] != "" || tests[1]["given"] != `{"summaries":[]}` {
		t.Errorf("stored given %q expect %q", tests[1]["given"], tests[1]["expect"])
	}
}

// The local model leaves a "given" string unclosed on a pretty-printed line.
func TestParseTestwriteClosesAStrayString(t *testing.T) {
	ct := map[string]any{"file": "internal/greet/greet.go", "errors": []any{
		map[string]any{"when": "name is empty", "returns": "*NameError"},
		map[string]any{"when": "name is too long", "returns": "*NameError"},
	}}
	src := strings.ReplaceAll(okTestSource, "\n\t\"example.com/greeter/internal/names\"\n", "")
	src = strings.Replace(src, "names.Default", `"x"`, 1)
	quoted, _ := jsonString(src)
	long := strings.Repeat("ab", 5000)
	pol := map[string]string{"a": "success", "b": "negative", "c": "negative"}
	cov := map[string]string{"a": "happy", "b": "error:1", "c": "error:2"}
	build := func(givenLine string) string {
		var parts []string
		for _, n := range []string{"a", "b", "c"} {
			given := `    "given": "g",`
			if n == "a" {
				given = givenLine
			}
			parts = append(parts, "  {\n    \"name\": \""+n+"\",\n"+given+"\n    \"expect\": \"e\",\n    \"polarity\": \""+pol[n]+"\",\n    \"covers\": \""+cov[n]+"\"\n  }")
		}
		return "{\n\"tests\": [\n" + strings.Join(parts, ",\n") + "\n],\n\"test_file\": " + quoted + "\n}"
	}
	slip := `    "given": "value: \"` + long + `\",`
	call := func(r string) ([]map[string]any, error) {
		tests, _, err := parseTestwrite(r, ct, "greet", "TestGreet", "example.com/greeter", nil)
		return tests, err
	}

	tests, err := call(build(slip))
	if err != nil {
		t.Fatalf("the stray string was not closed: %v", err)
	}
	if want := `value: "` + long + `"`; tests[0]["given"] != want {
		t.Errorf("given = %.40q, want value: \"abab...\"", tests[0]["given"])
	}

	// A well-formed reply is left alone.
	good := build(`    "given": "g",`)
	if fixed, changed := closeStrayStrings(good); changed || fixed != good {
		t.Error("a well-formed reply was touched")
	}
	// An even quote count is untouched.
	even := `    "given": "value: \"x\"",`
	if fixed, changed := closeStrayStrings(even); changed || fixed != even {
		t.Errorf("an even line was changed to %q", fixed)
	}
	// A different defect keeps the original message.
	missing := strings.Replace(good, "\"expect\": \"e\",\n    \"polarity\": \"success\"", "\"expect\": \"e\"\n    \"polarity\": \"success\"", 1)
	if _, err := call(missing); err == nil || !strings.Contains(err.Error(), "a comma is missing") {
		t.Errorf("missing comma: err = %v", err)
	}
	// A repair that does not make the reply decode reports the original error.
	broken := strings.Replace(build(slip), "\"name\": \"b\"", "\"name\": b", 1)
	if _, err := call(broken); err == nil || !strings.Contains(err.Error(), "line 5,") {
		t.Errorf("unrepairable reply: err = %v", err)
	}
}
