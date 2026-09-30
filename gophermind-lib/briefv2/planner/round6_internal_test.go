package planner

import (
	"fmt"
	"strings"
	"testing"
)

func bigDoc(n int) map[string]any {
	doc := map[string]any{}
	var comps, types []any
	comps = append(comps, map[string]any{"id": "types", "exports": []any{}})
	types = append(types, map[string]any{"id": "shared-entity"})
	for i := 0; i < n; i++ {
		comps = append(comps, map[string]any{"id": fmt.Sprintf("component-number-%04d", i), "exports": []any{}})
		types = append(types, map[string]any{"id": fmt.Sprintf("type-number-%04d", i)})
	}
	doc["components"], doc["types"] = comps, types
	return doc
}

func TestEmittedTextIsBoundedAndKeepsTheSharedAndTheRecentIDs(t *testing.T) {
	doc := bigDoc(400)
	text := outlineEmittedText(doc, []string{"types", "shared-entity"})
	if len(text) > maxEmittedTextBytes {
		t.Errorf("emitted text is %d bytes, cap %d", len(text), maxEmittedTextBytes)
	}
	for _, want := range []string{"types", "shared-entity", "component-number-0399", "type-number-0399", "names withheld"} {
		if !strings.Contains(text, want) {
			t.Errorf("emitted text lacks %q", want)
		}
	}
	if strings.Contains(text, "type-number-0000") {
		t.Error("an old type id is listed in full")
	}
	if n := strings.Count(text, "component-number-"); n > maxEmittedRecent+maxEmittedSummaries {
		t.Errorf("%d component ids listed", n)
	}
	// Under the cap the list is the plain one.
	small := outlineEmittedText(bigDoc(3), nil)
	if strings.Contains(small, "withheld") || !strings.Contains(small, "components: types, component-number-0000") {
		t.Errorf("small text = %q", small)
	}
}

func TestOutlinePassIsFinalWhateverTheBatchCount(t *testing.T) {
	two := [][]string{{"a"}, {"b"}}
	cases := []struct {
		name    string
		first   bool
		batches [][]string
		next    int
		repair  bool
		want    bool
	}{
		{"shared pass with no batches", true, nil, 0, false, true},
		{"shared pass with batches", true, two, 0, false, false},
		{"first batch of two", false, two, 0, false, false},
		{"last batch", false, two, 1, false, true},
		{"repair", false, two, 2, true, true},
	}
	for _, c := range cases {
		if got := outlinePassIsFinal(c.first, c.batches, c.next, c.repair); got != c.want {
			t.Errorf("%s: final = %v, want %v", c.name, got, c.want)
		}
	}
}

// The separator of a pending reference is reserved: a model's own "|" is an
// ordinary character to fold into a dash.
func TestPipeInAModelIDIsFoldedAway(t *testing.T) {
	if got := normalizeID("a|fn-a", false); got != "a-fn-a" {
		t.Errorf("type id = %q", got)
	}
	doc, _, err := parseOutline(okOutline, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	out, _, err := normalizeReply(doc, `{"types": [], "functions": [{"id": "fn-a", "package": "x", "file": "internal/x/a.go", "signature": "func A()", "doc": "d", "uses": ["a|fn-a"]}]}`, replyComponent)
	if err != nil {
		t.Fatal(err)
	}
	merged, _, _, err := mergePass(doc, "greeting", out, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	if got := usesOf(merged, "functions", "fn-a"); got != "a-fn-a|fn-a-fn-a" {
		t.Errorf("uses = %q, want a pending reference built from the folded id, not the model's own pair", got)
	}
	if out2, _, err := normalizeReply(nil, `{"components": [{"id": "x|y", "package": "x"}]}`, replyOutline); err != nil || !strings.Contains(out2, `"x-y"`) {
		t.Errorf("component id with a pipe: %v %s", err, out2)
	}
}

func TestDropSharedExtrasKeepsOnlyTheTypesComponent(t *testing.T) {
	in := `{"module": "m", "components": [{"id": "types", "package": "x"}, {"id": "greeting", "package": "x"}, {"id": "farewell", "package": "x"}], "types": [{"id": "t"}]}`
	out, dropped, err := dropSharedExtras(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(dropped) != 2 || dropped[0] != `component "greeting"` {
		t.Errorf("dropped = %v", dropped)
	}
	if strings.Contains(out, "greeting") || !strings.Contains(out, `"types"`) || !strings.Contains(out, `"module"`) {
		t.Errorf("out = %s", out)
	}
	if out2, d, _ := dropSharedExtras("prose"); out2 != "prose" || len(d) != 0 {
		t.Errorf("non-JSON changed: %q %v", out2, d)
	}
}
