package plantree

import (
	"strings"
	"testing"
)

const step1 = "phase-001.task-001.step-001"

func TestValidateAcceptsSkeletons(t *testing.T) {
	for _, id := range []string{RootID, "phase-001", "phase-001.task-001", step1} {
		if err := Validate(mk(t, id)); err != nil {
			t.Errorf("Validate(skeleton %s): %v", id, err)
		}
	}
}

func TestValidateRejects(t *testing.T) {
	cases := map[string]func(*Node){
		"wrong schema version":      func(n *Node) { n.SchemaVersion = 3 },
		"empty title":               func(n *Node) { n.Title = " " },
		"zero revision":             func(n *Node) { n.NodeRevision = 0 },
		"empty digest":              func(n *Node) { n.ContextDigest = "" },
		"wrong parent_ref":          func(n *Node) { r := "../meta.json"; n.ParentRef = &r },
		"missing parent_ref":        func(n *Node) { n.ParentRef = nil },
		"self dependency":           func(n *Node) { n.DependsOn = []string{n.ID} },
		"duplicate dependency":      func(n *Node) { n.DependsOn = []string{"phase-001", "phase-001"} },
		"malformed dependency id":   func(n *Node) { n.DependsOn = []string{"nope"} },
		"unknown stage":             func(n *Node) { n.Planning.Stage = "done" },
		"unknown status":            func(n *Node) { n.Status = "finished" },
		"blocked without reason":    func(n *Node) { n.Status = StatusBlocked },
		"drafted without work":      func(n *Node) { n.Planning.Stage = StageDrafted },
		"drafted without criteria":  func(n *Node) { n.Planning.Stage = StageDrafted; n.Work = &Work{Description: "d"} },
		"drafted empty description": func(n *Node) { n.Planning.Stage = StageDrafted; n.Work = &Work{AcceptanceCriteria: []string{"x"}} },
	}
	for name, mutate := range cases {
		n := mk(t, step1)
		mutate(&n)
		if err := Validate(n); err == nil {
			t.Errorf("%s: Validate accepted an invalid node", name)
		}
	}

	structural := map[string]func(*Node){
		"structural status": func(n *Node) { n.Status = StatusUntouched },
		"structural work":   func(n *Node) { n.Work = draftedWork() },
	}
	for name, mutate := range structural {
		n := mk(t, "phase-001")
		mutate(&n)
		if err := Validate(n); err == nil {
			t.Errorf("%s: Validate accepted an invalid structural node", name)
		}
	}

	root := mk(t, RootID)
	r := "../../plan.json"
	root.ParentRef = &r
	if err := Validate(root); err == nil {
		t.Error("root with a parent_ref must be rejected")
	}
}

func TestValidateAcceptsDraftedAndHeldSteps(t *testing.T) {
	n := mk(t, step1)
	n.Planning.Stage = StageDrafted
	n.Work = draftedWork()
	if err := Validate(n); err != nil {
		t.Errorf("drafted step: %v", err)
	}
	n.Status = StatusBlocked
	n.Reason = "waiting on a decision"
	if err := Validate(n); err != nil {
		t.Errorf("blocked step with reason: %v", err)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	n := mk(t, step1)
	n.Planning.Stage = StageDrafted
	n.Work = draftedWork()
	n.DependsOn = []string{"phase-001.task-001.step-002"}
	b, err := Encode(n)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(b)
	if err != nil {
		t.Fatalf("Decode: %v\n%s", err, b)
	}
	if got.ID != n.ID || got.Work == nil || got.Work.Description != "do the thing" || got.DependsOn[0] != n.DependsOn[0] {
		t.Errorf("round trip lost data: %+v", got)
	}
}

func TestEncodeWritesEmptyArraysNotNull(t *testing.T) {
	n := mk(t, step1)
	n.DependsOn = nil
	n.Work = &Work{Description: "d"}
	b, err := Encode(n)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, field := range []string{`"depends_on": []`, `"target_paths": []`, `"acceptance_criteria": []`, `"test_command": []`} {
		if !strings.Contains(s, field) {
			t.Errorf("encoded node missing %s:\n%s", field, s)
		}
	}
}

func TestDecodeRejects(t *testing.T) {
	if _, err := Decode([]byte(`{"schema_version":9}`)); err == nil || !strings.Contains(err.Error(), "schema_version 9") {
		t.Errorf("unsupported version: %v", err)
	}
	valid, _ := Encode(mk(t, RootID))
	withExtra := strings.Replace(string(valid), `"title"`, `"titel_typo": 1, "title"`, 1)
	if _, err := Decode([]byte(withExtra)); err == nil {
		t.Error("an unknown field must be rejected")
	}
	if _, err := Decode(append(valid, []byte(`{"x":1}`)...)); err == nil {
		t.Error("trailing data must be rejected")
	}
	if _, err := Decode([]byte(`not json`)); err == nil {
		t.Error("malformed JSON must be rejected")
	}
}
