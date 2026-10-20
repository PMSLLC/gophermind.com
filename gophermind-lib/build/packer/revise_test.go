package packer

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestPackReviseNoReplyText(t *testing.T) {
	n := baseNode()
	hist := []string{"attempt 1: test_failed TestMakeWidget", "attempt 2: build_failed"}
	p, err := PackRevise(n, contractsFixture(t), hist, Inputs{Budget: 8000})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range append([]string{"func MakeWidget(name string) Widget", "type Widget struct", "MakeWidget builds a Widget.", n.TestFile, "t.Errorf"}, hist...) {
		if !strings.Contains(p.Text, want) {
			t.Errorf("revise prompt lacks %q", want)
		}
	}
	for _, bad := range []string{"CANARY", "SPLIT_REQUIRED", "CONTRACT_CHANGE_REQUIRED", "type Options"} {
		if strings.Contains(p.Text, bad) {
			t.Errorf("revise prompt has %q", bad)
		}
	}
	if !strings.Contains(p.Text, "CONTRACT_PROBLEM:") || !strings.Contains(p.Text, `{"notes"`) {
		t.Error("reply forms missing")
	}
	// Only the given history lines: no other attempt text.
	q, _ := PackRevise(n, contractsFixture(t), nil, Inputs{Budget: 8000})
	if strings.Contains(q.Text, "attempt 1") {
		t.Error("history not caller-driven")
	}
	const canary = "CANARY-revise"
	r, _ := PackRevise(n, contractsFixture(t), []string{canary}, Inputs{Budget: 8000})
	forms := fmt.Sprintf("%v %+v %#v %s %q", r, r, r, r, r)
	js, _ := json.Marshal(r)
	if strings.Contains(forms, canary) || strings.Contains(string(js), canary) || !strings.Contains(forms, r.SHA256) {
		t.Error("Packed forms leak or lack the sha")
	}
	if _, err := PackRevise(n, nil, nil, Inputs{Budget: 8000}); err == nil {
		t.Error("nil contracts accepted")
	}
}

func TestParseRevise(t *testing.T) {
	long := strings.Repeat("n", 300)
	cases := []struct {
		name, in string
		notes    int
		problem  string
		bad      bool
	}{
		{"json", `{"notes":["a","b"]}`, 2, "", false},
		{"fenced", "```json\n{\"notes\":[\"a\"]}\n```", 1, "", false},
		{"six", `{"notes":["1","2","3","4","5","6"]}`, 0, "", true},
		{"empty", `{"notes":[]}`, 0, "", true},
		{"long note", `{"notes":["` + long + `"]}`, 0, "", true},
		{"problem", "CONTRACT_PROBLEM: s", 0, "s", false},
		{"prose", "I think the tests are wrong CANARY-note", 0, "", true},
		{"blank note", `{"notes":["  "]}`, 0, "", true},
		{"extra key", `{"notes":["a"],"x":1}`, 0, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			notes, problem, err := ParseRevise(c.in)
			if c.bad {
				if err == nil {
					t.Fatal("want error")
				}
				if strings.Contains(err.Error(), "CANARY") || strings.Contains(err.Error(), long[:20]) {
					t.Errorf("error quotes text: %v", err)
				}
				return
			}
			if err != nil || len(notes) != c.notes || problem != c.problem {
				t.Errorf("got %v %q %v", notes, problem, err)
			}
		})
	}
}

func TestPackReviseSecretsAndBudget(t *testing.T) {
	n := baseNode()
	const sec = "CANARY-secret-value"
	p, err := PackRevise(n, contractsFixture(t), []string{"attempt 1 " + sec, "attempt 2 ok"}, Inputs{Budget: 8000, Secrets: []string{sec}})
	if err != nil || strings.Contains(p.Text, sec) || !strings.Contains(p.Text, "attempt 2 ok") {
		t.Fatalf("history not redacted: %v", err)
	}
	bad := n
	bad.TestSource = baseTest + "// " + sec + "\n"
	if _, err := PackRevise(bad, contractsFixture(t), nil, Inputs{Budget: 8000, Secrets: []string{sec}}); err == nil || strings.Contains(err.Error(), sec) {
		t.Errorf("err = %v", err)
	}
	if _, err := PackRevise(n, contractsFixture(t), nil, Inputs{}); err == nil {
		t.Error("zero budget accepted")
	}
	// Budget cuts: history to the last 3, then test comments, then the floor error.
	var hist []string
	for i := 0; i < 12; i++ {
		hist = append(hist, fmt.Sprintf("attempt %02d: test_failed TestMakeWidget %s", i, strings.Repeat("n", 60)))
	}
	n.TestSource = commentedTest
	full, err := PackRevise(n, contractsFixture(t), hist, Inputs{Budget: 100000})
	if err != nil || len(full.Dropped) != 0 {
		t.Fatal(err)
	}
	cut, err := PackRevise(n, contractsFixture(t), hist, Inputs{Budget: full.Tokens - 1})
	if err != nil || strings.Join(cut.Dropped, ",") != "history:last3" || !strings.Contains(cut.Text, "attempt 11") || strings.Contains(cut.Text, "attempt 08") {
		t.Errorf("dropped %v (%v)", cut.Dropped, err)
	}
	var floor *ErrFloorOverBudget
	if _, err := PackRevise(n, contractsFixture(t), hist, Inputs{Budget: 200}); !errors.As(err, &floor) || floor.Tokens <= 200 {
		t.Errorf("err = %v", err)
	}
	stripped, _ := stripTestComments(commentedTest)
	sn := n
	sn.TestSource = stripped
	small, err := PackRevise(sn, contractsFixture(t), hist[:3], Inputs{Budget: 100000})
	if err != nil {
		t.Fatal(err)
	}
	mid, err := PackRevise(n, contractsFixture(t), hist[:3], Inputs{Budget: small.Tokens})
	if err != nil || strings.Join(mid.Dropped, ",") != "test_comments" {
		t.Errorf("dropped %v (%v)", mid.Dropped, err)
	}
}
