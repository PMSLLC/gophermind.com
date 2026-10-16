package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/report"
	"gophermind/gophermind-lib/runview"
)

const goldenDir = "../../gophermind-lib/runview/testdata/golden"

func golden(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(goldenDir, name))
	if err != nil {
		t.Fatalf("golden %s: %v (run `go test ./gophermind-lib/runview -run TestGolden -update` from the repo root)", name, err)
	}
	return b
}

// sameJSON fails unless the re-encoded document b has, at every object level,
// exactly the keys of the golden document a, and equal values. Decoding a
// golden file into a client type and encoding it again must lose nothing: a
// field the server sends and the client type lacks shows up as a missing key
// at its path.
func sameJSON(t *testing.T, name string, a, b []byte) {
	t.Helper()
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		t.Fatalf("%s: not JSON", name)
	}
	keyDiff(t, name, "$", x, y)
	if !reflect.DeepEqual(x, y) {
		t.Errorf("%s: decoding into the client types and encoding again changed a value", name)
	}
}

func keyDiff(t *testing.T, name, path string, a, b any) {
	t.Helper()
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			t.Errorf("%s %s: object became %T", name, path, b)
			return
		}
		for k, x := range av {
			y, ok := bv[k]
			if !ok {
				t.Errorf("%s: the client lacks the field %s.%s the server sends", name, path, k)
				continue
			}
			keyDiff(t, name, path+"."+k, x, y)
		}
		for k := range bv {
			if _, ok := av[k]; !ok {
				t.Errorf("%s: the client adds the field %s.%s the server does not send", name, path, k)
			}
		}
	case []any:
		bv, ok := b.([]any)
		if !ok || len(bv) != len(av) {
			t.Errorf("%s %s: array changed", name, path)
			return
		}
		for i := range av {
			keyDiff(t, name, path+"[]", av[i], bv[i])
		}
	}
}

func goldenServer(t *testing.T, header string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if header != "" {
			w.Header().Set("X-Gophermind-Pipeline-Schema", header)
		}
		files := map[string]string{
			"/pipeline/runs": "runs.json", "/pipeline/state": "state.json", "/pipeline/node": "node-fn-bye.json",
			"/pipeline/decisions": "decisions.json", "/pipeline/report": "report-partial.json",
		}
		if r.URL.Path == "/pipeline/artifact" {
			io.WriteString(w, "--- FAIL: TestBye\n")
			return
		}
		if r.URL.Path == "/pipeline/events" {
			w.Header().Set("Content-Type", "text/event-stream")
			return
		}
		name, ok := files[r.URL.Path]
		if !ok {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(golden(t, name))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// goldenServerFile serves one golden file at one path, with the schema header.
func goldenServerFile(t *testing.T, path, name string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("X-Gophermind-Pipeline-Schema", "1")
		w.Write(golden(t, name))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPipelineClientDecodesTheServersGoldenFiles(t *testing.T) {
	srv := goldenServer(t, "1")
	c := New(Config{BaseURL: srv.URL, Token: "t"})
	ctx := context.Background()

	runs, err := c.PipelineRuns(ctx)
	if err != nil || len(runs) != 1 || runs[0].Status != "building" {
		t.Fatalf("PipelineRuns = %+v, %v", runs, err)
	}
	b, _ := json.Marshal(runview.RunsBody{SchemaVersion: 1, Runs: runs})
	sameJSON(t, "runs.json", golden(t, "runs.json"), b)

	st, err := c.PipelineState(ctx, "")
	if err != nil || st.Run == nil || len(st.Nodes) != 5 || st.EventSeq == 0 {
		t.Fatalf("PipelineState = %+v, %v", st, err)
	}
	b, _ = json.Marshal(st)
	sameJSON(t, "state.json", golden(t, "state.json"), b)

	nb, err := c.PipelineNode(ctx, "", "fn-bye")
	if err != nil || nb.Runtime == nil || len(nb.Artifacts) != 1 {
		t.Fatalf("PipelineNode = %+v, %v", nb, err)
	}
	b, _ = json.Marshal(nb)
	sameJSON(t, "node-fn-bye.json", golden(t, "node-fn-bye.json"), b)

	db, err := c.PipelineDecisions(ctx, "")
	if err != nil || len(db.Decisions) != 2 || !strings.Contains(string(db.Decisions[1]), "rv-fn-bye-1") {
		t.Fatalf("PipelineDecisions = %+v, %v", db, err)
	}
	b, _ = json.Marshal(db)
	sameJSON(t, "decisions.json", golden(t, "decisions.json"), b)

	rb, err := c.PipelineReport(ctx, "")
	if err != nil || rb.IsComplete() || rb.Status != "building" {
		t.Fatalf("PipelineReport = %+v, %v", rb, err)
	}
	b, _ = json.Marshal(rb)
	sameJSON(t, "report-partial.json", golden(t, "report-partial.json"), b)

	// The final report carries what the revise stage and the parallel levels did.
	final := goldenServerFile(t, "/pipeline/report", "report-final.json")
	cf := New(Config{BaseURL: final.URL, Token: "t"})
	rf, err := cf.PipelineReport(ctx, "")
	if err != nil || !rf.IsComplete() || len(rf.Revised) != 1 || rf.SchemaVersion != report.SchemaVersion {
		t.Fatalf("PipelineReport(final) = %+v, %v", rf, err)
	}
	b, _ = json.Marshal(rf)
	sameJSON(t, "report-final.json", golden(t, "report-final.json"), b)

	txt, err := c.PipelineArtifact(ctx, "", "fn-bye", 1, "check-output.txt")
	if err != nil || !strings.Contains(txt, "TestBye") {
		t.Fatalf("PipelineArtifact = %q, %v", txt, err)
	}
}

func TestPipelineClientSchemaMismatch(t *testing.T) {
	for name, header := range map[string]string{"no header (an old server)": "", "another version": "2"} {
		srv := goldenServer(t, header)
		c := New(Config{BaseURL: srv.URL, Token: "t"})
		calls := map[string]func() error{
			"runs":      func() error { _, err := c.PipelineRuns(context.Background()); return err },
			"state":     func() error { _, err := c.PipelineState(context.Background(), ""); return err },
			"node":      func() error { _, err := c.PipelineNode(context.Background(), "", "x"); return err },
			"decisions": func() error { _, err := c.PipelineDecisions(context.Background(), ""); return err },
			"report":    func() error { _, err := c.PipelineReport(context.Background(), ""); return err },
			"artifact":  func() error { _, err := c.PipelineArtifact(context.Background(), "", "x", 1, "reply.txt"); return err },
			"events":    func() error { _, err := c.PipelineEvents(context.Background(), "", -1); return err },
		}
		for call, f := range calls {
			var mm *SchemaMismatchError
			if err := f(); !errors.As(err, &mm) || mm.Want != 1 {
				t.Errorf("%s / %s: err = %v, want *SchemaMismatchError", name, call, err)
			}
		}
	}
}

func TestPipelineClientBodyVersionIsCheckedToo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Gophermind-Pipeline-Schema", "1")
		io.WriteString(w, `{"tasks":[],"generated_at":"2026-01-01T00:00:00Z"}`) // the old shape
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Token: "t"})
	var mm *SchemaMismatchError
	if _, err := c.PipelineState(context.Background(), ""); !errors.As(err, &mm) || mm.Got != 0 {
		t.Errorf("err = %v, want a mismatch with Got 0", err)
	}
}

func TestPipelineClientSendsRunAndQueryEscaped(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		w.Header().Set("X-Gophermind-Pipeline-Schema", "1")
		io.WriteString(w, `{"schema_version":1,"node":{},"runtime":null,"artifacts":[]}`)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Token: "t"})
	if _, err := c.PipelineNode(context.Background(), "gm-2026-10-08-001", "fn a&b"); err != nil {
		t.Fatal(err)
	}
	if got != "id=fn+a%26b&run=gm-2026-10-08-001" {
		t.Errorf("query = %q", got)
	}
}

func sseServer(t *testing.T, header string, onReq func(*http.Request), body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if onReq != nil {
			onReq(r)
		}
		if header != "" {
			w.Header().Set("X-Gophermind-Pipeline-Schema", header)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPipelineEventsParsesIDsAndDecodesEveryType(t *testing.T) {
	var query string
	body := ": keepalive\n\n" +
		"id: 7\nevent: node-status\ndata: {\"id\":\"fn-a\",\"status\":\"verified\",\"wave\":1,\"revision\":2}\n\n" +
		"id: 8\nevent: node-attempt\ndata: {\"id\":\"fn-a\",\"attempt\":{\"model\":\"m\",\"verdict\":\"pass\",\"started_at\":\"t\"}}\n\n" +
		"id: 8\nevent: node-result\ndata: {\"id\":\"fn-a\",\"result\":{\"commit\":\"abc\",\"files_changed\":[\"a.go\"]}}\n\n" +
		"id: 8\nevent: wave-changed\ndata: {\"wave\":1,\"state\":\"finished\"}\n\n" +
		"id: 8\nevent: run-status\ndata: {\"status\":\"verified\",\"stop_reason\":\"\"}\n\n" +
		"id: 8\nevent: run-report\ndata: {\"schema_version\":" + strconv.Itoa(report.SchemaVersion) + ",\"run_id\":\"r\",\"status\":\"verified\"}\n\n"
	srv := sseServer(t, "1", func(r *http.Request) { query = r.URL.RawQuery }, body)
	c := New(Config{BaseURL: srv.URL, Token: "t"})
	s, err := c.PipelineEvents(context.Background(), "gm-2026-10-08-001", 6)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if query != "run=gm-2026-10-08-001&since=6" {
		t.Errorf("query = %q", query)
	}
	if s.Header().Get("X-Gophermind-Pipeline-Schema") != "1" {
		t.Error("the stream must expose the response header")
	}
	next := func() Event {
		ev, err := s.Next()
		if err != nil {
			t.Fatal(err)
		}
		return ev
	}
	ev := next()
	ns, err := ev.NodeStatus()
	if ev.ID != 7 || ev.Type != "node-status" || err != nil || ns.ID != "fn-a" || ns.Status != "verified" || ns.Wave != 1 || ns.Revision != 2 {
		t.Errorf("node-status = %+v %+v %v", ev, ns, err)
	}
	if s.LastID() != 7 {
		t.Errorf("LastID = %d, want 7", s.LastID())
	}
	na, err := next().NodeAttempt()
	if err != nil || na.Attempt.Model != "m" || na.Attempt.Verdict != "pass" {
		t.Errorf("node-attempt = %+v %v", na, err)
	}
	nr, err := next().NodeResult()
	if err != nil || nr.Result.Commit != "abc" {
		t.Errorf("node-result = %+v %v", nr, err)
	}
	wc, err := next().WaveChanged()
	if err != nil || wc.Wave != 1 || wc.State != "finished" {
		t.Errorf("wave-changed = %+v %v", wc, err)
	}
	rs, err := next().RunStatus()
	if err != nil || rs.Status != "verified" {
		t.Errorf("run-status = %+v %v", rs, err)
	}
	rr, err := next().RunReport()
	if err != nil || rr.RunID != "r" || !rr.IsComplete() {
		t.Errorf("run-report = %+v %v", rr, err)
	}
	if s.LastID() != 8 {
		t.Errorf("LastID = %d, want 8", s.LastID())
	}
}

func TestPipelineEventsOmitsSinceWhenNegative(t *testing.T) {
	var query string
	srv := sseServer(t, "1", func(r *http.Request) { query = r.URL.RawQuery }, "")
	c := New(Config{BaseURL: srv.URL, Token: "t"})
	s, err := c.PipelineEvents(context.Background(), "", -1)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if query != "" {
		t.Errorf("query = %q, want none", query)
	}
}

func TestPipelineEventsStreamCapIsAStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Gophermind-Pipeline-Schema", "1")
		http.Error(w, "too many streams", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Token: "t"})
	_, err := c.PipelineEvents(context.Background(), "", -1)
	var se *StatusError
	if !errors.As(err, &se) || se.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("err = %v, want a 503 *StatusError", err)
	}
}
