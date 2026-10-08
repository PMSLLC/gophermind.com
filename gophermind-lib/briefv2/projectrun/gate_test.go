package projectrun

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/human"
)

func gateFor(t *testing.T, reqs, covered []string, writeCov bool) human.Gate {
	t.Helper()
	dir := t.TempDir()
	var rs []map[string]any
	for _, id := range reqs {
		rs = append(rs, map[string]any{"id": id, "kind": "constraint", "text": "x", "line": 1})
	}
	b, _ := json.Marshal(rs)
	if err := os.WriteFile(filepath.Join(dir, "requirements.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if writeCov {
		var cs []map[string]any
		for _, id := range covered {
			cs = append(cs, map[string]any{"requirement": id, "nodes": []string{"n"}, "root_tests": []string{}})
		}
		b, _ := json.Marshal(map[string]any{"rounds": 0, "covered": cs, "root_tests": []any{}, "warnings": []string{}})
		if err := os.WriteFile(filepath.Join(dir, "coverage.json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return newUnattendedGate(func() (string, error) { return dir, nil })
}

func TestUnattendedGateApprovesFullCoverage(t *testing.T) {
	g := gateFor(t, []string{"C1", "A1"}, []string{"A1", "C1"}, true)
	d, err := g.Approve(context.Background(), human.PlanSummary{Markdown: "m", Hash: "0123456789abcdef0123"})
	if err != nil || !d.Approved || d.By != "unattended" || d.Note != "plan 0123456789ab" {
		t.Fatalf("%+v %v", d, err)
	}
}

func TestUnattendedGateRefusesGap(t *testing.T) {
	ps := human.PlanSummary{Hash: "0123456789abcdef"}
	for name, c := range map[string]struct {
		g    human.Gate
		note string
	}{
		"gap":         {gateFor(t, []string{"C1", "A1"}, []string{"C1"}, true), "coverage 1 of 2"},
		"zero reqs":   {gateFor(t, nil, nil, true), "coverage 0 of 0"},
		"no coverage": {gateFor(t, []string{"C1"}, nil, false), "no coverage file"},
	} {
		d, err := c.g.Approve(context.Background(), ps)
		if err != nil || d.Approved || d.By != "unattended" || d.Note != c.note {
			t.Errorf("%s: %+v %v", name, d, err)
		}
	}
}

func TestUnattendedGateEscalateStops(t *testing.T) {
	g := gateFor(t, nil, nil, false)
	r, err := g.Escalate(context.Background(), human.Escalation{NodeID: "n"})
	if err != nil || r.Action != human.ActionStop || r.AnsweredBy != human.AnsweredByUnattended {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestUnattendedGateAskFailsLoudly(t *testing.T) {
	g := gateFor(t, nil, nil, false)
	if _, err := g.Ask(context.Background(), []human.Question{{ID: "q"}}); !errors.Is(err, ErrUnattended) {
		t.Fatalf("err = %v", err)
	}
}

func TestUnattendedGateConfirmFailsLoudly(t *testing.T) {
	g := gateFor(t, nil, nil, false)
	for _, u := range []human.Understanding{{}, {Markdown: "None.\n\nFrontier empty", Hash: "abc"}} {
		d, err := g.Confirm(context.Background(), u)
		if !errors.Is(err, ErrUnattended) || d.Approved {
			t.Fatalf("%+v %v", d, err)
		}
	}
	if strings.Contains(ErrUnattended.Error(), "  ") {
		t.Fatal("odd message")
	}
}

func TestUnattendedGateRefusesEmptyPlanHash(t *testing.T) {
	g := gateFor(t, []string{"C1"}, []string{"C1"}, true)
	d, err := g.Approve(context.Background(), human.PlanSummary{})
	if err != nil || d.Approved || d.Note == "" || d.Note == "plan " {
		t.Fatalf("%+v %v", d, err)
	}
}
