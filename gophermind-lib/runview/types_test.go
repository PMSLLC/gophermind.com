package runview

import (
	"encoding/json"
	"sort"
	"testing"
)

func keysOf(t *testing.T, v any) []string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestWireFieldNamesMatchTheSpec(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want []string
	}{
		{"RunSummary", RunSummary{}, []string{"counts", "coverage", "planned_at", "repo_name", "run_id", "started_at", "status", "stop_reason", "title", "updated_at", "waves"}},
		{"NodeSummary", NodeSummary{}, []string{"attempt_count", "claimed_by", "depends_on", "id", "kind", "last_attempt", "model_tier", "node_class", "parent", "requirement_ids", "result", "revision", "status", "tests", "title", "wave"}},
		{"Attempt", Attempt{}, []string{"duration_ms", "failed_tests", "failure_reason", "model", "order", "provider", "revision", "started_at", "tests_passed", "tests_total", "verdict"}},
		{"StateBody", StateBody{}, []string{"event_seq", "generated_at", "nodes", "run", "schema_version"}},
		{"RunsBody", RunsBody{}, []string{"runs", "schema_version"}},
		{"NodeBody", NodeBody{}, []string{"artifacts", "node", "runtime", "schema_version"}},
		{"DecisionsBody", DecisionsBody{}, []string{"decisions", "schema_version"}},
		{"Counts", Counts{}, []string{"by_status", "nodes"}},
	}
	for _, c := range cases {
		if got := keysOf(t, c.v); !eq(got, c.want) {
			t.Errorf("%s keys = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestReportBodyCompleteKey(t *testing.T) {
	var b ReportBody
	if !b.IsComplete() {
		t.Error("a report with no complete key must read as complete")
	}
	no := false
	b.Complete = &no
	if b.IsComplete() {
		t.Error("complete:false must read as incomplete")
	}
	raw, _ := json.Marshal(b)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m["complete"] != false {
		t.Errorf("complete = %v, want false", m["complete"])
	}
}
