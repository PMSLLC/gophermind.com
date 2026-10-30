package planner

import (
	"encoding/json"
	"strings"
	"testing"
)

// wholeOrig is a small leaf node: only the fields the whole-document tests touch.
func wholeOrig() map[string]any {
	var m map[string]any
	_ = json.Unmarshal([]byte(`{
	  "id": "fn-x", "title": "X", "description": "Does x.", "model_tier": "standard",
	  "contract": {
	    "package": "internal/x", "file": "internal/x/x.go", "signature": "func x(a int, w io.Writer) error",
	    "inputs": [{"name":"a","type":"int","go_type":"int","nilable":false,"units":"rows"},
	               {"name":"w","type":"io.Writer","go_type":"io.Writer","nilable":false}],
	    "outputs": [{"name":"","type":"error","go_type":"error","nilable":true}],
	    "errors": [{"when":"write fails","returns":"error","kind":"wrapped","test":"negative_write"}],
	    "side_effects": ["writes to w"]
	  },
	  "context": {"dependency_signatures": ["type T struct{}"], "constraints": ["stdlib only"], "notes": []},
	  "construction": {"approach_chosen": "loop"},
	  "tests": [{"name":"negative_write"}]
	}`), &m)
	return m
}

func wholeReply(t *testing.T, mutate func(d map[string]any)) string {
	t.Helper()
	d := wholeOrig()
	delete(d, "id")
	delete(d, "title")
	delete(d, "model_tier")
	delete(d, "tests")
	mutate(d)
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestWholeDocReplyBecomesAPatchOfWhatChanged(t *testing.T) {
	reply := wholeReply(t, func(d map[string]any) {
		cx := d["context"].(map[string]any)
		cx["constraints"] = []any{"stdlib only", "check every write error"}
		d["groups"] = map[string]any{"construction": d["construction"]}
		delete(d, "construction")
		d["assumptions"] = []any{"the builder ignored the write error"}
	})
	rep, warns, err := parseReviseReplyFor(reply, nil, wholeOrig())
	if err != nil {
		t.Fatal(err)
	}
	p := rep.patch
	if p.Description != nil || p.Contract != nil || len(p.Groups) != 0 {
		t.Fatalf("unchanged parts survived: %+v", p)
	}
	if p.Context == nil || p.Context.Constraints == nil || len(*p.Context.Constraints) != 2 || p.Context.Notes != nil {
		t.Fatalf("context = %+v", p.Context)
	}
	if len(warns) == 0 {
		t.Fatal("dropping fields was not reported")
	}
	for _, w := range warns {
		if strings.Contains(w, "check every write error") {
			t.Fatalf("a warning quotes the reply: %q", w)
		}
	}
}

func TestWholeDocItemsKeepOnlyTheKeysAModelMayWrite(t *testing.T) {
	reply := wholeReply(t, func(d map[string]any) {
		in := d["contract"].(map[string]any)["inputs"].([]any)
		in[0].(map[string]any)["type"] = "int64"
		in[0].(map[string]any)["description"] = "row count"
	})
	rep, _, err := parseReviseReplyFor(reply, nil, wholeOrig())
	if err != nil {
		t.Fatal(err)
	}
	ins := rep.patch.Contract.Inputs
	if ins == nil || len(*ins) != 2 {
		t.Fatalf("inputs = %+v", rep.patch.Contract)
	}
	for _, k := range []string{"go_type", "nilable", "units"} {
		if _, ok := (*ins)[0][k]; ok {
			t.Errorf("item key %s kept", k)
		}
	}
	if (*ins)[0]["type"] != "int64" {
		t.Errorf("the change was lost: %v", (*ins)[0])
	}
	if rep.patch.Contract.Errors != nil || rep.patch.Contract.Outputs != nil || rep.patch.Contract.SideEffects != nil {
		t.Errorf("unchanged lists survived: %+v", rep.patch.Contract)
	}
}

func TestWholeDocChangedFrozenFieldIsStillRefusedAndNamed(t *testing.T) {
	reply := wholeReply(t, func(d map[string]any) {
		d["contract"].(map[string]any)["signature"] = "func x(a int) error"
		d["description"] = "Does x, differently."
	})
	_, _, err := parseReviseReplyFor(reply, nil, wholeOrig())
	if classOf(err, "") != ClassReviseInvalid || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("err = %v", err)
	}
}

func TestWholeDocWithNothingChangedIsRefusedAsANoop(t *testing.T) {
	_, _, err := parseReviseReplyFor(wholeReply(t, func(map[string]any) {}), nil, wholeOrig())
	if classOf(err, "") != ClassReviseNoop {
		t.Fatalf("err = %v", err)
	}
}

func TestUnknownKeyMessageNamesTheKindNotJustInvalidJSON(t *testing.T) {
	_, err := parseReviseReply(`{"description":"x","contract":{"signature":"func f()"}}`)
	if err == nil || strings.Contains(err.Error(), "not valid JSON") || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("err = %v", err)
	}
	// A key the reply invented is never quoted.
	_, err = parseReviseReply(`{"description":"x","zz_secret_key":1}`)
	if err == nil || strings.Contains(err.Error(), "zz_secret_key") || strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("err = %v", err)
	}
}

func TestWholeDocLeavesKeywordAndStrictRepliesAlone(t *testing.T) {
	if _, _, err := parseReviseReplyFor("SPLIT_REQUIRED\nToo big.", nil, wholeOrig()); err != nil {
		t.Fatal(err)
	}
	// A key that is not a field of the node is not dropped.
	if _, _, err := parseReviseReplyFor(`{"description":"y","test_file":"evil_test.go"}`, nil, wholeOrig()); err == nil {
		t.Fatal("an invented top-level key was accepted")
	}
	// A changed test list is not the model's to write.
	reply := wholeReply(t, func(d map[string]any) { d["tests"] = []any{map[string]any{"name": "other"}} })
	if _, _, err := parseReviseReplyFor(reply, nil, wholeOrig()); err == nil {
		t.Fatal("a changed tests list was accepted")
	}
}

// A CONTRACT_CHANGE_REQUIRED reply that names nothing a rewrite may not change
// (the run of gm-2026-10-30-038: "the contract is correct, the test is wrong")
// is the patch form's rewrite_tests; one that names the signature stays a flag.
func TestContractChangeNamingNoLockedFieldRewritesTests(t *testing.T) {
	testsWrong := "CONTRACT_CHANGE_REQUIRED\n\nThe function `getRomanMappings` is intended to return a set of symbol mappings, " +
		"not a slice of every integer from 1 to 3999. The test is incorrect in asserting that `wantLen` should be 3999. " +
		"The contract is correct; tests must be rewritten to expect the discrete Roman numeral symbols and subtractive pairs."
	rep, warns, err := parseReviseReplyFor(testsWrong, nil, wholeOrig())
	if err != nil {
		t.Fatal(err)
	}
	if rep.kind != kindPatch || !rep.patch.RewriteTests || rep.patch.Description != nil || rep.patch.Contract != nil {
		t.Fatalf("reply = %+v, want a patch that only rewrites the tests", rep)
	}
	if len(rep.patch.Assumptions) != 1 || len(rep.patch.Assumptions[0]) > maxAssumptionLen {
		t.Errorf("assumptions = %q, want the reason cut to %d bytes", rep.patch.Assumptions, maxAssumptionLen)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "rewrite_tests") {
		t.Errorf("warnings = %q", warns)
	}
	for _, locked := range []string{
		"The signature must return an error as well.",
		"It belongs in package internal/y, not internal/x.",
		"It depends on fn-parse, so its depends_on must list it.",
		"The function must also return the number of runes.",
	} {
		rep, _, err := parseReviseReplyFor("CONTRACT_CHANGE_REQUIRED\n"+locked, nil, wholeOrig())
		if err != nil || rep.kind != kindContractChange {
			t.Errorf("%q: kind %v err %v, want a contract change flag", locked, rep.kind, err)
		}
	}
}

// Run gm-2026-10-30-064: six revise replies wrote construction at the top
// level instead of under groups, with a context key the patch does not have.
// The group moves under groups and the unknown context key is left out.
func TestReviseReplyMovesATopLevelGroupUnderGroups(t *testing.T) {
	reply := `{"construction": {"approach_chosen": "a", "steps": ["s1", "s2"]}, "context": {"dependency_signatures": ["func A()"], "notes": ["n"]}}`
	rep, warns, err := parseReviseReplyFor(reply, nil, wholeOrig())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rep.patch.Groups["construction"]; !ok {
		t.Errorf("groups = %v, want construction", rep.patch.Groups)
	}
	if rep.patch.Context == nil || rep.patch.Context.Notes == nil {
		t.Errorf("context notes were lost: %+v", rep.patch.Context)
	}
	if !strings.Contains(strings.Join(warns, "|"), "context.dependency_signatures") {
		t.Errorf("warnings = %q", warns)
	}
}

// Run gm-2026-10-30-067: CONTRACT_CHANGE_REQUIRED asking for clearer exit
// codes and an output renamed, all of which the patch form can write. Such a
// reply is refused with the reason, so the model is asked again; a real
// signature change ("must also return") is still a flag.
func TestContractChangeAPatchCouldWriteIsRefused(t *testing.T) {
	patchable := "CONTRACT_CHANGE_REQUIRED\nThe Run contract is underspecified regarding exit codes and output mapping. It must state that it returns exit code 0 on success and 2 on failure. The outputs field is also incorrectly named int instead of exit_code."
	if _, _, err := parseReviseReplyFor(patchable, nil, wholeOrig()); err == nil || !strings.Contains(err.Error(), "write the patch") {
		t.Errorf("err = %v, want a refusal that asks for the patch", err)
	}
	if rep, _, err := parseReviseReplyFor("CONTRACT_CHANGE_REQUIRED\nThe function must also return the number of runes.", nil, wholeOrig()); err != nil || rep.kind != kindContractChange {
		t.Errorf("a new return value: kind %v err %v, want a flag", rep.kind, err)
	}
}
