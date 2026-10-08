package human_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/human"
)

// adapter runs one gate call the way a person would answer it. reply is the
// person's answer in words every adapter understands: "approve", "reject: why",
// "retry use the stdlib". typed holds one entry per question; "" means the
// person left the question alone (accepting its default).
type adapter struct {
	name     string
	ask      func(t *testing.T, qs []human.Question, typed []string) ([]human.Answer, error)
	approve  func(t *testing.T, plan human.PlanSummary, reply string) (human.Decision, error)
	escalate func(t *testing.T, e human.Escalation, reply string) (human.Resolution, error)
}

func terminalAdapter() adapter {
	run := func(input string) (*human.Terminal, *bytes.Buffer) {
		out := &bytes.Buffer{}
		return human.NewTerminal(strings.NewReader(input), out), out
	}
	return adapter{
		name: "terminal",
		ask: func(t *testing.T, qs []human.Question, typed []string) ([]human.Answer, error) {
			g, _ := run(strings.Join(typed, "\n") + "\n")
			return g.Ask(context.Background(), qs)
		},
		approve: func(t *testing.T, plan human.PlanSummary, reply string) (human.Decision, error) {
			g, _ := run(reply + "\n")
			return g.Approve(context.Background(), plan)
		},
		escalate: func(t *testing.T, e human.Escalation, reply string) (human.Resolution, error) {
			g, _ := run(reply + "\n")
			return g.Escalate(context.Background(), e)
		},
	}
}

// fill replaces the text of the block of the given kind (and id, when non-empty) in path.
func fill(t *testing.T, path, kind, id, text string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	head := "```" + kind
	if id != "" {
		head += " " + id
	}
	re := regexp.MustCompile("(?ms)^" + regexp.QuoteMeta(head) + "[ \\t]*\\n.*?^```[ \\t]*$")
	if !re.Match(data) {
		t.Fatalf("no %q block in %s:\n%s", head, path, data)
	}
	out := re.ReplaceAllLiteral(data, []byte(head+"\n"+text+"\n```"))
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

func fileAdapter() adapter {
	return adapter{
		name: "file",
		ask: func(t *testing.T, qs []human.Question, typed []string) ([]human.Answer, error) {
			dir := t.TempDir()
			g := human.NewFile(dir)
			if _, err := g.Ask(context.Background(), qs); !errors.Is(err, human.ErrWaiting) {
				t.Fatalf("first call = %v, want ErrWaiting", err)
			}
			for i, q := range qs {
				if typed[i] != "" {
					fill(t, filepath.Join(dir, "QUESTIONS.md"), "answer", q.ID, typed[i])
				}
			}
			return g.Ask(context.Background(), qs)
		},
		approve: func(t *testing.T, plan human.PlanSummary, reply string) (human.Decision, error) {
			dir := t.TempDir()
			g := human.NewFile(dir)
			if _, err := g.Approve(context.Background(), plan); !errors.Is(err, human.ErrWaiting) {
				t.Fatalf("first call = %v, want ErrWaiting", err)
			}
			fill(t, filepath.Join(dir, "APPROVAL.md"), "decision", "", reply)
			return g.Approve(context.Background(), plan)
		},
		escalate: func(t *testing.T, e human.Escalation, reply string) (human.Resolution, error) {
			dir := t.TempDir()
			g := human.NewFile(dir)
			if _, err := g.Escalate(context.Background(), e); !errors.Is(err, human.ErrWaiting) {
				t.Fatalf("first call = %v, want ErrWaiting", err)
			}
			fill(t, filepath.Join(dir, "ESCALATION-"+e.NodeID+".md"), "resolution", "", reply)
			return g.Escalate(context.Background(), e)
		},
	}
}

func programmaticAdapter() adapter {
	return adapter{
		name: "programmatic",
		ask: func(t *testing.T, qs []human.Question, typed []string) ([]human.Answer, error) {
			g := human.NewProgrammatic()
			type out struct {
				a   []human.Answer
				err error
			}
			done := make(chan out, 1)
			go func() { a, err := g.Ask(context.Background(), qs); done <- out{a, err} }()
			req := <-g.Requests()
			if req.Kind != human.KindAsk || len(req.Questions) != len(qs) {
				t.Errorf("request = %+v", req)
			}
			as := make([]human.Answer, len(qs))
			for i, q := range qs {
				as[i] = human.Answer{ID: q.ID, Text: typed[i]}
				if typed[i] == "" {
					as[i] = human.Answer{ID: q.ID, Text: q.Default, Assumed: true}
				}
			}
			req.Answer(as)
			r := <-done
			return r.a, r.err
		},
		approve: func(t *testing.T, plan human.PlanSummary, reply string) (human.Decision, error) {
			g := human.NewProgrammatic()
			type out struct {
				d   human.Decision
				err error
			}
			done := make(chan out, 1)
			go func() { d, err := g.Approve(context.Background(), plan); done <- out{d, err} }()
			req := <-g.Requests()
			if req.Kind != human.KindApprove || req.Plan != plan {
				t.Errorf("request = %+v", req)
			}
			word, note, _ := strings.Cut(reply, ":")
			req.Decide(human.Decision{Approved: word == "approve", Note: strings.TrimSpace(note)})
			r := <-done
			return r.d, r.err
		},
		escalate: func(t *testing.T, e human.Escalation, reply string) (human.Resolution, error) {
			g := human.NewProgrammatic()
			type out struct {
				r   human.Resolution
				err error
			}
			done := make(chan out, 1)
			go func() { r, err := g.Escalate(context.Background(), e); done <- out{r, err} }()
			req := <-g.Requests()
			word, note, _ := strings.Cut(reply, " ")
			req.Resolve(human.Resolution{Action: human.Action(word), Note: note})
			r := <-done
			return r.r, r.err
		},
	}
}

func adapters() []adapter { return []adapter{terminalAdapter(), fileAdapter(), programmaticAdapter()} }

var twoQuestions = []human.Question{
	{ID: "db", Text: "Which database?"},
	{ID: "lang", Text: "Which language?", Default: "Go", Options: []string{"Go", "Python"}},
}

func TestEveryAdapterAsksQuestionsTheSameWay(t *testing.T) {
	cases := []struct {
		name  string
		typed []string
		want  []human.Answer
	}{
		{"typed answer and an accepted default", []string{"postgres", ""},
			[]human.Answer{{ID: "db", Text: "postgres"}, {ID: "lang", Text: "Go", Assumed: true}}},
		{"both typed", []string{"postgres", "Python"},
			[]human.Answer{{ID: "db", Text: "postgres"}, {ID: "lang", Text: "Python"}}},
	}
	for _, a := range adapters() {
		for _, c := range cases {
			t.Run(a.name+"/"+c.name, func(t *testing.T) {
				got, err := a.ask(t, twoQuestions, c.typed)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, c.want) {
					t.Errorf("answers = %+v, want %+v", got, c.want)
				}
			})
		}
	}
}

func TestEveryAdapterHandlesApproval(t *testing.T) {
	plan := human.PlanSummary{Markdown: "# Plan\n3 components, 12 functions", Hash: "h123"}
	cases := []struct {
		name  string
		reply string
		want  human.Decision
	}{
		{"approve", "approve", human.Decision{Approved: true}},
		{"reject with a reason", "reject: too many components", human.Decision{Approved: false, Note: "too many components"}},
	}
	for _, a := range adapters() {
		for _, c := range cases {
			t.Run(a.name+"/"+c.name, func(t *testing.T) {
				got, err := a.approve(t, plan, c.reply)
				if err != nil {
					t.Fatal(err)
				}
				if got.By != a.name {
					t.Errorf("By = %q, want %q", got.By, a.name)
				}
				got.By = ""
				if got != c.want {
					t.Errorf("decision = %+v, want %+v", got, c.want)
				}
			})
		}
	}
}

// answeredBy is the source each gate kind records on a resolution.
var answeredBy = map[string]string{"terminal": human.AnsweredByHuman, "file": human.AnsweredByHuman, "programmatic": human.AnsweredByProgrammatic}

func TestEveryAdapterRecordsWhoAnswered(t *testing.T) {
	for _, a := range adapters() {
		t.Run(a.name, func(t *testing.T) {
			got, err := a.escalate(t, human.Escalation{NodeID: "fn-a"}, "skip")
			if err != nil || got.AnsweredBy != answeredBy[a.name] || got.AnsweredBy == "" {
				t.Fatalf("resolution = %+v, %v; want AnsweredBy %q", got, err, answeredBy[a.name])
			}
		})
	}
}

// An auto-answering gate built on the programmatic one names itself; a value
// outside the fixed vocabulary is not believed.
func TestProgrammaticKeepsAKnownSourceAndRefusesAnUnknownOne(t *testing.T) {
	for _, tc := range []struct{ set, want string }{
		{human.AnsweredByUnattended, human.AnsweredByUnattended},
		{"", human.AnsweredByProgrammatic},
		{"a person, honest", human.AnsweredByProgrammatic},
	} {
		g := human.NewProgrammatic()
		done := make(chan human.Resolution, 1)
		go func() { r, _ := g.Escalate(context.Background(), human.Escalation{NodeID: "n"}); done <- r }()
		(<-g.Requests()).Resolve(human.Resolution{Action: human.ActionStop, AnsweredBy: tc.set})
		if r := <-done; r.AnsweredBy != tc.want {
			t.Errorf("set %q: AnsweredBy = %q, want %q", tc.set, r.AnsweredBy, tc.want)
		}
	}
}

func TestEveryAdapterHandlesEscalation(t *testing.T) {
	e := human.Escalation{NodeID: "fn-a", Reason: "all models failed", History: []string{"mini: tests failed", "kilo: timeout"}}
	cases := []struct {
		name  string
		reply string
		want  human.Resolution
	}{
		{"retry with a note", "retry use the standard library", human.Resolution{Action: human.ActionRetry, Note: "use the standard library"}},
		{"stop", "stop", human.Resolution{Action: human.ActionStop}},
	}
	for _, a := range adapters() {
		for _, c := range cases {
			t.Run(a.name+"/"+c.name, func(t *testing.T) {
				got, err := a.escalate(t, e, c.reply)
				want := c.want
				want.AnsweredBy = answeredBy[a.name]
				if err != nil || got != want {
					t.Errorf("resolution = %+v, %v; want %+v", got, err, want)
				}
			})
		}
	}
}

func TestTerminalPromptsOnTheOutputWriter(t *testing.T) {
	out := &bytes.Buffer{}
	g := human.NewTerminal(strings.NewReader("postgres\n\n"), out)
	if _, err := g.Ask(context.Background(), twoQuestions); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Question 1 of 2", "Which database?", "Question 2 of 2", "1) Go", "2) Python", "press Enter for: Go"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("prompt output lacks %q:\n%s", want, out)
		}
	}
}

func TestTerminalNumberPicksAnOption(t *testing.T) {
	g := human.NewTerminal(strings.NewReader("mysql\n2\n"), &bytes.Buffer{})
	got, err := g.Ask(context.Background(), twoQuestions)
	if err != nil || got[1].Text != "Python" || got[1].Assumed {
		t.Errorf("%+v %v", got, err)
	}
}

func TestTerminalRequiresAnAnswerWhenThereIsNoDefault(t *testing.T) {
	out := &bytes.Buffer{}
	g := human.NewTerminal(strings.NewReader("\n\npostgres\n\n"), out)
	got, err := g.Ask(context.Background(), twoQuestions)
	if err != nil || got[0].Text != "postgres" {
		t.Fatalf("%+v %v", got, err)
	}
	if strings.Count(out.String(), "An answer is required.") != 2 {
		t.Errorf("expected two reminders:\n%s", out)
	}
}

func TestTerminalEndOfInput(t *testing.T) {
	// Input ends: a question with a default takes it; one without is an error.
	g := human.NewTerminal(strings.NewReader("postgres\n"), &bytes.Buffer{})
	got, err := g.Ask(context.Background(), twoQuestions)
	if err != nil || got[1].Text != "Go" || !got[1].Assumed {
		t.Errorf("default at end of input: %+v %v", got, err)
	}
	g = human.NewTerminal(strings.NewReader(""), &bytes.Buffer{})
	if _, err := g.Ask(context.Background(), twoQuestions); err == nil {
		t.Error("no input and no default should be an error")
	}
	g = human.NewTerminal(strings.NewReader("postgres"), &bytes.Buffer{}) // no trailing newline
	if got, err := g.Ask(context.Background(), twoQuestions[:1]); err != nil || got[0].Text != "postgres" {
		t.Errorf("last line without newline: %+v %v", got, err)
	}
}

func TestTerminalApprovalAndEscalationReprompt(t *testing.T) {
	plan := human.PlanSummary{Markdown: "plan", Hash: "h"}
	g := human.NewTerminal(strings.NewReader("maybe\ny\n"), &bytes.Buffer{})
	if d, err := g.Approve(context.Background(), plan); err != nil || !d.Approved {
		t.Errorf("garbage then y: %+v %v", d, err)
	}
	g = human.NewTerminal(strings.NewReader("\n"), &bytes.Buffer{})
	if d, err := g.Approve(context.Background(), plan); err != nil || d.Approved {
		t.Errorf("an empty line must not approve: %+v %v", d, err)
	}
	g = human.NewTerminal(strings.NewReader(""), &bytes.Buffer{})
	if _, err := g.Approve(context.Background(), plan); err == nil {
		t.Error("end of input must not approve")
	}
	g = human.NewTerminal(strings.NewReader("later\nskip\n"), &bytes.Buffer{})
	if r, err := g.Escalate(context.Background(), human.Escalation{NodeID: "fn-a"}); err != nil || r.Action != human.ActionSkip {
		t.Errorf("garbage then skip: %+v %v", r, err)
	}
}

func TestTerminalStopsWhenTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g := human.NewTerminal(strings.NewReader("x\n"), &bytes.Buffer{})
	if _, err := g.Ask(ctx, twoQuestions); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
}

func TestFileAskWritesTheQuestionsFileAndWaits(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "run")
	g := human.NewFile(dir)
	_, err := g.Ask(context.Background(), twoQuestions)
	if !errors.Is(err, human.ErrWaiting) {
		t.Fatalf("err = %v", err)
	}
	path := filepath.Join(dir, "QUESTIONS.md")
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("file: %v %v", fi, err)
	}
	b, _ := os.ReadFile(path)
	for _, want := range []string{"Which database?", "Which language?", "Options: Go | Python", "```answer db", "```answer lang\nGo\n```"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("QUESTIONS.md lacks %q:\n%s", want, b)
		}
	}
}

func TestFileAskKeepsWaitingAndNeverOverwritesAPartialAnswer(t *testing.T) {
	dir := t.TempDir()
	g := human.NewFile(dir)
	g.Ask(context.Background(), twoQuestions)
	path := filepath.Join(dir, "QUESTIONS.md")
	fill(t, path, "answer", "lang", "Python") // db still empty and it has no default
	before, _ := os.ReadFile(path)
	if _, err := g.Ask(context.Background(), twoQuestions); !errors.Is(err, human.ErrWaiting) {
		t.Fatalf("err = %v, want ErrWaiting", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("a waiting call rewrote the file and lost the person's edits")
	}
}

func TestFileAnswersAreArchivedSoTheNextAskStartsFresh(t *testing.T) {
	dir := t.TempDir()
	g := human.NewFile(dir)
	g.Ask(context.Background(), twoQuestions)
	fill(t, filepath.Join(dir, "QUESTIONS.md"), "answer", "db", "postgres")
	if _, err := g.Ask(context.Background(), twoQuestions); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "QUESTIONS.md")); !errors.Is(err, os.ErrNotExist) {
		t.Error("QUESTIONS.md should have been moved aside once answered")
	}
	if _, err := os.Stat(filepath.Join(dir, "QUESTIONS.answered.md")); err != nil {
		t.Error("the answered file should be kept as a record")
	}
	next := []human.Question{{ID: "other", Text: "A later question?"}}
	if _, err := g.Ask(context.Background(), next); !errors.Is(err, human.ErrWaiting) {
		t.Errorf("a new question set should wait, got %v", err)
	}
}

func TestFileAskRefusesAFileForDifferentQuestions(t *testing.T) {
	dir := t.TempDir()
	g := human.NewFile(dir)
	g.Ask(context.Background(), twoQuestions)
	_, err := g.Ask(context.Background(), []human.Question{{ID: "other", Text: "?"}})
	if err == nil || errors.Is(err, human.ErrWaiting) {
		t.Errorf("err = %v, want a mismatch error", err)
	}
	_, err = g.Ask(context.Background(), []human.Question{{ID: "lang", Text: "?"}, {ID: "db", Text: "?"}})
	if err == nil || errors.Is(err, human.ErrWaiting) {
		t.Errorf("questions in a different order: err = %v", err)
	}
}

func TestFileApprovalIsBoundToTheExactPlan(t *testing.T) {
	dir := t.TempDir()
	g := human.NewFile(dir)
	ctx := context.Background()
	planA := human.PlanSummary{Markdown: "plan A", Hash: "hashA"}
	planB := human.PlanSummary{Markdown: "plan B", Hash: "hashB"}
	if _, err := g.Approve(ctx, planA); !errors.Is(err, human.ErrWaiting) {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "APPROVAL.md")
	fill(t, path, "decision", "", "approve")
	for i := 0; i < 2; i++ { // asking again for the same plan returns the same decision
		if d, err := g.Approve(ctx, planA); err != nil || !d.Approved {
			t.Fatalf("call %d: %+v %v", i, d, err)
		}
	}
	// The plan changed: the old approval must not carry over.
	if _, err := g.Approve(ctx, planB); !errors.Is(err, human.ErrWaiting) {
		t.Fatalf("a changed plan must wait for a new decision, got %v", err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "Plan hash: hashB") || !strings.Contains(string(b), "plan B") || strings.Contains(string(b), "approve\n```") {
		t.Errorf("APPROVAL.md was not rewritten for the new plan:\n%s", b)
	}
}

func TestFileApprovalRejectsAnUnreadableDecision(t *testing.T) {
	dir := t.TempDir()
	g := human.NewFile(dir)
	plan := human.PlanSummary{Markdown: "plan", Hash: "h"}
	g.Approve(context.Background(), plan)
	fill(t, filepath.Join(dir, "APPROVAL.md"), "decision", "", "looks fine I guess")
	_, err := g.Approve(context.Background(), plan)
	if err == nil || errors.Is(err, human.ErrWaiting) {
		t.Errorf("err = %v, want a format error", err)
	}
}

func TestFileEscalationNamesStayInsideTheFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "run")
	g := human.NewFile(dir)
	if _, err := g.Escalate(context.Background(), human.Escalation{NodeID: "../../evil/fn"}); !errors.Is(err, human.ErrWaiting) {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "ESCALATION-") || strings.Contains(entries[0].Name(), "/") {
		t.Errorf("entries = %v", entries)
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "..", "evil")); err == nil {
		t.Error("a node id escaped the run folder")
	}
}

func TestProgrammaticRejectsAnswersForTheWrongQuestions(t *testing.T) {
	g := human.NewProgrammatic()
	done := make(chan error, 1)
	go func() { _, err := g.Ask(context.Background(), twoQuestions); done <- err }()
	req := <-g.Requests()
	req.Answer([]human.Answer{{ID: "db", Text: "x"}}) // one answer for two questions
	if err := <-done; err == nil {
		t.Error("a short answer list was accepted")
	}
	go func() { _, err := g.Ask(context.Background(), twoQuestions); done <- err }()
	req = <-g.Requests()
	req.Answer([]human.Answer{{ID: "db", Text: "x"}, {ID: "wrong", Text: "y"}})
	if err := <-done; err == nil {
		t.Error("an answer for the wrong question id was accepted")
	}
	go func() { _, err := g.Ask(context.Background(), twoQuestions[:1]); done <- err }()
	req = <-g.Requests()
	req.Answer([]human.Answer{{ID: "db", Text: "  "}})
	if err := <-done; err == nil {
		t.Error("a blank answer was accepted")
	}
}

func TestProgrammaticCancellationFailureAndDoubleReply(t *testing.T) {
	g := human.NewProgrammatic()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := g.Approve(ctx, human.PlanSummary{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("unanswered request: err = %v", err)
	}
	<-g.Requests() // drain the abandoned request

	boom := errors.New("the app closed")
	done := make(chan error, 1)
	go func() { _, err := g.Escalate(context.Background(), human.Escalation{NodeID: "n"}); done <- err }()
	req := <-g.Requests()
	req.Fail(boom)
	req.Fail(errors.New("second reply is ignored")) // must not block or panic
	if err := <-done; !errors.Is(err, boom) {
		t.Errorf("Fail: err = %v", err)
	}

	go func() { _, err := g.Escalate(context.Background(), human.Escalation{NodeID: "n"}); done <- err }()
	req = <-g.Requests()
	req.Resolve(human.Resolution{}) // no action
	if err := <-done; err == nil {
		t.Error("a resolution without an action was accepted")
	}
}

func TestTerminalShowsTheRecommendationAndAcceptsIt(t *testing.T) {
	var out strings.Builder
	term := human.NewTerminal(strings.NewReader("accept\n"), &out)
	got, err := term.Ask(context.Background(), []human.Question{{ID: "q1", Text: "Which store?", Options: []string{"memory", "postgres"}, Recommended: "memory", RecommendedWhy: "no database is declared"}})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Text != "memory" || !got[0].Accepted || got[0].Assumed {
		t.Errorf("answer = %+v", got[0])
	}
	if !strings.Contains(out.String(), "Recommended: memory") || !strings.Contains(out.String(), "no database is declared") {
		t.Errorf("the recommendation was not shown:\n%s", out.String())
	}
}

func TestTerminalRefusesABlankAnswerWhenOnlyARecommendationExists(t *testing.T) {
	var out strings.Builder
	term := human.NewTerminal(strings.NewReader("\nmemory\n"), &out)
	got, err := term.Ask(context.Background(), []human.Question{{ID: "q1", Text: "Which store?", Options: []string{"memory", "postgres"}, Recommended: "postgres"}})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Text != "memory" || got[0].Accepted {
		t.Errorf("a blank line must not take the recommendation: %+v", got[0])
	}
	if !strings.Contains(out.String(), "An answer is required.") {
		t.Errorf("the blank answer was not refused:\n%s", out.String())
	}
}

func TestTerminalAcceptWithoutARecommendationIsFreeText(t *testing.T) {
	term := human.NewTerminal(strings.NewReader("accept\n"), io.Discard)
	got, err := term.Ask(context.Background(), []human.Question{{ID: "q1", Text: "Name?"}})
	if err != nil || got[0].Text != "accept" || got[0].Accepted {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestTerminalConfirm(t *testing.T) {
	for in, want := range map[string]bool{"y\n": true, "yes please\n": true, "n\n": false, "\n": false} {
		term := human.NewTerminal(strings.NewReader(in), io.Discard)
		d, err := term.Confirm(context.Background(), human.Understanding{Markdown: "# U", Hash: "abc"})
		if err != nil || d.Approved != want || d.By != "terminal" {
			t.Errorf("input %q: %+v, %v", in, d, err)
		}
	}
}

func TestFileGateWritesTheRecommendationAndReadsAccept(t *testing.T) {
	dir := t.TempDir()
	g := human.NewFile(dir)
	qs := []human.Question{{ID: "q1", Text: "Which store?", Round: 2, Options: []string{"memory", "postgres"}, Recommended: "memory", RecommendedWhy: "no database is declared"}}
	if _, err := g.Ask(context.Background(), qs); !errors.Is(err, human.ErrWaiting) {
		t.Fatalf("first call = %v, want ErrWaiting", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "QUESTIONS.md"))
	for _, want := range []string{"Round 2", "Recommended: memory", "no database is declared", "```answer q1"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("QUESTIONS.md lacks %q:\n%s", want, raw)
		}
	}
	filled := strings.Replace(string(raw), "```answer q1\n\n```", "```answer q1\naccept\n```", 1)
	if err := os.WriteFile(filepath.Join(dir, "QUESTIONS.md"), []byte(filled), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := g.Ask(context.Background(), qs)
	if err != nil || got[0].Text != "memory" || !got[0].Accepted {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "QUESTIONS.answered.md")); err != nil {
		t.Errorf("the answered file was not archived, so the next round would reuse it: %v", err)
	}
}

func TestFileGateConfirmWaitsThenReadsTheDecisionAndIgnoresAStaleHash(t *testing.T) {
	dir := t.TempDir()
	g := human.NewFile(dir)
	u := human.Understanding{Markdown: "# Understanding\n\nBody", Hash: "h1"}
	if _, err := g.Confirm(context.Background(), u); !errors.Is(err, human.ErrWaiting) {
		t.Fatalf("first call = %v", err)
	}
	p := filepath.Join(dir, "UNDERSTANDING.md")
	raw, _ := os.ReadFile(p)
	if !strings.Contains(string(raw), "Understanding hash: h1") {
		t.Fatalf("the hash line is missing:\n%s", raw)
	}
	if err := os.WriteFile(p, []byte(strings.Replace(string(raw), "```decision\n\n```", "```decision\napprove\n```", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := g.Confirm(context.Background(), u)
	if err != nil || !d.Approved || d.By != "file" {
		t.Fatalf("got %+v, %v", d, err)
	}
	// The understanding changed: an old answer must not confirm a new one.
	if _, err := g.Confirm(context.Background(), human.Understanding{Markdown: "# U2", Hash: "h2"}); !errors.Is(err, human.ErrWaiting) {
		t.Fatalf("a stale confirmation was accepted: %v", err)
	}
}

func TestProgrammaticConfirm(t *testing.T) {
	p := human.NewProgrammatic()
	go func() {
		r := <-p.Requests()
		if r.Kind != human.KindConfirm || r.Understanding.Hash != "h" {
			t.Errorf("request = %+v", r)
		}
		r.Decide(human.Decision{Approved: true})
	}()
	d, err := p.Confirm(context.Background(), human.Understanding{Markdown: "m", Hash: "h"})
	if err != nil || !d.Approved || d.By != "programmatic" {
		t.Fatalf("got %+v, %v", d, err)
	}
}
