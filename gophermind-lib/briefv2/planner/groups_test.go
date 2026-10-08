package planner

import (
	"encoding/json"
	"strings"
	"testing"
)

const handlerGroupsJSON = `{
 "rationale":"Serves POST /register so a visitor can create an account.",
 "construction":{"approach_chosen":"decode, validate, create, push to the CRM, respond","steps":["Decode the JSON body.","Validate email then username.","Create the user in the store.","Push to the CRM without failing the request.","Write the JSON response."]},
 "alternatives":[{"approach":"validate in middleware","rejected_because":"the order of the checks is part of the contract"}],
 "portability":{"os":["linux","darwin"],"arch":["amd64","arm64"],"go_min":"1.22","cgo":false,"deps":["net/http","encoding/json","example.com/acme/store"]},
 "security":{"trust_boundary":"external_input","untrusted_inputs":["r"],"authz":"none","secret_use":["CRM_TOKEN"],"threats":[{"threat":"oversized request body","mitigation":"limit the body with http.MaxBytesReader"}]},
 "performance":{"complexity":"O(1)","max_latency_ms":200,"alloc_budget":"under 10 KB","concurrency":"safe_for_concurrent_use","hot_path":false},
 "observability":{"log_events":[{"level":"warn","msg":"crm push failed","fields":["error"]}],"metrics":[{"name":"register_total","kind":"counter"}],"trace_span":null},
 "refactor_notes":[{"what":"extract the CRM push","why":"shared with the update handler","when":"when a second caller appears"}],
 "profile_hooks":{"not_applicable":"a registration handler is not a hot path so no profiling hook is useful"},
 "assumptions":[],"open_questions":[],"decision_ids":["q1"]
}`

func testEnv() groupEnv {
	return groupEnv{
		Secrets:   map[string]bool{"CRM_TOKEN": true},
		Decisions: map[string]bool{"q1": true},
		Params:    map[string]bool{"w": true, "r": true},
		GoMin:     "1.23",
		DepMods:   []string{"example.com/acme", "github.com/google/uuid"},
	}
}

func groupsDoc(t *testing.T, edit func(m map[string]any)) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(handlerGroupsJSON), &m); err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(m)
	}
	return m
}

func kindsOf(ds []groupDefect) []string {
	var out []string
	for _, d := range ds {
		out = append(out, d.kind)
	}
	return out
}

func hasKind(ds []groupDefect, kind string) bool {
	for _, d := range ds {
		if d.kind == kind {
			return true
		}
	}
	return false
}

func TestAFullHandlerNodePassesEveryGroupCheck(t *testing.T) {
	if ds := groupDefects(groupsDoc(t, nil), "handler", testEnv()); len(ds) != 0 {
		t.Fatalf("defects on a valid node: %v", kindsOf(ds))
	}
}

func TestGroupDefectKinds(t *testing.T) {
	cases := []struct {
		name  string
		class string
		edit  func(m map[string]any)
		kind  string
	}{
		{"rationale missing", "handler", func(m map[string]any) { delete(m, "rationale") }, grpMissing},
		{"rationale too short", "handler", func(m map[string]any) { m["rationale"] = "short" }, grpMissing},
		{"security missing", "handler", func(m map[string]any) { delete(m, "security") }, grpMissing},
		{"decision_ids missing", "handler", func(m map[string]any) { delete(m, "decision_ids") }, grpMissing},
		{"security na on a handler", "handler", func(m map[string]any) {
			m["security"] = map[string]any{"not_applicable": "this handler never touches untrusted data at all"}
		}, grpNAForbid},
		{"observability na on a validation node", "validation", func(m map[string]any) {
			m["observability"] = map[string]any{"not_applicable": "validation functions do not log anything at all"}
		}, grpNAForbid},
		{"performance na on a handler", "handler", func(m map[string]any) {
			m["performance"] = map[string]any{"not_applicable": "this handler has no performance requirement of note"}
		}, grpNAForbid},
		{"na reason is a non-answer", "handler", func(m map[string]any) {
			m["profile_hooks"] = map[string]any{"not_applicable": "not applicable not applicable none"}
		}, grpNAReason},
		{"na reason too short", "handler", func(m map[string]any) { m["alternatives"] = map[string]any{"not_applicable": "none"} }, grpNAReason},
		{"one construction step", "handler", func(m map[string]any) {
			m["construction"] = map[string]any{"approach_chosen": "a", "steps": []any{"only one"}}
		}, grpConstruct},
		{"blank construction step", "handler", func(m map[string]any) {
			m["construction"] = map[string]any{"approach_chosen": "a", "steps": []any{"one", "  "}}
		}, grpConstruct},
		{"secret_use is not a declared secret", "handler", func(m map[string]any) {
			m["security"].(map[string]any)["secret_use"] = []any{"NOT_DECLARED"}
		}, grpSecret},
		{"untrusted input is not a parameter", "handler", func(m map[string]any) {
			m["security"].(map[string]any)["untrusted_inputs"] = []any{"zzz"}
		}, grpSecret},
		{"boundary without threats", "handler", func(m map[string]any) {
			m["security"].(map[string]any)["threats"] = []any{}
		}, grpThreats},
		{"go_min newer than the repo", "handler", func(m map[string]any) {
			m["portability"].(map[string]any)["go_min"] = "1.30"
		}, grpGoMin},
		{"dependency not planned", "handler", func(m map[string]any) {
			m["portability"].(map[string]any)["deps"] = []any{"github.com/evil/x"}
		}, grpDeps},
		{"format verb in a log message", "handler", func(m map[string]any) {
			m["observability"].(map[string]any)["log_events"] = []any{map[string]any{"level": "warn", "msg": "failed: %s"}}
		}, grpObsFields},
		{"secret name as a log field", "handler", func(m map[string]any) {
			m["observability"].(map[string]any)["log_events"] = []any{map[string]any{"level": "warn", "msg": "failed", "fields": []any{"crm_token"}}}
		}, grpObsFields},
		{"unknown decision id", "handler", func(m map[string]any) { m["decision_ids"] = []any{"q9"} }, grpDecision},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ds := groupDefects(groupsDoc(t, tc.edit), tc.class, testEnv())
			if !hasKind(ds, tc.kind) {
				t.Fatalf("kinds = %v, want %s", kindsOf(ds), tc.kind)
			}
		})
	}
}

func TestPureNodesMayAnswerNotApplicableWhereAllowed(t *testing.T) {
	m := groupsDoc(t, func(m map[string]any) {
		m["performance"] = map[string]any{"not_applicable": "a pure function with no latency requirement of its own"}
		m["observability"] = map[string]any{"not_applicable": "a pure function has nothing to log or measure"}
		m["security"] = map[string]any{"not_applicable": "it handles no untrusted input and holds no secret"}
	})
	if ds := groupDefects(m, "pure", testEnv()); len(ds) != 0 {
		t.Fatalf("defects: %v", kindsOf(ds))
	}
}

func TestGroupMessagesQuoteNothingFromTheReply(t *testing.T) {
	const canary = "CANARY-xyzzy-7731"
	m := groupsDoc(t, func(m map[string]any) {
		sec := m["security"].(map[string]any)
		sec["secret_use"] = []any{canary}
		sec["untrusted_inputs"] = []any{canary}
		m["portability"].(map[string]any)["deps"] = []any{"github.com/" + canary + "/x"}
		m["observability"].(map[string]any)["log_events"] = []any{map[string]any{"level": "warn", "msg": canary + " %s", "fields": []any{canary}}}
		m["decision_ids"] = []any{canary}
		m["alternatives"] = map[string]any{"not_applicable": canary}
	})
	for _, d := range groupDefects(m, "handler", testEnv()) {
		if strings.Contains(d.msg, canary) || strings.Contains(d.field, canary) {
			t.Errorf("a %s message quotes the reply: %q", d.kind, d.msg)
		}
	}
}

func TestOpenQuestionsMustBeEmpty(t *testing.T) {
	m := groupsDoc(t, func(m map[string]any) { m["open_questions"] = []any{"Which store?"} })
	if ds := openQuestionDefects(m); !hasKind(ds, grpOpenQ) {
		t.Fatalf("kinds = %v", kindsOf(ds))
	}
	if ds := openQuestionDefects(groupsDoc(t, nil)); len(ds) != 0 {
		t.Fatalf("kinds = %v", kindsOf(ds))
	}
}

func TestNaReasonOK(t *testing.T) {
	for s, want := range map[string]bool{
		"":                            false,
		"none":                        false,
		"not applicable":              false,
		"N/A N/A N/A N/A N/A N/A":     false,
		"pure function pure function": false,
		"this function never touches the network":          true,
		"it is a pure function that only formats a string": true,
	} {
		if got := naReasonOK(s); got != want {
			t.Errorf("naReasonOK(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestGoVersionLess(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		less bool
	}{{"1.22", "1.23", true}, {"1.23", "1.22", false}, {"1.22", "1.22", false}, {"1.22", "1.22.3", true}, {"go1.21", "1.22", true}, {"1.9", "1.10", true}} {
		if got := goVersionLess(tc.a, tc.b); got != tc.less {
			t.Errorf("goVersionLess(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.less)
		}
	}
}
