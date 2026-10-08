package planner_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
)

func chainDir(t *testing.T) string {
	return fixtureDir(t, map[string]string{"clarify.txt": chainQuestions, "clarify.more.txt": "[]"})
}

func TestUnattendedClarifyTakesRecommendationsOverHalt(t *testing.T) {
	gate := approving()
	g := newRig(t, gate, chainDir(t))
	if out, err := g.plan(planner.Options{Unattended: true, StopAfter: "clarify"}); err != nil || out != planner.Done {
		t.Fatalf("plan = %v, %v", out, err)
	}
	if len(gate.rounds) != 0 {
		t.Fatalf("the gate was asked: %v", gate.rounds)
	}
	st := readStore(t, g.runDir)
	want := map[string]string{"q1": "memory", "q2": "pgx", "q3": "10"}
	if st.Calls != 2 || len(st.Questions) != 3 {
		t.Fatalf("store = %+v", st)
	}
	for _, q := range st.Questions {
		if q.AnsweredBy != "unattended-default" || q.Answer != want[q.ID] {
			t.Errorf("%s = %+v", q.ID, q)
		}
	}
	as, err := planner.ReadAnswers(g.runDir)
	if err != nil || len(as) != 3 {
		t.Fatalf("ReadAnswers = %v, %v", as, err)
	}
	for _, a := range as {
		if !a.Assumed {
			t.Errorf("answer %s not assumed", a.ID)
		}
	}
	if !g.has("decisions/q1.md") {
		t.Error("no decisions/q1.md")
	}
	if g.has("QUESTIONS.md") {
		t.Error("QUESTIONS.md written")
	}
}

func TestUnattendedClarifyRequiresRecommendations(t *testing.T) {
	good := `[{"id":"q1","question":"Which store?","recommended":"memory"}]`
	gate := approving()
	g := newRig(t, gate, variant(t, map[string]string{
		"clarify.txt":   `[{"id":"q1","question":"Which store?"}]`,
		"clarify.2.txt": good, "clarify.more.txt": "[]",
	}))
	g.mustPlan(planner.Options{Unattended: true, StopAfter: "clarify"})
	rows, err := g.led.List(context.Background(), greeterID, ledger.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	var outcomes []ledger.Outcome
	for _, r := range rows {
		if r.Stage == "clarify" {
			outcomes = append(outcomes, r.Outcome)
		}
	}
	if len(outcomes) < 2 || outcomes[0] != ledger.OutcomeMalformed || outcomes[1] != ledger.OutcomeOK {
		t.Fatalf("clarify outcomes = %v", outcomes)
	}
	st := readStore(t, g.runDir)
	if len(st.Questions) != 1 || st.Questions[0].Answer != "memory" {
		t.Errorf("store = %+v", st)
	}
	if len(gate.rounds) != 0 {
		t.Error("gate asked")
	}
}

func TestUnattendedClarifyFailsWithoutRecommendations(t *testing.T) {
	bad := `[{"id":"q1","question":"Which store?"}]`
	g := newRig(t, approving(), variant(t, map[string]string{"clarify.txt": bad, "clarify.2.txt": bad}))
	_, err := g.plan(planner.Options{Unattended: true, StopAfter: "clarify"})
	if err == nil || !strings.Contains(err.Error(), "needs a recommended answer") {
		t.Fatalf("err = %v", err)
	}
	if g.has("answers.json") {
		t.Error("answers.json written")
	}
}

func TestUnattendedClarifySavedQuestionWithoutRecommendation(t *testing.T) {
	gate := approving()
	g := newRig(t, gate, chainDir(t))
	g.mustPlan(planner.Options{StopAfter: "load"})
	store := `{"calls":1,"round":0,"complete":false,"questions":[{"id":"q1","question":"Which store?","kind":"decision","raised_by":"clarify","round":0,"status":"open"}]}`
	if err := os.MkdirAll(filepath.Join(g.runDir, "_state", "clarify"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.runDir, "_state", "clarify", "questions.json"), []byte(store), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := g.plan(planner.Options{RunID: greeterID, Unattended: true, StopAfter: "clarify"})
	if err == nil || !strings.Contains(err.Error(), "no recommended answer") {
		t.Fatalf("err = %v", err)
	}
	if st := readStore(t, g.runDir); st.Questions[0].Status != "open" || len(gate.rounds) != 0 {
		t.Errorf("a question was answered: %+v", st)
	}
}

func TestUnattendedMidStageQuestionAssumes(t *testing.T) {
	greeting, _ := os.ReadFile(filepath.Join(greeter, "decompose.greeting.txt"))
	gate := approving()
	g := newRig(t, gate, variant(t, map[string]string{
		"decompose.greeting.txt":   question,
		"decompose.greeting.2.txt": string(greeting),
	}))
	g.mustPlan(planner.Options{Unattended: true, StopAfter: "decompose"})
	if len(gate.asked) != 0 {
		t.Errorf("the gate was asked %d questions", len(gate.asked))
	}
	var second string
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) == "decompose:greeting" {
			second = r.Messages[1].Content
		}
	}
	if !strings.Contains(second, "No human is available. Choose the most conservative option") {
		t.Error("the rerun prompt does not tell the model to assume")
	}
	found := false
	for _, q := range readStore(t, g.runDir).Questions {
		if strings.HasPrefix(q.ID, "decompose-") {
			found = true
			if q.Status != "settled" || q.AnsweredBy != "unattended-default" {
				t.Errorf("mid-stage question = %+v", q)
			}
		}
	}
	if !found {
		t.Error("no decompose question in the store")
	}
	if g.has("_state/question.json") {
		t.Error("pending question left behind")
	}
}

func TestUnattendedConfirmRecordsUnattendedAndBindsApproval(t *testing.T) {
	gate := approving()
	g := newRig(t, gate, chainDir(t))
	g.mustPlan(planner.Options{Unattended: true, StopAfter: "approve"})
	if len(gate.understandings) != 0 {
		t.Fatalf("Confirm was called %d times", len(gate.understandings))
	}
	if len(gate.plans) != 1 {
		t.Fatalf("Approve was called %d times, want 1", len(gate.plans))
	}
	by, hash, ok := readUnderstandingRec(t, g.runDir)
	if !ok || by != "unattended" || hash == "" {
		t.Fatalf("understanding: by=%q hash=%q ok=%v", by, hash, ok)
	}
	if !g.has("UNDERSTANDING.md") {
		t.Error("no UNDERSTANDING.md")
	}
	u, ok, err := planner.ReadUnderstanding(g.runDir)
	if err != nil || !ok || u.ConfirmedBy != "unattended" || u.Hash != hash {
		t.Fatalf("ReadUnderstanding = %+v, %v, %v", u, ok, err)
	}
	ap, ok, err := planner.ReadApproval(g.runDir)
	if err != nil || !ok {
		t.Fatalf("ReadApproval = %+v, %v, %v", ap, ok, err)
	}
	if ap.UnderstandingHash != u.Hash || ap.ApprovedBy != "test" || ap.PlanHash == "" {
		t.Errorf("approval = %+v", ap)
	}
	if err := planner.VerifyApproval(g.runDir); err != nil {
		t.Fatalf("VerifyApproval: %v", err)
	}
	// The approval still binds: an edit to answers.json or enriched.json breaks it.
	for _, name := range []string{"answers.json", "_state/enriched.json"} {
		path := filepath.Join(g.runDir, filepath.FromSlash(name))
		orig, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		edited := strings.Replace(string(orig), `"`, `"x`, 1)
		if name == "answers.json" {
			edited = strings.Replace(string(orig), "memory", "memoryX", 1)
		}
		if edited == string(orig) {
			t.Fatalf("%s: edit did not change the file", name)
		}
		if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := planner.VerifyApproval(g.runDir); err == nil {
			t.Errorf("editing %s did not invalidate the approval", name)
		}
		if err := os.WriteFile(path, orig, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUnattendedConfirmRefusesOpenQuestions(t *testing.T) {
	g := newRig(t, approving(), chainDir(t))
	g.mustPlan(planner.Options{Unattended: true, StopAfter: "clarify"})
	path := filepath.Join(g.runDir, "_state", "clarify", "questions.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(raw), `"status": "settled"`, `"status": "open"`, 1)
	if edited == string(raw) {
		edited = strings.Replace(string(raw), `"status":"settled"`, `"status":"open"`, 1)
	}
	if edited == string(raw) {
		t.Fatal("could not reopen a question")
	}
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	// answers.json is derived from the store; keep it in step with the edit
	// (the reopened question is the first one, so it has no answer).
	var af struct {
		Answers []map[string]any `json:"answers"`
	}
	if err := json.Unmarshal(g.read("answers.json"), &af); err != nil {
		t.Fatal(err)
	}
	af.Answers = af.Answers[1:]
	out, _ := json.Marshal(af)
	if err := os.WriteFile(filepath.Join(g.runDir, "answers.json"), out, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = g.plan(planner.Options{RunID: greeterID, Unattended: true})
	if err == nil || !strings.Contains(err.Error(), "open questions") {
		t.Fatalf("err = %v", err)
	}
	if g.has("_state/understanding.json") {
		t.Error("understanding.json written")
	}
}

func TestAttendedClarifyUnchanged(t *testing.T) {
	dir := t.TempDir()
	g := newRig(t, human.NewFile(dir), chainDir(t))
	out, err := g.plan(planner.Options{StopAfter: "clarify"})
	if err != nil || out != planner.Waiting {
		t.Fatalf("plan = %v, %v; want waiting", out, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "QUESTIONS.md")); err != nil {
		t.Errorf("no QUESTIONS.md in the gate folder: %v", err)
	}
}

func TestAttendedConfirmUsesGate(t *testing.T) {
	gate := approving()
	g := newRig(t, gate, chainDir(t))
	g.mustPlan(planner.Options{StopAfter: "confirm"})
	if len(gate.understandings) != 1 {
		t.Fatalf("Confirm called %d times", len(gate.understandings))
	}
	if by, _, ok := readUnderstandingRec(t, g.runDir); !ok || by != "test" {
		t.Errorf("confirmed_by = %q", by)
	}
}
