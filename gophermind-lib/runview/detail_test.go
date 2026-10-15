package runview_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/report"
	"gophermind/gophermind-lib/runview"
	"gophermind/gophermind-lib/runview/runviewtest"
)

var update = flag.Bool("update", false, "rewrite the golden files under testdata/golden")

func fixedClock() func() time.Time {
	at := time.Date(2026, 10, 8, 13, 0, 0, 0, time.UTC)
	return func() time.Time { return at }
}

func open(t *testing.T, st runviewtest.Stage) (*runview.View, runviewtest.Fixture) {
	t.Helper()
	root := t.TempDir()
	f := runviewtest.Build(t, root, st)
	v, err := runview.Open(root, runviewtest.RunID, runview.WithClock(fixedClock()))
	if err != nil {
		t.Fatal(err)
	}
	return v, f
}

func TestNodeBodyCarriesGroupsRuntimeAndArtifacts(t *testing.T) {
	v, _ := open(t, runviewtest.Building)
	b, err := v.Node("fn-bye")
	if err != nil {
		t.Fatal(err)
	}
	if b.SchemaVersion != 1 || b.Runtime == nil || b.Runtime.Status != "in_progress" || len(b.Runtime.Attempts) != 1 {
		t.Fatalf("body = %+v", b)
	}
	if len(b.Artifacts) != 1 || b.Artifacts[0].Attempt != 1 ||
		strings.Join(b.Artifacts[0].Files, ",") != "reply.txt,check-output.txt,attempt.json" {
		t.Errorf("artifacts = %+v", b.Artifacts)
	}
	var doc map[string]any
	if err := json.Unmarshal(b.Node, &doc); err != nil || doc["id"] != "fn-bye" {
		t.Errorf("node doc = %v, %v", doc, err)
	}
	g, err := v.Node("fn-greet")
	if err != nil {
		t.Fatal(err)
	}
	var gd map[string]any
	_ = json.Unmarshal(g.Node, &gd)
	if gd["rationale"] == nil || gd["construction"] == nil {
		t.Errorf("the full node, groups included, must be returned: %v", gd)
	}
	r, err := v.Node(runviewtest.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var rd map[string]any
	_ = json.Unmarshal(r.Node, &rd)
	if ds, _ := rd["decisions"].([]any); len(ds) != 1 {
		t.Errorf("the root node must carry its decisions: %v", rd["decisions"])
	}
}

func TestNodeBeforeTheExecutorStartedHasNoRuntime(t *testing.T) {
	v, _ := open(t, runviewtest.Planned)
	b, err := v.Node("fn-greet")
	if err != nil || b.Runtime != nil || b.Artifacts == nil || len(b.Artifacts) != 0 {
		t.Errorf("body = %+v, %v (runtime nil, artifacts an empty list)", b, err)
	}
}

func TestNodeRefusesBadAndUnknownIDs(t *testing.T) {
	v, _ := open(t, runviewtest.Building)
	for _, id := range []string{"", "../x", "Fn", "a/b", ".."} {
		if _, err := v.Node(id); !errors.Is(err, runview.ErrBadID) {
			t.Errorf("Node(%q) = %v, want ErrBadID", id, err)
		}
	}
	if _, err := v.Node("nope"); !errors.Is(err, runview.ErrNotFound) {
		t.Errorf("unknown node = %v, want ErrNotFound", err)
	}
}

func TestArtifact(t *testing.T) {
	v, f := open(t, runviewtest.Building)
	txt, err := v.Artifact("fn-bye", 1, "check-output.txt")
	if err != nil || !strings.Contains(txt, "want bye, got hello") {
		t.Fatalf("artifact = %q, %v", txt, err)
	}
	bad := []struct {
		id      string
		attempt int
		file    string
	}{
		{"fn-bye", 0, "reply.txt"}, {"fn-bye", -1, "reply.txt"}, {"fn-bye", 1, "prompt.txt"},
		{"fn-bye", 1, "../reply.txt"}, {"fn-bye", 1, "reply.json"}, {"../fn-bye", 1, "reply.txt"}, {"fn-bye", 1, ""},
	}
	for _, c := range bad {
		if _, err := v.Artifact(c.id, c.attempt, c.file); !errors.Is(err, runview.ErrBadID) {
			t.Errorf("Artifact(%+v) = %v, want ErrBadID", c, err)
		}
	}
	if _, err := v.Artifact("fn-bye", 9, "reply.txt"); !errors.Is(err, runview.ErrNotFound) {
		t.Errorf("missing attempt = %v, want ErrNotFound", err)
	}
	if _, err := v.Artifact("fn-bye", 1, "reply.go"); !errors.Is(err, runview.ErrNotFound) {
		t.Errorf("file not saved = %v, want ErrNotFound", err)
	}
	// A symbolic link in the attempt folder is refused and not listed.
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("do not serve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(f.Dir, "attempts", "fn-bye", "1", "reply.go")); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Artifact("fn-bye", 1, "reply.go"); !errors.Is(err, runview.ErrNotFound) {
		t.Errorf("symlinked artifact = %v, want ErrNotFound", err)
	}
	for _, a := range v.ArtifactNames("fn-bye") {
		for _, name := range a.Files {
			if name == "reply.go" {
				t.Error("a symlinked file must not be listed")
			}
		}
	}
	// Symlinked directories are refused too.
	elsewhere := t.TempDir()
	if err := os.WriteFile(filepath.Join(elsewhere, "reply.txt"), []byte("do not serve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(elsewhere, "1"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(elsewhere, "1", "reply.txt"), []byte("do not serve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(f.Dir, "attempts", "fn-link")); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Artifact("fn-link", 1, "reply.txt"); !errors.Is(err, runview.ErrNotFound) {
		t.Errorf("symlinked node dir = %v, want ErrNotFound", err)
	}
	if err := os.Symlink(filepath.Join(elsewhere, "1"), filepath.Join(f.Dir, "attempts", "fn-bye", "7")); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Artifact("fn-bye", 7, "reply.txt"); !errors.Is(err, runview.ErrNotFound) {
		t.Errorf("symlinked attempt dir = %v, want ErrNotFound", err)
	}
	// A file over the size cap is cut and says so.
	huge := bytes.Repeat([]byte("a"), 9<<20)
	if err := os.WriteFile(filepath.Join(f.Dir, "attempts", "fn-bye", "1", "reply.txt"), huge, 0o600); err != nil {
		t.Fatal(err)
	}
	txt, err = v.Artifact("fn-bye", 1, "reply.txt")
	if err != nil || !strings.HasSuffix(txt, "\n[truncated by the server]\n") || len(txt) >= len(huge) {
		t.Errorf("oversize artifact: err %v, len %d", err, len(txt))
	}
}

func TestDecisionsAreTheRootRecordsThenTheRevisionDecisions(t *testing.T) {
	v, f := open(t, runviewtest.Building)
	d, err := v.Decisions()
	if err != nil || d.SchemaVersion != 1 || len(d.Decisions) != 2 {
		t.Fatalf("decisions = %+v, %v", d, err)
	}
	var rec, rv map[string]any
	_ = json.Unmarshal(d.Decisions[0], &rec)
	_ = json.Unmarshal(d.Decisions[1], &rv)
	if rec["id"] != "q1" || rec["answer"] != "memory" {
		t.Errorf("root record = %v", rec)
	}
	if rv["id"] != "rv-fn-bye-1" || !strings.HasPrefix(rv["answer"].(string), "retry") {
		t.Errorf("revision record = %v", rv)
	}

	// A damaged revision file is a warning, never an error; the root's records stay.
	if err := os.WriteFile(filepath.Join(f.Dir, "_state", "revisions", "decisions.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err = v.Decisions()
	if err != nil || len(d.Decisions) != 1 {
		t.Fatalf("damaged: %+v, %v", d, err)
	}
	if len(v.Warnings()) == 0 {
		t.Error("a damaged revision decisions file must add a warning")
	}
}

func TestReportFinalIsVerbatimAndRunningIsPartial(t *testing.T) {
	v, f := open(t, runviewtest.Verified)
	got, err := v.Report()
	if err != nil {
		t.Fatal(err)
	}
	want, _ := os.ReadFile(filepath.Join(f.Dir, "report.json"))
	if !bytes.Equal(got, want) {
		t.Error("the final report must be served byte for byte")
	}
	var fin runview.ReportBody
	if err := json.Unmarshal(got, &fin); err != nil || !fin.IsComplete() || fin.Status != "verified" {
		t.Errorf("final = %+v, %v", fin, err)
	}
	if fin.SchemaVersion != report.SchemaVersion || len(fin.Revised) != 1 || fin.Rewritten != 1 || fin.Parallel == nil {
		t.Errorf("revision and parallel totals must pass through: %+v", fin)
	}

	var finKeys map[string]any
	if err := json.Unmarshal(got, &finKeys); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"revised_leaves", "rewritten_leaves", "parallel"} {
		if _, ok := finKeys[k]; !ok {
			t.Errorf("final report lacks key %q", k)
		}
	}

	v2, _ := open(t, runviewtest.Building)
	part, err := v2.Report()
	if err != nil {
		t.Fatal(err)
	}
	var partKeys map[string]any
	if err := json.Unmarshal(part, &partKeys); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"revised_leaves", "rewritten_leaves", "parallel"} {
		if _, ok := partKeys[k]; ok {
			t.Errorf("partial report must not carry key %q", k)
		}
	}
	var pr runview.ReportBody
	if err := json.Unmarshal(part, &pr); err != nil {
		t.Fatal(err)
	}
	if pr.IsComplete() || pr.RunID != runviewtest.RunID || pr.Status != "building" || len(pr.ByTaskType) == 0 {
		t.Errorf("partial = complete %v, run %q, status %q, %d task rows", pr.IsComplete(), pr.RunID, pr.Status, len(pr.ByTaskType))
	}
}

func TestReportWithAnUnreadableFileIsAnErrorNotAGuess(t *testing.T) {
	v, f := open(t, runviewtest.Verified)
	if err := os.WriteFile(filepath.Join(f.Dir, "report.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Report(); err == nil || errors.Is(err, runview.ErrNotFound) {
		t.Errorf("Report = %v, want a plain error", err)
	}
}

func TestReportOverTheSizeCapIsAnErrorNotTruncatedJSON(t *testing.T) {
	v, f := open(t, runviewtest.Verified)
	big := append([]byte(`{"schema_version":1,"pad":"`), bytes.Repeat([]byte("a"), 9<<20)...)
	big = append(big, []byte(`"}`)...)
	if err := os.WriteFile(filepath.Join(f.Dir, "report.json"), big, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := v.Report(); err == nil || got != nil {
		t.Errorf("Report = %d bytes, %v; want an error", len(got), err)
	}
}

func TestDetailReadersNeverWrite(t *testing.T) {
	v, f := open(t, runviewtest.Building)
	before := runviewtest.Digest(t, f.Root)
	_, _ = v.Node("fn-bye")
	_ = v.ArtifactNames("fn-bye")
	_, _ = v.Artifact("fn-bye", 1, "reply.txt")
	_, _ = v.Decisions()
	_, _ = v.Report()
	if runviewtest.Digest(t, f.Root) != before {
		t.Fatal("a reader changed the run folder")
	}
}

// golden compares v, marshaled as indented JSON, with testdata/golden/<name>.
// The desktop client tests decode the same files, so a change to the contract
// is a visible diff here and a red test there. With -update the file is
// rewritten; review the diff before committing it. A field that varies between
// runs is zeroed by the caller, never by adding a clock to production code.
func golden(t *testing.T, name string, v any) {
	t.Helper()
	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s: run `go test ./gophermind-lib/runview -run TestGolden -update`", name)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from its golden; if the change is intended run with -update and review the diff", name)
	}
}

func TestGoldenContract(t *testing.T) {
	v, f := open(t, runviewtest.Building)

	runs, _ := runview.Runs(f.Root)
	for i := range runs {
		runs[i].UpdatedAt = "" // file modification time
		runs[i].RepoName = "repo"
	}
	golden(t, "runs.json", runview.RunsBody{SchemaVersion: runview.SchemaVersion, Runs: runs})

	st := v.State()
	st.Run.UpdatedAt, st.Run.RepoName = "", "repo"
	golden(t, "state.json", st)

	nb, err := v.Node("fn-bye")
	if err != nil {
		t.Fatal(err)
	}
	nb.Runtime.UpdatedAt, nb.Runtime.HeartbeatAt = "", "" // written at run time
	golden(t, "node-fn-bye.json", nb)

	db, _ := v.Decisions()
	golden(t, "decisions.json", db)

	partial, err := v.Report()
	if err != nil {
		t.Fatal(err)
	}
	var pr runview.ReportBody
	if err := json.Unmarshal(partial, &pr); err != nil {
		t.Fatal(err)
	}
	golden(t, "report-partial.json", pr)

	fv, _ := open(t, runviewtest.Verified)
	final, err := fv.Report()
	if err != nil {
		t.Fatal(err)
	}
	var fr runview.ReportBody
	if err := json.Unmarshal(final, &fr); err != nil {
		t.Fatal(err)
	}
	golden(t, "report-final.json", fr)

	// The raw key set of the final report is the contract the client decodes.
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(final, &keys); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"schema_version", "revised_leaves", "rewritten_leaves", "parallel"} {
		if _, ok := keys[k]; !ok {
			t.Errorf("report-final is missing the key %q", k)
		}
	}
	var sv int
	_ = json.Unmarshal(keys["schema_version"], &sv)
	if sv != report.SchemaVersion {
		t.Errorf("report-final schema_version = %d, want %d", sv, report.SchemaVersion)
	}
	var pk map[string]json.RawMessage
	_ = json.Unmarshal(partial, &pk)
	for _, k := range []string{"revised_leaves", "rewritten_leaves", "parallel"} {
		if _, ok := pk[k]; ok {
			t.Errorf("the partial report must not carry %q", k)
		}
	}
	if string(pk["complete"]) != "false" {
		t.Errorf("partial complete = %s", pk["complete"])
	}
}
