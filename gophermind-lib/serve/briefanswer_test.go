//go:build !windows

package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const gateFixture = "# Questions\n\nRound 1.\n\nFill in each answer block.\n" +
	"\n## Question 1 (id: clarify-q1)\n\nWhich database should hold the data?\n\nWhy it matters: it sets the driver.\n\nOptions: sqlite | postgres\n\nRecommended: sqlite\nWhy: no server to run.\n\n```answer clarify-q1\n\n```\n" +
	"\n## Question 2 (id: clarify-q2)\n\nShould the CLI print JSON?\n\n```answer clarify-q2\n\n```\n"

// askScript is a launcher whose plan step writes QUESTIONS.md and exits 3
// (waiting), whose resume step logs the file it was given, and whose run step
// succeeds.
func (e *briefEnv) askScript(questions string) {
	e.script = func(args []string) string {
		id := ""
		for _, a := range args {
			if strings.HasPrefix(a, "gm-") {
				id = a
			}
		}
		if len(args) > 1 && strings.HasSuffix(args[2], "brief.md") {
			id = filepath.Base(filepath.Dir(args[2]))
		}
		dir := filepath.Join(e.root, ".gophermind", id)
		switch args[1] {
		case "plan":
			return "mkdir -p '" + dir + "' && cat > '" + dir + "/QUESTIONS.md' <<'EOQ'\n" + questions + "EOQ\nexit 3"
		case "resume":
			return "echo resumed-with; cat '" + dir + "/QUESTIONS.md'"
		}
		return "echo ran"
	}
}

func answerBody(qid, ans string) string {
	b, _ := json.Marshal(map[string]string{"question_id": qid, "answer": ans})
	return string(b)
}

func (e *briefEnv) startAsking(id string) {
	e.t.Helper()
	e.askScript(gateFixture)
	if w := e.post("/app/briefs?ask=1", e.briefText(id), nil); w.Code != http.StatusAccepted {
		e.t.Fatalf("create = %d %s", w.Code, w.Body)
	}
	if rec := e.waitPhase(id, phaseWaiting, phaseFailed, phaseDone); rec.Phase != phaseWaiting {
		e.t.Fatalf("phase %s, want waiting", rec.Phase)
	}
}

func TestBriefAskPlansWithFileGateAndWaits(t *testing.T) {
	e := newBriefEnv(t)
	const id = "gm-2026-10-24-001"
	e.startAsking(id)
	e.mu.Lock()
	first := strings.Join(e.calls[0], " ")
	n := len(e.calls)
	e.mu.Unlock()
	if !strings.HasSuffix(first, "brief.md --yes --gate file") || n != 1 {
		t.Errorf("first call %q, %d calls (run must not start while waiting)", first, n)
	}
	var st BriefState
	if err := json.Unmarshal(e.get("/app/briefs/"+id+"/state").Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Run.Phase != phaseWaiting || st.Run.Active /* a waiting run frees the slot */ || len(st.Questions) != 2 {
		t.Fatalf("state %+v questions %+v", st.Run, st.Questions)
	}
	q := st.Questions[0]
	if q.ID != "clarify-q1" || q.Status != "open" || !q.Askable || q.Question != "Which database should hold the data?" {
		t.Errorf("question %+v", q)
	}
}

func TestBriefAnswerResumesPlanning(t *testing.T) {
	e := newBriefEnv(t)
	const id = "gm-2026-10-24-002"
	e.startAsking(id)
	w := e.post("/app/briefs/"+id+"/answer", answerBody("clarify-q1", "sqlite"), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("first answer = %d %s", w.Code, w.Body)
	}
	if m := decodeMap(t, w); m["resumed"] != false || m["remaining"] != float64(1) {
		t.Errorf("first answer body %v", m)
	}
	w = e.post("/app/briefs/"+id+"/answer", answerBody("clarify-q2", "yes\nwith indent"), nil)
	if w.Code != http.StatusAccepted {
		t.Fatalf("last answer = %d %s", w.Code, w.Body)
	}
	rec := e.waitPhase(id, phaseDone, phaseFailed)
	if rec.Phase != phaseDone {
		t.Fatalf("record %+v", rec)
	}
	e.mu.Lock()
	var got []string
	for _, c := range e.calls {
		got = append(got, strings.Join(c[:2], " "))
	}
	resume := strings.Join(e.calls[1], " ")
	e.mu.Unlock()
	if strings.Join(got, "|") != "brief plan|brief resume|brief run" {
		t.Errorf("calls %v", got)
	}
	if resume != "brief resume "+id+" --yes --gate file" {
		t.Errorf("resume call %q", resume)
	}
	logb, _ := os.ReadFile(filepath.Join(e.dir, id, "run.log"))
	if !strings.Contains(string(logb), "```answer clarify-q1\nsqlite\n```") ||
		!strings.Contains(string(logb), "```answer clarify-q2\nyes\nwith indent\n```") {
		t.Errorf("the resume step did not see the answers:\n%s", logb)
	}
}

func TestBriefAnswerRefusals(t *testing.T) {
	e := newBriefEnv(t)
	const id = "gm-2026-10-24-003"
	e.startAsking(id)
	path := "/app/briefs/" + id + "/answer"
	cases := []struct {
		name, path, body string
		want             int
	}{
		{"unknown question", path, answerBody("clarify-q9", "x"), http.StatusNotFound},
		{"bad id charset", path, answerBody("../x", "x"), http.StatusBadRequest},
		{"id with space", path, answerBody("a b", "x"), http.StatusBadRequest},
		{"empty id", path, answerBody("", "x"), http.StatusBadRequest},
		{"empty answer", path, answerBody("clarify-q1", "  "), http.StatusBadRequest},
		{"fence in answer", path, answerBody("clarify-q1", "a\n```\nb"), http.StatusBadRequest},
		{"too long", path, answerBody("clarify-q1", strings.Repeat("x", maxAnswerBytes+1)), http.StatusRequestEntityTooLarge},
		{"not json", path, "answer=1", http.StatusBadRequest},
		{"bad run id", "/app/briefs/x/answer", answerBody("clarify-q1", "x"), http.StatusNotFound},
		{"unknown run", "/app/briefs/gm-2026-10-24-777/answer", answerBody("clarify-q1", "x"), http.StatusNotFound},
	}
	for _, c := range cases {
		if w := e.post(c.path, c.body, nil); w.Code != c.want {
			t.Errorf("%s = %d %s, want %d", c.name, w.Code, w.Body, c.want)
		}
	}
	b, _ := os.ReadFile(filepath.Join(e.root, ".gophermind", id, "QUESTIONS.md"))
	if string(b) != gateFixture {
		t.Errorf("a refused answer changed QUESTIONS.md:\n%s", b)
	}
	if w := e.post(path, answerBody("clarify-q1", "x"), map[string]string{"Sec-Fetch-Site": "cross-site"}); w.Code != http.StatusForbidden {
		t.Errorf("cross-site = %d", w.Code)
	}
}

func TestBriefAnswerWhenNotPausedIs409(t *testing.T) {
	e := newBriefEnv(t)
	const id = "gm-2026-10-24-004"
	if w := e.post("/app/briefs", e.briefText(id), nil); w.Code != http.StatusAccepted {
		t.Fatalf("create = %d", w.Code)
	}
	e.waitPhase(id, phaseDone)
	w := e.post("/app/briefs/"+id+"/answer", answerBody("clarify-q1", "x"), nil)
	if w.Code != http.StatusConflict || decodeMap(t, w)["phase"] != phaseDone {
		t.Errorf("answer to a finished run = %d %s", w.Code, w.Body)
	}
}

func TestBriefAnswerNeedsSignIn(t *testing.T) {
	e := newBriefEnv(t)
	if w := doBody(e.h, "POST", "/app/briefs/gm-2026-10-24-005/answer", answerBody("q", "x"), nil); w.Code != http.StatusUnauthorized {
		t.Errorf("signed out = %d", w.Code)
	}
}

func TestParseGateQuestions(t *testing.T) {
	_, qs := parseGateQuestions(gateFixture)
	if len(qs) != 2 || qs[0].ID != "clarify-q1" || qs[0].Text != "Which database should hold the data?" || qs[0].Filled ||
		qs[1].ID != "clarify-q2" || qs[1].Text != "Should the CLI print JSON?" {
		t.Fatalf("%+v", qs)
	}
	filled := strings.Replace(gateFixture, "```answer clarify-q2\n\n```", "```answer clarify-q2\nyes\n```", 1)
	if _, qs := parseGateQuestions(filled); !qs[1].Filled || qs[0].Filled {
		t.Errorf("filled flags %+v", qs)
	}
}

func TestMarkAskableMergesStoreAndGate(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".gophermind", "gm-2026-10-24-009")
	if err := os.MkdirAll(filepath.Join(dir, "_state", "clarify"), 0o755); err != nil {
		t.Fatal(err)
	}
	store := `{"questions":[{"id":"clarify-q1","question":"Which database should hold the data?","kind":"decision","round":1,"status":"open","recommended":"sqlite","recommended_why":"no server","options":["sqlite","postgres"]},{"id":"clarify-q5","question":"Later one","kind":"decision","round":2,"status":"open"}]}`
	os.WriteFile(filepath.Join(dir, "_state", "clarify", "questions.json"), []byte(store), 0o644)
	os.WriteFile(filepath.Join(dir, "QUESTIONS.md"), []byte(gateFixture), 0o600)
	rec := &briefRecord{ID: "gm-2026-10-24-009", Phase: phaseWaiting}
	st, err := BuildBriefState(root, rec.ID, rec, false, "", stateNow)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]BriefQuestion{}
	for _, q := range st.Questions {
		by[q.ID] = q
	}
	if len(by) != 3 || !by["clarify-q1"].Askable || by["clarify-q1"].Recommended != "sqlite" ||
		len(by["clarify-q1"].Options) != 2 || by["clarify-q5"].Askable || !by["clarify-q2"].Askable {
		t.Errorf("questions %+v", st.Questions)
	}
}

// A run folder the server user cannot write is answered through the launcher:
// `brief fill-answer` runs as the run user with the text on stdin.
func TestBriefAnswerFallsBackToTheLauncherWhenTheFolderIsReadOnly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory modes")
	}
	e := newBriefEnv(t)
	const id = "gm-2026-10-24-003"
	e.startAsking(id)
	dir := filepath.Join(e.root, ".gophermind", id)
	inner := e.script
	e.script = func(args []string) string {
		if len(args) > 1 && args[1] == "fill-answer" {
			return "cat > '" + filepath.Join(e.root, "got.txt") + "'; echo 'remaining 1'"
		}
		return inner(args)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	w := e.post("/app/briefs/"+id+"/answer", answerBody("clarify-q1", "sqlite"), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("answer = %d %s", w.Code, w.Body)
	}
	if m := decodeMap(t, w); m["saved"] != true || m["remaining"] != float64(1) {
		t.Errorf("body %v", m)
	}
	e.mu.Lock()
	last := strings.Join(e.calls[len(e.calls)-1], " ")
	e.mu.Unlock()
	if want := "brief fill-answer " + id + " clarify-q1 --repo " + e.root; last != want {
		t.Errorf("launcher call %q, want %q", last, want)
	}
	if got, _ := os.ReadFile(filepath.Join(e.root, "got.txt")); string(got) != "sqlite" {
		t.Errorf("stdin text %q", got)
	}
}

func TestMarkAskableShowsAFilledBlockAsSettled(t *testing.T) {
	_, gate := parseGateQuestions(strings.Replace(gateFixture, "```answer clarify-q1\n\n```", "```answer clarify-q1\nsqlite\n```", 1))
	qs := markAskable([]BriefQuestion{{ID: "clarify-q1", Status: "open"}}, gate)
	if len(qs) != 2 {
		t.Fatalf("questions %+v", qs)
	}
	if q := qs[0]; q.Status != "settled" || q.Answer != "sqlite" || q.AnsweredBy != "human" || q.Askable {
		t.Errorf("answered question %+v", q)
	}
	if q := qs[1]; q.Status != "open" || !q.Askable {
		t.Errorf("open question %+v", q)
	}
}
