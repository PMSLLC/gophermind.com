package planner

import (
	"strings"
	"testing"
)

func enrichPromptData() map[string]string {
	return map[string]string{
		"Component": "{}", "Nodes": "[]", "BriefSection": "(none)", "Decisions": "(none)", "Facts": "(none)",
		"Deps": "(none)", "Secrets": "(none)", "Fields": "rationale", "Defects": "",
	}
}

// The portability example named linux and amd64, and the enrich model copied
// it onto every node of a brief that restricts no platform.
func TestEnrichPromptDoesNotSuggestLinuxAmd64(t *testing.T) {
	got, err := render("enrich", enrichPromptData())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `"os": ["any"]`) {
		t.Errorf("prompt has no \"os\": [\"any\"] example")
	}
	for _, w := range []string{"linux", "amd64"} {
		if strings.Contains(got, w) {
			t.Errorf("prompt still names %q", w)
		}
	}
	for _, s := range []string{
		"Name an OS or architecture only when the brief restricts one.",
		"Enrichment explains the accepted contract. Add no behaviour the brief does not require: no log events, metrics, sanitization, flags, parameters or dependencies the brief and the contract do not already state.",
	} {
		if !strings.Contains(got, s) {
			t.Errorf("prompt lacks %q", s)
		}
	}
}

func obsWithLogEvent(m map[string]any) {
	m["observability"] = map[string]any{
		"log_events": []any{map[string]any{"level": "info", "msg": "greeting built", "fields": []any{}}},
		"metrics":    []any{}, "trace_span": nil,
	}
}

func TestUnrequestedLogEventIsADefect(t *testing.T) {
	// A pure node: nothing it does is worth a log line.
	if ds := groupDefects(groupsDoc(t, obsWithLogEvent), "pure", testEnv()); !hasKind(ds, grpObsUnrequested) {
		t.Errorf("pure node with a log event: defects %v, want %s", kindsOf(ds), grpObsUnrequested)
	}
	// A handler where the brief or the conventions say no logging.
	env := testEnv()
	env.NoLogging = true
	if ds := groupDefects(groupsDoc(t, obsWithLogEvent), "handler", env); !hasKind(ds, grpObsUnrequested) {
		t.Errorf("no-logging handler with a log event: defects %v, want %s", kindsOf(ds), grpObsUnrequested)
	}
	// A handler with logging allowed keeps its log events.
	if ds := groupDefects(groupsDoc(t, obsWithLogEvent), "handler", testEnv()); hasKind(ds, grpObsUnrequested) {
		t.Errorf("handler with logging allowed: defects %v, want no %s", kindsOf(ds), grpObsUnrequested)
	}
	// An empty list of log events is never a defect.
	empty := func(m map[string]any) {
		m["observability"] = map[string]any{"log_events": []any{}, "metrics": []any{}, "trace_span": nil}
	}
	if ds := groupDefects(groupsDoc(t, empty), "handler", env); hasKind(ds, grpObsUnrequested) {
		t.Errorf("empty log events: defects %v", kindsOf(ds))
	}
}

func TestNoLoggingAskedReadsConstraintsAndConventions(t *testing.T) {
	reqs := []Requirement{{ID: "C1", Kind: ReqConstraint, Text: "No logging."}}
	if !noLoggingAsked(reqs, nil) {
		t.Error("constraint No logging. not seen")
	}
	if !noLoggingAsked(nil, map[string]any{"logging": "No logging: the program prints only its result."}) {
		t.Error("conventions logging not seen")
	}
	feat := []Requirement{{ID: "F1", Kind: ReqFeature, Text: "no logging of the name"}}
	if noLoggingAsked(feat, map[string]any{"logging": "log/slog at info"}) {
		t.Error("a feature sentence or a logging convention made no-logging true")
	}
}
