package plan

import (
	"errors"
	"strings"
	"testing"
)

const goodReply = `Here is the plan.
` + "```json" + `
{"phases":[{"title":"Foundation","digest":"Everything else rests on this.","objective":"Set up the base.",
 "tasks":[{"title":"Repo layout","digest":"Where code lives.","objective":"",
  "steps":[{"title":"Create the module","digest":"Needed to compile anything."}]}]}],
 "overview":"A small project."}
` + "```" + `
Hope that helps.`

func TestExtractJSON(t *testing.T) {
	got, err := ExtractJSON(`noise {"a":"}{","b":{"c":1}} trailing {"x":2}`)
	if err != nil || got != `{"a":"}{","b":{"c":1}}` {
		t.Errorf("ExtractJSON = %q, %v", got, err)
	}
	got, err = ExtractJSON(`{"quote":"say \"hi\" {"}`)
	if err != nil || got != `{"quote":"say \"hi\" {"}` {
		t.Errorf("escaped quote: %q, %v", got, err)
	}
	for _, bad := range []string{"no json here", `{"open":1`, ""} {
		if _, err := ExtractJSON(bad); err == nil {
			t.Errorf("ExtractJSON(%q) should fail", bad)
		}
	}
}

func TestParsePass1Accepts(t *testing.T) {
	out, err := ParsePass1(goodReply)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Phases) != 1 || out.Phases[0].Tasks[0].Steps[0].Title != "Create the module" || out.Overview != "A small project." {
		t.Errorf("parsed %+v", out)
	}
	if _, err := ParsePass1(`{"phases":[],"overview":"nothing new in this chunk"}`); err != nil {
		t.Errorf("a chunk that adds nothing is valid: %v", err)
	}
}

func TestParsePass1Rejects(t *testing.T) {
	long := strings.Repeat("x", 501)
	cases := map[string]string{
		"no overview":         `{"phases":[]}`,
		"blank overview":      `{"phases":[],"overview":"  "}`,
		"unknown field":       `{"phases":[],"overview":"o","extra":1}`,
		"not json":            `no braces at all`,
		"empty phase title":   `{"phases":[{"title":"","digest":"d","objective":"","tasks":[]}],"overview":"o"}`,
		"empty digest":        `{"phases":[{"title":"P","digest":"","objective":"","tasks":[]}],"overview":"o"}`,
		"long digest":         `{"phases":[{"title":"P","digest":"` + long + `","objective":"","tasks":[]}],"overview":"o"}`,
		"duplicate phases":    `{"phases":[{"title":"P","digest":"d","objective":"","tasks":[]},{"title":" p ","digest":"d","objective":"","tasks":[]}],"overview":"o"}`,
		"duplicate steps":     `{"phases":[{"title":"P","digest":"d","objective":"","tasks":[{"title":"T","digest":"d","objective":"","steps":[{"title":"S","digest":"d"},{"title":"s","digest":"d"}]}]}],"overview":"o"}`,
		"step missing digest": `{"phases":[{"title":"P","digest":"d","objective":"","tasks":[{"title":"T","digest":"d","objective":"","steps":[{"title":"S","digest":""}]}]}],"overview":"o"}`,
	}
	for name, reply := range cases {
		if _, err := ParsePass1(reply); err == nil {
			t.Errorf("%s: ParsePass1 accepted an invalid reply", name)
		}
	}
	var many strings.Builder
	many.WriteString(`{"phases":[`)
	for i := 0; i < maxPhasesPerPass+1; i++ {
		if i > 0 {
			many.WriteString(",")
		}
		many.WriteString(`{"title":"P` + strings.Repeat("x", i) + `","digest":"d","objective":"","tasks":[]}`)
	}
	many.WriteString(`],"overview":"o"}`)
	if _, err := ParsePass1(many.String()); err == nil || !strings.Contains(err.Error(), "too many phases") {
		t.Errorf("too many phases: %v", err)
	}
}

func TestParsePass1ErrorsNameTheProblem(t *testing.T) {
	_, err := ParsePass1(`{"phases":[{"title":"Setup","digest":"","objective":"","tasks":[]}],"overview":"o"}`)
	if err == nil || !strings.Contains(err.Error(), `phase "Setup"`) || !strings.Contains(err.Error(), "digest") {
		t.Errorf("error should name the phase and the field: %v", err)
	}
}

func TestNormalizeTitle(t *testing.T) {
	if NormalizeTitle("  Set   UP\tRepo ") != "set up repo" {
		t.Error("NormalizeTitle did not fold case and whitespace")
	}
}

func TestParsePass1ErrorIsBoundedByAHugeTitle(t *testing.T) {
	huge := strings.Repeat("x", 5_000_000)
	_, err := ParsePass1(`{"phases":[{"title":"` + huge + `","digest":"","objective":"","tasks":[]}],"overview":"o"}`)
	if err == nil || len(err.Error()) > 500 || !strings.Contains(err.Error(), "title is longer") {
		n := 0
		if err != nil {
			n = len(err.Error())
		}
		t.Errorf("want a short error naming the title field, got %d bytes: %v", n, err)
	}
}

func TestExtractJSONFindsTheRealObject(t *testing.T) {
	cases := map[string]string{
		`I used {curly} braces. {"a":1}`:           `{"a":1}`,
		`note { unbalanced. {"a":1}`:               `{"a":1}`,
		`{bad} then {"ok":true}`:                   `{"ok":true}`,
		`{bad}`:                                    `{bad}`,
		`{"a":"x\\"}`:                              `{"a":"x\\"}`,
		`{"phases":[{"title":"P"}], "overview": }`: `{"phases":[{"title":"P"}], "overview": }`,
	}
	for in, want := range cases {
		got, err := ExtractJSON(in)
		if err != nil || got != want {
			t.Errorf("ExtractJSON(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	_, err := ParsePass1(`{bad}`)
	if err == nil || !strings.Contains(err.Error(), "does not match the schema") {
		t.Errorf("ParsePass1({bad}) = %v, want a decode error", err)
	}
	if _, err := ExtractJSON(`only { prose`); err == nil || !strings.Contains(err.Error(), "not closed") {
		t.Errorf("unclosed: %v", err)
	}
}

func TestCandidates(t *testing.T) {
	list, seen := candidates(`a {"x":{"y":1}} b {bad} {open`)
	if !seen || len(list) != 2 || list[0] != `{"x":{"y":1}}` || list[1] != `{bad}` {
		t.Errorf("candidates = %q, seen=%v", list, seen)
	}
	if list, seen := candidates("no braces at all"); seen || len(list) != 0 {
		t.Errorf("no braces: %q, %v", list, seen)
	}
}

func TestParsePass1SkipsAnEarlierObjectThatIsNotThePlan(t *testing.T) {
	out, err := ParsePass1("I checked {} and {\"note\":1} first.\n" + goodReply)
	if err != nil || len(out.Phases) != 1 || out.Overview != "A small project." {
		t.Fatalf("ParsePass1 = %+v, %v", out, err)
	}
}

func TestParsePass1ReportsTheFirstCandidatesError(t *testing.T) {
	_, err := ParsePass1(`{"phases":[],"extra":1} {"overview":""}`)
	if err == nil || !strings.Contains(err.Error(), "does not match the schema") {
		t.Errorf("err = %v, want the first candidate's schema error", err)
	}
}

func TestParsePass1BoundsTheSchemaError(t *testing.T) {
	reply := `{"phases":[],"overview":"o","` + strings.Repeat("k", 200000) + `":1}`
	_, err := ParsePass1(reply)
	if err == nil || !strings.Contains(err.Error(), "does not match the schema") {
		t.Fatalf("err = %v", err)
	}
	if len(err.Error()) >= 700 {
		t.Errorf("error is %d bytes", len(err.Error()))
	}
}

func TestParsePass1ReportsTheLongestCandidate(t *testing.T) {
	_, err := ParsePass1("{} " + `{"phases":[{"title":"","digest":"d","objective":"","tasks":[]}],"overview":"o"}`)
	if err == nil || !strings.Contains(err.Error(), "title is empty") || strings.Contains(err.Error(), "overview") {
		t.Errorf("err = %v", err)
	}
	_, err = ParsePass1(`{"phases":[],"extra":1} {"overview":""}`)
	if err == nil || !strings.Contains(err.Error(), "does not match the schema") {
		t.Errorf("first, longer candidate should win: %v", err)
	}
}

func TestParseFirst(t *testing.T) {
	dec := func(raw string) (int, error) {
		if raw == "{}" {
			return 0, errors.New("bad " + raw)
		}
		return len(raw), nil
	}
	if _, err := parseFirst("no braces", dec); err == nil || !strings.Contains(err.Error(), "no JSON object") {
		t.Errorf("no braces: %v", err)
	}
	if _, err := parseFirst("{", dec); err == nil || !strings.Contains(err.Error(), "not closed") {
		t.Errorf("lone brace: %v", err)
	}
	v, err := parseFirst("{} {}", dec)
	if err == nil || v != 0 {
		t.Errorf("all fail: %d, %v", v, err)
	}
	v, err = parseFirst("{} {abc} {defg}", dec)
	if err != nil || v != 5 {
		t.Errorf("first success: %d, %v", v, err)
	}
}
