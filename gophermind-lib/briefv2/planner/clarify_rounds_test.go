package planner_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
)

// fixtureDir writes canned replies for the stages a test overrides.
func fixtureDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const chainQuestions = `[
 {"id":"q1","question":"Which store?","why_it_matters":"types","options":["memory","postgres"],"recommended":"memory","recommended_why":"no database is declared"},
 {"id":"q2","question":"Which postgres driver?","depends_on":["q1"],"options":["pgx","lib/pq"],"recommended":"pgx"},
 {"id":"q3","question":"Pool size?","depends_on":["q2"],"recommended":"10"}]`

type storedQ struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	Answer     string `json:"answer"`
	AnsweredBy string `json:"answered_by"`
	Round      int    `json:"round"`
	Kind       string `json:"kind"`
}

type storedState struct {
	Calls     int       `json:"calls"`
	Complete  bool      `json:"complete"`
	Questions []storedQ `json:"questions"`
}

func readStore(t *testing.T, runDir string) storedState {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(runDir, "_state", "clarify", "questions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var st storedState
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	return st
}

func assumeBrief(g *rig) {
	g.briefPath = writeBrief(g.t, g.repo, func(s string) string {
		return strings.Replace(s, "on_ambiguity: halt", "on_ambiguity: assume_and_document", 1)
	})
}

func TestClarifyAsksOneFrontierPerRound(t *testing.T) {
	gate := approving()
	g := newRig(t, gate, fixtureDir(t, map[string]string{"clarify.txt": chainQuestions, "clarify.more.txt": "[]"}))
	if out, err := g.plan(planner.Options{StopAfter: "clarify"}); err != nil || out != planner.Done {
		t.Fatalf("plan = %v, %v", out, err)
	}
	if want := [][]string{{"q1"}, {"q2"}, {"q3"}}; !reflect.DeepEqual(gate.rounds, want) {
		t.Fatalf("rounds = %v, want %v", gate.rounds, want)
	}
	st := readStore(t, g.runDir)
	if !st.Complete || st.Calls != 2 || len(st.Questions) != 3 {
		t.Fatalf("store = %+v", st)
	}
	for i, q := range st.Questions {
		if q.Status != "settled" || q.AnsweredBy != "human" || q.Round != i+1 {
			t.Errorf("question %d = %+v", i+1, q)
		}
	}
	for _, id := range []string{"q1", "q2", "q3"} {
		if _, err := os.Stat(filepath.Join(g.runDir, "decisions", id+".md")); err != nil {
			t.Errorf("decision record %s: %v", id, err)
		}
	}
	raw, _ := os.ReadFile(filepath.Join(g.runDir, "answers.json"))
	if strings.Count(string(raw), `"question"`) != 3 {
		t.Errorf("answers.json is not the view of the store:\n%s", raw)
	}
}

func TestClarifyCapForcesAFinalRoundOfEverythingStillOpen(t *testing.T) {
	gate := approving()
	g := newRig(t, gate, fixtureDir(t, map[string]string{"clarify.txt": chainQuestions, "clarify.more.txt": "[]"}))
	g.cfg.Defaults.ClarifyMaxCalls = 1
	if _, err := g.plan(planner.Options{StopAfter: "clarify"}); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"q1", "q2", "q3"}}; !reflect.DeepEqual(gate.rounds, want) {
		t.Fatalf("rounds = %v, want one last round of everything: %v", gate.rounds, want)
	}
	if st := readStore(t, g.runDir); st.Calls != 1 || !st.Complete {
		t.Errorf("store = %+v", st)
	}
}

func TestClarifyUnattendedTakesRecommendationsAndStillRunsEveryRound(t *testing.T) {
	gate := approving()
	g := newRig(t, gate, fixtureDir(t, map[string]string{"clarify.txt": chainQuestions, "clarify.more.txt": "[]"}))
	assumeBrief(g)
	if _, err := g.plan(planner.Options{StopAfter: "clarify"}); err != nil {
		t.Fatal(err)
	}
	if len(gate.rounds) != 0 {
		t.Fatalf("an unattended run asked the gate: %v", gate.rounds)
	}
	st := readStore(t, g.runDir)
	want := map[string]string{"q1": "memory", "q2": "pgx", "q3": "10"}
	for _, q := range st.Questions {
		if q.AnsweredBy != "unattended-default" || q.Answer != want[q.ID] {
			t.Errorf("%s = %+v", q.ID, q)
		}
	}
	if st.Calls != 2 {
		t.Errorf("calls = %d, want 2 (the dependent questions still came from a follow-up call)", st.Calls)
	}
}

func TestClarifyUnattendedRefusesADecisionWithoutARecommendation(t *testing.T) {
	g := newRig(t, approving(), fixtureDir(t, map[string]string{
		"clarify.txt": `[{"id":"q1","question":"Which store?"}]`, "clarify.more.txt": "[]"}))
	assumeBrief(g)
	if _, err := g.plan(planner.Options{StopAfter: "clarify"}); err == nil || !strings.Contains(err.Error(), "clarify") {
		t.Fatalf("err = %v, want a clarify failure", err)
	}
}

func TestClarifyAnswersAFactByCodeAndNeverAsksIt(t *testing.T) {
	gate := approving()
	g := newRig(t, gate, fixtureDir(t, map[string]string{
		"clarify.txt": `[{"id":"q1","kind":"fact","fact_key":"go_module","question":"What is the module path?"},
 {"id":"q2","question":"Which store?","recommended":"memory"}]`,
		"clarify.more.txt": "[]"}))
	if err := os.WriteFile(filepath.Join(g.repo, "go.mod"), []byte("module example.com/acme\n\ngo 1.22\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := g.plan(planner.Options{StopAfter: "clarify"}); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"q2"}}; !reflect.DeepEqual(gate.rounds, want) {
		t.Fatalf("rounds = %v, want only the decision", gate.rounds)
	}
	for _, q := range readStore(t, g.runDir).Questions {
		if q.ID == "q1" && (q.AnsweredBy != "probe" || q.Answer != "example.com/acme") {
			t.Errorf("q1 = %+v", q)
		}
	}
}

func TestClarifyShowsAFactTheProbeCannotAnswer(t *testing.T) {
	gate := approving()
	g := newRig(t, gate, fixtureDir(t, map[string]string{
		"clarify.txt":      `[{"id":"q1","kind":"fact","fact_key":"go_module","question":"What is the module path?","recommended":"example.com/acme"}]`,
		"clarify.more.txt": "[]"}))
	if _, err := g.plan(planner.Options{StopAfter: "clarify"}); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"q1"}}; !reflect.DeepEqual(gate.rounds, want) {
		t.Fatalf("rounds = %v: a fact the harness cannot answer goes to the person", gate.rounds)
	}
}

var emptyBlockRE = regexp.MustCompile("(?m)^```answer (q[0-9]+)\n\n```")

func TestClarifyResumesAFileGateRoundWithoutAskingTheModelAgain(t *testing.T) {
	dir := t.TempDir()
	g := newRig(t, human.NewFile(dir), fixtureDir(t, map[string]string{"clarify.txt": chainQuestions, "clarify.more.txt": "[]"}))
	clarifyCalls := func() int {
		a, _ := g.led.List(context.Background(), greeterID, ledger.Filter{Stage: "clarify"})
		b, _ := g.led.List(context.Background(), greeterID, ledger.Filter{Stage: "clarify:more"})
		return len(a) + len(b)
	}
	for i := 0; i < 8; i++ {
		opts := planner.Options{StopAfter: "clarify"}
		if i > 0 {
			opts.RunID = greeterID
		}
		out, err := g.plan(opts)
		if err != nil {
			t.Fatal(err)
		}
		if out == planner.Done {
			if got := clarifyCalls(); got != 2 {
				t.Errorf("the model was called %d times for Clarify, want 2", got)
			}
			if _, err := os.Stat(filepath.Join(dir, "QUESTIONS.answered.md")); err != nil {
				t.Errorf("an answered round was not archived: %v", err)
			}
			return
		}
		raw, err := os.ReadFile(filepath.Join(dir, "QUESTIONS.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "Round ") || !strings.Contains(string(raw), "Recommended: ") {
			t.Errorf("QUESTIONS.md does not show the round and the recommendation:\n%s", raw)
		}
		filled := emptyBlockRE.ReplaceAllString(string(raw), "```answer $1\naccept\n```")
		if err := os.WriteFile(filepath.Join(dir, "QUESTIONS.md"), []byte(filled), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("the file gate never finished")
}

func TestClarifyOpenQuestionOffTheFrontierIsAnErrorNotASilentFinish(t *testing.T) {
	for name, deps := range map[string]string{"unknown": `["zz"]`, "cycle": `["q2"]`} {
		t.Run(name, func(t *testing.T) {
			g := newRig(t, approving(), fixtureDir(t, map[string]string{"clarify.txt": "[]", "clarify.more.txt": "[]"}))
			g.mustPlan(planner.Options{StopAfter: "load"})
			store := `{"calls":1,"round":0,"complete":false,"questions":[
 {"id":"q1","question":"A?","kind":"decision","raised_by":"clarify","round":0,"status":"open","depends_on":` + deps + `},
 {"id":"q2","question":"B?","kind":"decision","raised_by":"clarify","round":0,"status":"open","depends_on":["q1"]}]}`
			p := filepath.Join(g.runDir, "_state", "clarify", "questions.json")
			if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(store), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := g.plan(planner.Options{RunID: greeterID, StopAfter: "clarify"})
			if err == nil || !strings.Contains(err.Error(), "q1") {
				t.Fatalf("err = %v, want an error naming q1", err)
			}
			if st := readStore(t, g.runDir); st.Complete {
				t.Error("the store was marked complete with a question still open")
			}
		})
	}
}

func TestClarifyOfARunPlannedBeforeTheStoreAsksNothingAgain(t *testing.T) {
	gate := approving()
	g := newRig(t, gate, fixtureDir(t, map[string]string{"clarify.txt": chainQuestions, "clarify.more.txt": "[]"}))
	g.mustPlan(planner.Options{StopAfter: "load"})
	if err := os.WriteFile(filepath.Join(g.runDir, "answers.json"),
		[]byte(`{"answers":[{"id":"q1","stage":"clarify","question":"Old?","answer":"kept","assumed":false}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "clarify"})
	if len(gate.rounds) != 0 || len(g.fake.Requests()) != 0 {
		t.Errorf("a legacy run was asked again: rounds %v, model calls %d", gate.rounds, len(g.fake.Requests()))
	}
}
