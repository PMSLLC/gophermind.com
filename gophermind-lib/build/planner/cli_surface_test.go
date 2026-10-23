package planner_test

import (
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/planner"
)

// surfaceBrief gives the greeter brief the csvstat brief's command-line surface.
func surfaceBrief(s string) string {
	return s + "- `greeter --format json hello.txt` prints the greetings as JSON.\n" +
		"- The accepted values of `--format` are `text` (the default) and `json`; anything else exits with code 2 and a\n  usage message.\n" +
		"- Reading `-` as the file name reads standard input.\n" +
		"- The exit code is 0 on success and 1 on any error.\n"
}

const runWithoutSurface = `{"types": [], "functions": [
  {"id": "fn-main", "package": "cmd/greeter", "file": "cmd/greeter/main.go",
   "signature": "func main()", "doc": "main calls run and exits with its code.", "uses": ["fn-run"]},
  {"id": "fn-run", "package": "cmd/greeter", "file": "cmd/greeter/run.go",
   "signature": "func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int", "doc": "run parses args to determine input source and output format.", "uses": ["fn-greet"]}
], "more": false}`

const runWithSurface = `{"types": [], "functions": [
  {"id": "fn-run", "package": "cmd/greeter", "file": "cmd/greeter/run.go",
   "signature": "func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int",
   "doc": "run parses --format text|json (default text) and one file name; - reads stdin. It exits 0 on success, 1 on any error and exit code 2 on an invalid --format.", "uses": ["fn-greet"]}
], "more": false}`

func surfaceRig(t *testing.T, files map[string]string) *rig {
	t.Helper()
	files["contract.outline.entry.txt"] = `{"components": [` + entryComp + `]}`
	g := newRig(t, approving(), variant(t, files))
	g.briefPath = writeBrief(t, g.repo, surfaceBrief)
	return g
}

func runDoc(g *rig) string {
	for _, f := range g.contracts().Functions {
		if f.ID == "fn-run" {
			return f.Doc
		}
	}
	return ""
}

func TestEntryPointMissingTheBriefsFlagsIsSentToRepair(t *testing.T) {
	g := surfaceRig(t, map[string]string{
		"contract.entry-point.txt":   runWithoutSurface,
		"contract.entry-point.2.txt": runWithSurface,
	})
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if n := count(g.stagesCalled(), "contract:entry-point"); n != 2 {
		t.Fatalf("entry-point component calls = %d, want 2", n)
	}
	if doc := runDoc(g); !strings.Contains(doc, "--format") || !strings.Contains(doc, "stdin") {
		t.Errorf("fn-run doc after repair = %q", doc)
	}
	n := 0
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) != "contract:entry-point" {
			continue
		}
		n++
		p := r.Messages[1].Content
		if !strings.Contains(p, "<cli_surface>") || !strings.Contains(p, "--format") {
			t.Errorf("call %d does not carry the brief's surface", n)
		}
		if has := strings.Contains(p, "does not state these items"); n == 1 && has || n == 2 && !has {
			t.Errorf("call %d: defect present = %v", n, has)
		}
	}
}

func TestEntryPointStatingTheSurfaceIsNotRepaired(t *testing.T) {
	g := surfaceRig(t, map[string]string{"contract.entry-point.txt": strings.Replace(entryFns, `"run wires the feature functions and returns the exit code."`,
		`"run parses --format text|json; - reads stdin; exits 0 on success, 1 on error, exit code 2 on a bad --format."`, 1)})
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if n := count(g.stagesCalled(), "contract:entry-point"); n != 1 {
		t.Fatalf("entry-point component calls = %d, want 1", n)
	}
}

func TestEntryPointStillMissingTheSurfaceIsRepairedOnceAndWarns(t *testing.T) {
	g := surfaceRig(t, map[string]string{"contract.entry-point.txt": runWithoutSurface})
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if n := count(g.stagesCalled(), "contract:entry-point"); n != 2 {
		t.Fatalf("entry-point component calls = %d, want 2 (one repair)", n)
	}
	if doc := runDoc(g); !strings.Contains(doc, "determine input source") {
		t.Errorf("the original fn-run was lost: %q", doc)
	}
	warned := false
	for _, w := range g.sink.OfKind("warning") {
		warned = warned || strings.Contains(w.Message, "cli_surface_missing")
	}
	if !warned {
		t.Error("no cli_surface_missing warning")
	}
}

func TestUnattendedClarifyDefaultCannotContradictTheBriefsSurface(t *testing.T) {
	g := newRig(t, approving(), fixtureDir(t, map[string]string{
		"clarify.txt": `[
 {"id":"q1","question":"Should the tool read standard input?","options":["Yes, with -","No, only -f file"],"recommended":"No, only -f file","recommended_why":"simplest"},
 {"id":"q2","question":"How are greetings ordered?","recommended":"File order"}]`,
		"clarify.more.txt": "[]"}))
	g.briefPath = writeBrief(t, g.repo, func(s string) string {
		return surfaceBrief(strings.Replace(s, "on_ambiguity: halt", "on_ambiguity: assume_and_document", 1))
	})
	if out, err := g.plan(planner.Options{StopAfter: "clarify"}); err != nil || out != planner.Done {
		t.Fatalf("plan = %v, %v", out, err)
	}
	st := readStore(t, g.runDir)
	q1, q2 := st.Questions[0], st.Questions[1]
	if q1.AnsweredBy != "brief" || strings.Contains(q1.Answer, "-f file") ||
		!strings.Contains(q1.Answer, "Reading `-` as the file name reads standard input") {
		t.Errorf("q1 = %+v", q1)
	}
	if q2.AnsweredBy != "unattended-default" || q2.Answer != "File order" {
		t.Errorf("q2 = %+v", q2)
	}
}
