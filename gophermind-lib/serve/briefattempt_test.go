package serve

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/build/blackboard"
	"gophermind/gophermind-lib/runview/runviewtest"
)

// An attempt carries its model and provider; one recorded without them omits
// both keys, so the page leaves the columns out.
func TestAttemptModelAndProviderOmittedWhenAbsent(t *testing.T) {
	root := t.TempDir()
	f := runviewtest.Build(t, root, runviewtest.Building)
	// fn-bye is in progress with one attempt that has a model and provider.
	bare := blackboard.Attempt{Order: 2, StartedAt: runviewtest.T0.Add(time.Hour), DurationMS: 5, Verdict: blackboard.VerdictFail,
		FailureReason: "timeout: slow", TestsTotal: 2}
	if err := f.BB.AppendAttempt(context.Background(), f.RunID, "fn-bye", bare); err != nil {
		t.Fatal(err)
	}
	st, err := BuildBriefState(root, f.RunID, nil, false, "", stateNow)
	if err != nil {
		t.Fatal(err)
	}
	at := nodeByID(t, st, "fn-bye").Attempts
	if len(at) != 2 || at[0].Model != "qwen3.6" || at[0].Provider != "mini" || at[1].Model != "" || at[1].Provider != "" {
		t.Fatalf("attempts %+v", at)
	}
	b0, _ := json.Marshal(at[0])
	b1, _ := json.Marshal(at[1])
	if !strings.Contains(string(b0), `"model":"qwen3.6"`) || !strings.Contains(string(b0), `"provider":"mini"`) {
		t.Errorf("attempt 1 json %s", b0)
	}
	if strings.Contains(string(b1), `"model"`) || strings.Contains(string(b1), `"provider"`) {
		t.Errorf("attempt 2 json must omit model and provider: %s", b1)
	}
}
