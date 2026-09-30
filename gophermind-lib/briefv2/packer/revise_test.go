package packer

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestPackReviseNoReplyText(t *testing.T) {
	n := baseNode()
	hist := []string{"attempt 1: test_failed TestMakeWidget", "attempt 2: build_failed"}
	p, err := PackRevise(n, contractsFixture(t), hist)
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
	q, _ := PackRevise(n, contractsFixture(t), nil)
	if strings.Contains(q.Text, "attempt 1") {
		t.Error("history not caller-driven")
	}
	const canary = "CANARY-revise"
	r, _ := PackRevise(n, contractsFixture(t), []string{canary})
	forms := fmt.Sprintf("%v %+v %#v %s %q", r, r, r, r, r)
	js, _ := json.Marshal(r)
	if strings.Contains(forms, canary) || strings.Contains(string(js), canary) || !strings.Contains(forms, r.SHA256) {
		t.Error("Packed forms leak or lack the sha")
	}
	if _, err := PackRevise(n, nil, nil); err == nil {
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
