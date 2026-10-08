package executor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/packer"
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
	gs, err := guidanceFrom(d)
	if err != nil {
		t.Fatal(err)
	}
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
	gs, err := guidanceFrom(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(gs) != 1 || gs[0].Section != "construction" {
		t.Errorf("guidance = %+v, want only the construction section", gs)
	}
}

func TestGuidanceFromAnOldNodeIsEmpty(t *testing.T) {
	var d leafDoc
	if err := json.Unmarshal([]byte(`{"title":"t"}`), &d); err != nil {
		t.Fatal(err)
	}
	if gs, err := guidanceFrom(d); err != nil || len(gs) != 0 {
		t.Errorf("a node planned before the groups gave guidance: %+v, %v", gs, err)
	}
}

// A security group that exists but cannot be read must stop the prompt build,
// naming the group; silently dropping it would send the implement call out
// without its warnings.
func TestGuidanceFromFailsClosedOnAnUnreadableGroup(t *testing.T) {
	for _, group := range []string{"security", "observability", "performance", "portability"} {
		t.Run(group, func(t *testing.T) {
			var d leafDoc
			raw := `{"` + group + `":{"threats":"not a list","log_events":"x","metrics":"x","os":"linux","max_latency_ms":"slow"}}`
			if err := json.Unmarshal([]byte(raw), &d); err != nil {
				t.Fatal(err)
			}
			gs, err := guidanceFrom(d)
			if err == nil || !strings.Contains(err.Error(), "the "+group+" group is not readable") {
				t.Fatalf("guidance = %+v, err = %v, want an error naming the %s group", gs, err, group)
			}
		})
	}
}

func TestGuidanceFromRendersAnEmptyGoMinSensibly(t *testing.T) {
	var d leafDoc
	if err := json.Unmarshal([]byte(`{"portability":{"os":["linux"],"arch":[],"go_min":"","cgo":false,"deps":[]}}`), &d); err != nil {
		t.Fatal(err)
	}
	gs, err := guidanceFrom(d)
	if err != nil || len(gs) != 1 {
		t.Fatalf("guidance = %+v, err = %v", gs, err)
	}
	text := strings.Join(gs[0].Lines, "|")
	if strings.Contains(text, "Go  or newer") || strings.Contains(text, "Go or newer") || strings.Contains(text, "Architectures: |") || strings.HasSuffix(text, "Architectures: ") {
		t.Errorf("empty values were rendered as blanks: %q", text)
	}
	if !strings.Contains(text, "Operating systems: linux") || !strings.Contains(text, "cgo: not allowed") {
		t.Errorf("lines = %q", text)
	}
}

// From the planned node through View to the prompt: the build groups arrive,
// the reasoning of the node does not.
func TestAPlannedNodeReachesThePromptWithGuidanceButNoReasoning(t *testing.T) {
	g := newRig(t)
	l := g.plan.Leaf("fn-greet")
	if l == nil || len(l.Guidance) == 0 {
		t.Fatalf("fn-greet has no guidance: %+v", l)
	}
	matches, _ := filepath.Glob(filepath.Join(g.runDir, "*", "fn-greet.json"))
	if len(matches) != 1 {
		t.Fatalf("node files = %v", matches)
	}
	raw, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Rationale    string `json:"rationale"`
		Construction struct {
			Approach string `json:"approach_chosen"`
		} `json:"construction"`
		Alternatives json.RawMessage `json:"alternatives"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Rationale == "" || doc.Construction.Approach == "" {
		t.Fatalf("node = %s, %v", raw, err)
	}
	packed, err := packer.Pack(g.plan.View(l, "package greet\n"), g.plan.Contracts, packer.Inputs{Budget: 100000})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(packed.Text, "Approach: "+doc.Construction.Approach) {
		t.Errorf("the prompt lacks the construction approach:\n%s", packed.Text)
	}
	if strings.Contains(packed.Text, doc.Rationale) {
		t.Errorf("the node's rationale reached the prompt:\n%s", packed.Text)
	}
	if strings.Contains(packed.Text, "rationale") || strings.Contains(packed.Text, "decision_ids") {
		t.Errorf("a reasoning field name reached the prompt:\n%s", packed.Text)
	}
}
