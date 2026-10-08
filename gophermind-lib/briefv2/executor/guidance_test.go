package executor

import (
	"encoding/json"
	"strings"
	"testing"
)

const guidanceNodeJSON = `{
 "title":"POST /register",
 "construction":{"approach_chosen":"decode then validate","steps":["Decode the body.","Validate email then username."]},
 "security":{"trust_boundary":"external_input","untrusted_inputs":["r"],"threats":[{"threat":"huge body","mitigation":"limit it"}]},
 "observability":{"log_events":[{"level":"warn","msg":"crm push failed","fields":["error"]}],"metrics":[{"name":"register_total","kind":"counter"}]},
 "performance":{"complexity":"O(1)","max_latency_ms":200,"alloc_budget":"under 10 KB","concurrency":"safe_for_concurrent_use","hot_path":true},
 "portability":{"os":["linux","darwin"],"arch":["amd64"],"go_min":"1.22","cgo":false,"deps":["github.com/google/uuid"]},
 "rationale":"CANARY-rationale","alternatives":[{"approach":"CANARY-alt","rejected_because":"CANARY-why"}],
 "refactor_notes":[{"what":"CANARY-refactor","why":"w","when":"n"}],"assumptions":["CANARY-assume"],"open_questions":[],
 "decision_ids":["q1"],"decisions":[{"id":"q1","question":"CANARY-question","answer":"CANARY-answer"}]}`

func TestGuidanceFromReadsOnlyTheBuildGroups(t *testing.T) {
	var d leafDoc
	if err := json.Unmarshal([]byte(guidanceNodeJSON), &d); err != nil {
		t.Fatal(err)
	}
	gs := guidanceFrom(d)
	var all strings.Builder
	for _, g := range gs {
		all.WriteString(g.Section + ":" + strings.Join(g.Lines, "|") + "\n")
	}
	text := all.String()
	for _, want := range []string{
		"construction:Approach: decode then validate|Step 1: Decode the body.|Step 2: Validate email then username.",
		"security:Trust boundary: external_input|Untrusted input: r|Threat: huge body. Mitigation: limit it.",
		`observability:Log at warn: "crm push failed" with fields error|Metric: register_total (counter)`,
		"performance:Complexity: O(1)|Latency budget: 200 ms|Allocation budget: under 10 KB|Concurrency: safe_for_concurrent_use|Hot path: yes",
		"portability:Go 1.22 or newer|Operating systems: linux, darwin|Architectures: amd64|cgo: not allowed|Dependencies: github.com/google/uuid",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("guidance lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "CANARY") {
		t.Errorf("reasoning from the brief reached the guidance:\n%s", text)
	}
	order := []string{}
	for _, g := range gs {
		order = append(order, g.Section)
	}
	if strings.Join(order, ",") != "construction,security,observability,performance,portability" {
		t.Errorf("sections = %v", order)
	}
}

func TestGuidanceFromSkipsNotApplicableGroups(t *testing.T) {
	var d leafDoc
	raw := `{"construction":{"approach_chosen":"a","steps":["one","two"]},
 "security":{"not_applicable":"it handles no input and holds no secret at all"},
 "observability":{"not_applicable":"a pure function has nothing to log or measure"},
 "performance":{"not_applicable":"a pure function with no latency requirement of its own"},
 "portability":{"not_applicable":"nothing here depends on the platform in any way"}}`
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatal(err)
	}
	gs := guidanceFrom(d)
	if len(gs) != 1 || gs[0].Section != "construction" {
		t.Errorf("guidance = %+v, want only the construction section", gs)
	}
}

func TestGuidanceFromAnOldNodeIsEmpty(t *testing.T) {
	var d leafDoc
	if err := json.Unmarshal([]byte(`{"title":"t"}`), &d); err != nil {
		t.Fatal(err)
	}
	if gs := guidanceFrom(d); len(gs) != 0 {
		t.Errorf("a node planned before the groups gave guidance: %+v", gs)
	}
}
