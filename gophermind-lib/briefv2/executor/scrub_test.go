package executor

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/blackboard"
)

// The leaf path and the wave path scrub with the packer's secret forms, not
// one exact substring; a reason with no colon never comes back whole.
func TestScrubUsesPackerSecretForms(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.schedRC(t, Script{})
	b64 := base64.StdEncoding.EncodeToString([]byte(canarySecret))

	for _, tc := range []struct{ name, reason, want string }{
		{"raw with colon", "test_fail: TestA_" + canarySecret, "test_fail"},
		{"base64 with colon", "test_fail: TestA_" + b64, "test_fail"},
		{"no colon is not returned whole", "TestA_" + canarySecret, ""},
		{"no colon base64", "x" + b64, ""},
		{"clean reason kept", "test_fail: TestA", "test_fail: TestA"},
	} {
		got := rc.scrubReason(tc.reason)
		if got != tc.want {
			t.Errorf("%s: scrubReason = %q, want %q", tc.name, got, tc.want)
		}
		if strings.Contains(got, canarySecret) || strings.Contains(got, b64) {
			t.Errorf("%s: secret survived in %q", tc.name, got)
		}
	}
	names := rc.scrubNames([]string{"TestA", "TestB_" + b64, "TestC_" + canarySecret, "TestD"})
	if strings.Join(names, ",") != "TestA,TestD" {
		t.Errorf("scrubNames = %v", names)
	}
}

// A failed attempt never has an empty failure reason: when the reason holds a
// secret and has no class to keep, the attempt's own class is recorded.
func TestRecordKeepsClassWhenReasonIsScrubbedAway(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, Script{}, true)
	l := g.plan.Leaf("fn-greet")
	lr, err := rc.newLeafRun(context.Background(), l, leafIn{})
	if err != nil {
		t.Fatal(err)
	}
	e := ladderEntry{Name: "a/m1", Pos: 1}
	if err := lr.record(context.Background(), e, rc.o.Now(), blackboard.VerdictFail, "test_fail", "TestA_"+canarySecret, nil, ""); err != nil {
		t.Fatal(err)
	}
	if err := lr.record(context.Background(), e, rc.o.Now(), blackboard.VerdictFail, "test_fail", "test_fail: TestB_"+canarySecret, nil, ""); err != nil {
		t.Fatal(err)
	}
	got := reasonsOf(t, g.board, "fn-greet")
	if len(got) != 2 || got[0] != "test_fail" || got[1] != "test_fail" {
		t.Fatalf("failure reasons = %q, want both the class test_fail", got)
	}
}
