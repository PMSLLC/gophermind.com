package planner

import (
	"encoding/json"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/contract"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/settings"
)

func TestTierForFollowsTheNodeUnlessTheSettingIsOff(t *testing.T) {
	p := &Planner{d: Deps{Settings: settings.Default()}}
	for in, want := range map[string]router.Tier{"strong": router.TierStrong, "standard": router.TierStandard, "any": router.TierAny, "": router.TierStrong, "weird": router.TierStrong} {
		if got := p.tierFor(in); got != want {
			t.Errorf("tierFor(%q) = %q, want %q", in, got, want)
		}
	}
	off := false
	p.d.Settings.Defaults.RouteByNodeTier = &off
	if got := p.tierFor("any"); got != router.TierStrong {
		t.Errorf("with route_by_node_tier false every stage is strong, got %q", got)
	}
}

func TestBatchTierIsTheStrongestInTheBatch(t *testing.T) {
	p := &Planner{d: Deps{Settings: settings.Default()}}
	mk := func(tiers ...string) []map[string]any {
		var out []map[string]any
		for _, t := range tiers {
			out = append(out, map[string]any{"model_tier": t})
		}
		return out
	}
	for want, in := range map[router.Tier][]string{router.TierAny: {"any", "any"}, router.TierStandard: {"any", "standard"}, router.TierStrong: {"any", "strong", "standard"}} {
		if got := p.batchTier(mk(in...)); got != want {
			t.Errorf("batchTier(%v) = %q, want %q", in, got, want)
		}
	}
	if got := p.batchTier(nil); got != router.TierStrong {
		t.Errorf("an empty batch is strong, got %q", got)
	}
}

func TestNextBatchSizeHalvesOnATruncatedReply(t *testing.T) {
	for _, tc := range []struct {
		size, n   int
		truncated bool
		want      int
	}{{4, 4, true, 2}, {2, 2, true, 1}, {1, 1, true, 1}, {4, 4, false, 4}, {4, 3, true, 1}} {
		if got := nextBatchSize(tc.size, tc.n, tc.truncated); got != tc.want {
			t.Errorf("nextBatchSize(%d,%d,%v) = %d, want %d", tc.size, tc.n, tc.truncated, got, tc.want)
		}
	}
}

func draftFixture(t *testing.T) map[string]any {
	t.Helper()
	var d map[string]any
	raw := `{"spec_version":"2.0","id":"fn-register","kind":"function","parent":"registration","title":"POST /register","description":"Decode, validate, create, respond.",
 "brief_ref":"#features/registration","status":"pending","revision":0,"model_tier":"standard","node_class":"handler","depends_on":[],
 "context":{"dependency_signatures":[],"constraints":[]},
 "contract":{"package":"httpapi","file":"internal/httpapi/register.go","signature":"func (s *Server) HandleRegister(w http.ResponseWriter, r *http.Request)",
  "inputs":[{"name":"w","type":"the response writer"},{"name":"r","type":"the request"}],"outputs":[],
  "errors":[{"when":"body is not valid JSON","returns":"400 BAD_JSON"},{"when":"email taken","returns":"409 EMAIL_TAKEN"}],"side_effects":[]}}`
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func contractFixtureForEnrich() *contract.Contracts {
	return &contract.Contracts{Module: "example.com/acme", Components: []contract.Component{{ID: "registration", Package: "httpapi"}}}
}

func replyFixture(t *testing.T, edit func(m map[string]any)) map[string]any {
	t.Helper()
	m := groupsDoc(t, nil) // the valid handler groups of groups_test.go
	delete(m, "assumptions")
	m["assumptions"] = []any{}
	m["error_kinds"] = []any{"wrapped", "sentinel"}
	if edit != nil {
		edit(m)
	}
	return m
}

func enrichEnvFixture() groupEnv {
	e := testEnv()
	e.Params = map[string]bool{"w": true, "r": true}
	return e
}

func TestMergeEnrichmentFillsGroupsKindsAndGoTypes(t *testing.T) {
	merged, ds, warns := mergeEnrichment(draftFixture(t), replyFixture(t, nil), contractFixtureForEnrich(), enrichEnvFixture())
	if len(ds) != 0 {
		t.Fatalf("defects: %v", kindsOf(ds))
	}
	if len(warns) != 0 {
		t.Errorf("warnings: %v", warns)
	}
	ct := merged["contract"].(map[string]any)
	errs := objects(ct["errors"])
	if errs[0]["kind"] != "wrapped" || errs[1]["kind"] != "sentinel" {
		t.Errorf("error kinds = %v", errs)
	}
	in := objects(ct["inputs"])
	if in[0]["go_type"] != "http.ResponseWriter" || in[1]["go_type"] != "*http.Request" {
		t.Errorf("go_type = %v", in)
	}
	if merged["rationale"] == nil || merged["security"] == nil || merged["node_class"] != "handler" {
		t.Errorf("merged = %v", merged)
	}
	if _, has := merged["error_kinds"]; has {
		t.Error("error_kinds is a reply field, not a node field")
	}
}

func TestMergeEnrichmentDefects(t *testing.T) {
	cases := []struct {
		name string
		edit func(m map[string]any)
		kind string
	}{
		{"a missing group", func(m map[string]any) { delete(m, "security") }, grpMissing},
		{"too few error kinds", func(m map[string]any) { m["error_kinds"] = []any{"wrapped"} }, grpEnum},
		{"an unknown decision id", func(m map[string]any) { m["decision_ids"] = []any{"q9"} }, grpDecision},
		{"a shape the schema refuses", func(m map[string]any) {
			m["performance"] = map[string]any{"complexity": "O(1)", "concurrency": "maybe", "hot_path": false}
		}, grpSchema},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ds, _ := mergeEnrichment(draftFixture(t), replyFixture(t, tc.edit), contractFixtureForEnrich(), enrichEnvFixture())
			if !hasKind(ds, tc.kind) {
				t.Fatalf("kinds = %v, want %s", kindsOf(ds), tc.kind)
			}
		})
	}
}

func TestMergeEnrichmentTurnsAnUnresolvedTypeIntoAWarningNotAFailure(t *testing.T) {
	d := draftFixture(t)
	d["contract"].(map[string]any)["signature"] = "func (s *Server) HandleRegister(w http.ResponseWriter, r *http.Request, store Ghost)"
	d["contract"].(map[string]any)["inputs"] = []any{map[string]any{"name": "w", "type": "x"}, map[string]any{"name": "r", "type": "x"}, map[string]any{"name": "store", "type": "x"}}
	e := enrichEnvFixture()
	e.Params["store"] = true
	_, ds, warns := mergeEnrichment(d, replyFixture(t, nil), contractFixtureForEnrich(), e)
	if len(ds) != 0 {
		t.Fatalf("defects: %v", kindsOf(ds))
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "fn-register") {
		t.Errorf("warnings = %v", warns)
	}
}

func TestEnrichDefectMessagesQuoteNothing(t *testing.T) {
	const canary = "CANARY-enrich-4410"
	_, ds, warns := mergeEnrichment(draftFixture(t), replyFixture(t, func(m map[string]any) {
		m["rationale"] = canary
		m["decision_ids"] = []any{canary}
		m["error_kinds"] = []any{canary, canary}
		m["security"].(map[string]any)["secret_use"] = []any{canary}
	}), contractFixtureForEnrich(), enrichEnvFixture())
	for _, d := range ds {
		if strings.Contains(d.msg, canary) {
			t.Errorf("a %s message quotes the reply: %q", d.kind, d.msg)
		}
	}
	for _, w := range warns {
		if strings.Contains(w, canary) {
			t.Errorf("a warning quotes the reply: %q", w)
		}
	}
}

func TestFieldsOfGroupDefectsAreAskedForAgain(t *testing.T) {
	ds := []groupDefect{{grpMissing, "security", ""}, {grpNAForbid, "observability", ""}, {grpEnum, "error_kinds", ""}, {grpSchema, "", ""}}
	got := fieldsOfGroup(ds)
	for _, want := range []string{"security", "observability", "error_kinds"} {
		if !contains(got, want) {
			t.Errorf("fields = %v lacks %s", got, want)
		}
	}
	if len(fieldsOfGroup([]groupDefect{{grpSchema, "", ""}})) != len(enrichFieldsAll) {
		t.Error("a schema defect with no named field asks for every group again")
	}
}

func TestEnrichQuestionIdsPassTheIdPattern(t *testing.T) {
	for _, stage := range []string{"enrich:greeting", enrichFixStage, "enrich_comp:greeting", "enrich_root"} {
		id := strings.ReplaceAll(stage, ":", "-") + "-q1"
		if !decisionIDRE.MatchString(id) {
			t.Errorf("the id %q a question in stage %s would get does not match the decision id pattern", id, stage)
		}
	}
}
