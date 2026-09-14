package secaudit

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeReviewer returns a scripted reply per call, keyed by call index.
type fakeReviewer struct {
	replies []string
	err     error
	calls   int
}

func (f *fakeReviewer) Review(ctx context.Context, prompt string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	r := ""
	if f.calls < len(f.replies) {
		r = f.replies[f.calls]
	}
	f.calls++
	return r, nil
}

func base() []Finding {
	return []Finding{
		{RuleID: "weak-crypto (CWE-327)", Severity: Medium, File: "a.go", Line: 1, Snippet: "md5(x)"},
	}
}

// TestVerifyMarksConfirmedFinding: a model that confirms sets Verified=true.
func TestVerifyMarksConfirmedFinding(t *testing.T) {
	r := &fakeReviewer{replies: []string{`{"exploitable": true, "note": "user input reaches this hash"}`}}
	out := Verify(context.Background(), "/root", base(), r)

	if len(out) != 1 || !out[0].Verified {
		t.Fatalf("finding not marked verified: %+v", out)
	}
	if !strings.Contains(out[0].VerifyNote, "user input") {
		t.Errorf("verify note lost: %q", out[0].VerifyNote)
	}
}

// TestVerifyDowngradesRefutedFinding: crucially, a refuted finding is KEPT but
// marked unverified — a weak model must not be able to delete a real static
// finding, only lower its confidence.
func TestVerifyDowngradesRefutedFinding(t *testing.T) {
	r := &fakeReviewer{replies: []string{`{"exploitable": false, "note": "constant test data, not reachable"}`}}
	out := Verify(context.Background(), "/root", base(), r)

	if len(out) != 1 {
		t.Fatalf("refuted finding was dropped; want it kept: %+v", out)
	}
	if out[0].Verified {
		t.Error("refuted finding should not be marked verified")
	}
	if !strings.Contains(strings.ToLower(out[0].VerifyNote), "not") {
		t.Errorf("refutation note not recorded: %q", out[0].VerifyNote)
	}
}

// TestVerifyToleratesProseWrappedJSON: local models wrap JSON in chatter.
func TestVerifyToleratesProseWrappedJSON(t *testing.T) {
	r := &fakeReviewer{replies: []string{"Sure!\n```json\n{\"exploitable\": true, \"note\": \"yes\"}\n```\n"}}
	out := Verify(context.Background(), "/root", base(), r)
	if !out[0].Verified {
		t.Errorf("did not parse JSON out of prose: %+v", out)
	}
}

// TestVerifyUnparseableReplyLeavesFindingUnverified: garbage in must not crash
// or fabricate a verdict.
func TestVerifyUnparseableReplyLeavesFindingUnverified(t *testing.T) {
	r := &fakeReviewer{replies: []string{"I don't know"}}
	out := Verify(context.Background(), "/root", base(), r)
	if len(out) != 1 || out[0].Verified {
		t.Errorf("unparseable reply produced a verdict: %+v", out)
	}
}

// TestVerifyReviewerErrorLeavesFindingsIntact: an offline model degrades to the
// static findings unchanged, never fewer.
func TestVerifyReviewerErrorLeavesFindingsIntact(t *testing.T) {
	r := &fakeReviewer{err: errors.New("connection refused")}
	in := base()
	out := Verify(context.Background(), "/root", in, r)
	if len(out) != len(in) {
		t.Fatalf("reviewer error changed the finding count: %d -> %d", len(in), len(out))
	}
	if out[0].Verified {
		t.Error("errored review should leave findings unverified")
	}
}

// TestVerifyNilReviewerIsStaticOnly: no reviewer at all just returns the
// findings.
func TestVerifyNilReviewerIsStaticOnly(t *testing.T) {
	in := base()
	out := Verify(context.Background(), "/root", in, nil)
	if len(out) != len(in) {
		t.Fatalf("nil reviewer changed findings")
	}
}
