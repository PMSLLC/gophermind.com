package plan

import (
	"encoding/json"
	"strings"
	"testing"
)

const (
	s1 = "phase-001.task-001.step-001"
	s2 = "phase-001.task-001.step-002"
	s3 = "phase-001.task-001.step-003"
)

var (
	batch23  = []string{s2, s3}
	siblings = []string{s1, s2, s3}
)

func goodSpec(id string) StepSpecOut {
	return StepSpecOut{
		ID:                 id,
		Description:        "build " + id,
		TargetPaths:        []string{"internal/x/x.go"},
		AcceptanceCriteria: []string{"it compiles"},
		TestCommand:        []string{"go", "test", "./..."},
		DependsOn:          []string{},
	}
}

func replyOf(t *testing.T, steps ...StepSpecOut) string {
	t.Helper()
	b, err := json.Marshal(Pass2Output{Steps: steps})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParsePass2Accepts(t *testing.T) {
	a, b := goodSpec(s2), goodSpec(s3)
	b.DependsOn = []string{s1, s2}
	out, err := ParsePass2("Here you go:\n```json\n"+replyOf(t, a, b)+"\n```", batch23, siblings)
	if err != nil || len(out.Steps) != 2 || out.Steps[1].DependsOn[1] != s2 {
		t.Fatalf("ParsePass2 = %+v, %v", out, err)
	}
	out, err = ParsePass2("I checked {} first. "+replyOf(t, a, goodSpec(s3)), batch23, siblings)
	if err != nil || len(out.Steps) != 2 {
		t.Errorf("an earlier object that is not the reply must be skipped: %+v, %v", out, err)
	}
	empty := goodSpec(s2)
	empty.TargetPaths, empty.TestCommand = nil, nil
	if _, err := ParsePass2(replyOf(t, empty, goodSpec(s3)), batch23, siblings); err != nil {
		t.Errorf("no target paths and no test command are allowed: %v", err)
	}
}

func TestParsePass2Rejects(t *testing.T) {
	bad := func(f func(*StepSpecOut)) string {
		a := goodSpec(s2)
		f(&a)
		return replyOf(t, a, goodSpec(s3))
	}
	cases := map[string]struct {
		reply string
		want  string
	}{
		"missing step":      {replyOf(t, goodSpec(s2)), "missing from the reply"},
		"extra step":        {replyOf(t, goodSpec(s2), goodSpec(s3), goodSpec(s1)), "not one of the steps asked for"},
		"duplicate step":    {replyOf(t, goodSpec(s2), goodSpec(s2), goodSpec(s3)), "more than once"},
		"empty description": {bad(func(s *StepSpecOut) { s.Description = " " }), "description is empty"},
		"long description":  {bad(func(s *StepSpecOut) { s.Description = strings.Repeat("d", maxDescRunes+1) }), "description is longer"},
		"no criteria":       {bad(func(s *StepSpecOut) { s.AcceptanceCriteria = nil }), "acceptance_criteria needs"},
		"too many criteria": {bad(func(s *StepSpecOut) {
			s.AcceptanceCriteria = make([]string, maxCriteria+1)
			for i := range s.AcceptanceCriteria {
				s.AcceptanceCriteria[i] = "c"
			}
		}), "too many acceptance criteria"},
		"empty criterion":   {bad(func(s *StepSpecOut) { s.AcceptanceCriteria = []string{"ok", ""} }), "criterion is empty"},
		"long criterion":    {bad(func(s *StepSpecOut) { s.AcceptanceCriteria = []string{strings.Repeat("c", maxCriterionRunes+1)} }), "criterion is longer"},
		"absolute path":     {bad(func(s *StepSpecOut) { s.TargetPaths = []string{"/etc/passwd"} }), "not absolute"},
		"backslash root":    {bad(func(s *StepSpecOut) { s.TargetPaths = []string{`\windows`} }), "not absolute"},
		"drive path":        {bad(func(s *StepSpecOut) { s.TargetPaths = []string{`C:\x`} }), "not drive-qualified"},
		"dotdot":            {bad(func(s *StepSpecOut) { s.TargetPaths = []string{"a/../../b"} }), "must not contain"},
		"control character": {bad(func(s *StepSpecOut) { s.TargetPaths = []string{"a\nb"} }), "control character"},
		"empty path":        {bad(func(s *StepSpecOut) { s.TargetPaths = []string{""} }), "is empty"},
		"too many paths": {bad(func(s *StepSpecOut) {
			s.TargetPaths = make([]string, maxTargetPaths+1)
			for i := range s.TargetPaths {
				s.TargetPaths[i] = "p"
			}
		}), "too many target_paths"},
		"empty command arg": {bad(func(s *StepSpecOut) { s.TestCommand = []string{"go", ""} }), "empty argument"},
		"too many command": {bad(func(s *StepSpecOut) {
			s.TestCommand = make([]string, maxCommandArgs+1)
			for i := range s.TestCommand {
				s.TestCommand[i] = "a"
			}
		}), "too many arguments"},
		"unknown dependency":   {bad(func(s *StepSpecOut) { s.DependsOn = []string{"phase-009.task-001.step-001"} }), "not a step of this task"},
		"self dependency":      {bad(func(s *StepSpecOut) { s.DependsOn = []string{s2} }), "earlier step"},
		"later dependency":     {bad(func(s *StepSpecOut) { s.DependsOn = []string{s3} }), "earlier step"},
		"duplicate dependency": {bad(func(s *StepSpecOut) { s.DependsOn = []string{s1, s1} }), "twice"},
		"unknown field":        {`{"steps":[],"extra":1}`, "does not match the schema"},
		"not json":             {"no braces here", "no JSON object"},
	}
	for name, c := range cases {
		_, err := ParsePass2(c.reply, batch23, siblings)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want it to contain %q", name, err, c.want)
		}
	}
}

func TestParsePass2ErrorsStayBoundedForHugeValues(t *testing.T) {
	huge := strings.Repeat("x", 5_000_000)
	a := goodSpec(s2)
	a.TargetPaths = []string{"/" + huge}
	_, err := ParsePass2(replyOf(t, a, goodSpec(s3)), batch23, siblings)
	if err == nil || len(err.Error()) > 500 {
		t.Errorf("error length = %d, want a bounded error", len(err.Error()))
	}
	_, err = ParsePass2(replyOf(t, goodSpec(huge), goodSpec(s3)), batch23, siblings)
	if err == nil || len(err.Error()) > 500 {
		t.Errorf("huge step id: error length = %d", len(err.Error()))
	}
}

func TestParsePass2BoundsTheSchemaError(t *testing.T) {
	reply := `{"steps":[],"` + strings.Repeat("k", 200000) + `":1}`
	_, err := ParsePass2(reply, []string{"s"}, []string{"s"})
	if err == nil || !strings.Contains(err.Error(), "does not match the schema") {
		t.Fatalf("err = %v", err)
	}
	if len(err.Error()) >= 700 {
		t.Errorf("error is %d bytes", len(err.Error()))
	}
}
