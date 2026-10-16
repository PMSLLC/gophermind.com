package serve

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/runview"
	"gophermind/gophermind-lib/runview/runviewtest"
)

func apiServer(t *testing.T, root string) *httptest.Server {
	t.Helper()
	t.Setenv("GOPHERMIND_SERVE_TOKEN", "t")
	mux, err := NewMux(Deps{Pipeline: &PipelineDeps{Root: root}}, Options{})
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func apiGet(t *testing.T, srv *httptest.Server, path, token string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func TestPipelineRoutesRequireTheTokenAndCarryTheSchemaHeader(t *testing.T) {
	srv := apiServer(t, t.TempDir())
	for _, p := range []string{"/pipeline/runs", "/pipeline/state", "/pipeline/node?id=x", "/pipeline/artifact", "/pipeline/decisions", "/pipeline/report"} {
		resp, _ := apiGet(t, srv, p, "")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s without a token: %d, want 401", p, resp.StatusCode)
		}
		if resp.Header.Get(PipelineSchemaHeader) != "1" {
			t.Errorf("%s: a 401 must still carry %s: 1", p, PipelineSchemaHeader)
		}
	}
	resp, _ := apiGet(t, srv, "/pipeline/runs", "wrong")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong token: %d", resp.StatusCode)
	}
}

func TestPipelineRunsAndState(t *testing.T) {
	root := t.TempDir()
	runviewtest.BuildID(t, root, "gm-2026-10-07-001", runviewtest.Verified)
	runviewtest.BuildID(t, root, "gm-2026-10-08-001", runviewtest.Building)
	srv := apiServer(t, root)

	resp, body := apiGet(t, srv, "/pipeline/runs", "t")
	var runs runview.RunsBody
	if resp.StatusCode != 200 || json.Unmarshal(body, &runs) != nil || runs.SchemaVersion != 1 ||
		len(runs.Runs) != 2 || runs.Runs[0].RunID != "gm-2026-10-08-001" {
		t.Fatalf("runs = %d %s", resp.StatusCode, body)
	}
	if resp.Header.Get(PipelineSchemaHeader) != "1" || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Errorf("headers = %v", resp.Header)
	}

	// Default run is the newest.
	resp, body = apiGet(t, srv, "/pipeline/state", "t")
	var st runview.StateBody
	if resp.StatusCode != 200 || json.Unmarshal(body, &st) != nil || st.Run == nil || st.Run.RunID != "gm-2026-10-08-001" ||
		st.SchemaVersion != 1 || len(st.Nodes) != 5 || st.EventSeq == 0 {
		t.Fatalf("state = %d %s", resp.StatusCode, body)
	}
	// An explicit run.
	_, body = apiGet(t, srv, "/pipeline/state?run=gm-2026-10-07-001", "t")
	_ = json.Unmarshal(body, &st)
	if st.Run == nil || st.Run.Status != "verified" {
		t.Errorf("explicit run = %s", body)
	}
	// Unknown but well-formed: 404. Malformed: 400.
	if resp, _ := apiGet(t, srv, "/pipeline/state?run=gm-2026-10-09-009", "t"); resp.StatusCode != 404 {
		t.Errorf("unknown run: %d, want 404", resp.StatusCode)
	}
	if resp, _ := apiGet(t, srv, "/pipeline/state?run=..%2F..%2Fetc", "t"); resp.StatusCode != 400 {
		t.Errorf("malformed run: %d, want 400", resp.StatusCode)
	}
}

func TestPipelineStateWithNoRunIsEmptyNotAnError(t *testing.T) {
	srv := apiServer(t, t.TempDir())
	resp, body := apiGet(t, srv, "/pipeline/state", "t")
	var st runview.StateBody
	if resp.StatusCode != 200 || json.Unmarshal(body, &st) != nil || st.Run != nil || st.Nodes == nil || len(st.Nodes) != 0 || st.SchemaVersion != 1 {
		t.Fatalf("empty state = %d %s", resp.StatusCode, body)
	}
	if resp, _ := apiGet(t, srv, "/pipeline/node?id=fn-greet", "t"); resp.StatusCode != 404 {
		t.Errorf("node with no run: %d, want 404", resp.StatusCode)
	}
}

func TestPipelineNodeArtifactDecisions(t *testing.T) {
	root := t.TempDir()
	runviewtest.Build(t, root, runviewtest.Building)
	srv := apiServer(t, root)

	resp, body := apiGet(t, srv, "/pipeline/node?id=fn-bye", "t")
	var nb runview.NodeBody
	if resp.StatusCode != 200 || json.Unmarshal(body, &nb) != nil || nb.Runtime == nil || nb.Runtime.Status != "in_progress" || len(nb.Artifacts) != 1 {
		t.Fatalf("node = %d %s", resp.StatusCode, body)
	}
	if resp, _ := apiGet(t, srv, "/pipeline/node?id=..%2Fx", "t"); resp.StatusCode != 400 {
		t.Errorf("bad node id: %d, want 400", resp.StatusCode)
	}
	if resp, _ := apiGet(t, srv, "/pipeline/node?id=nope", "t"); resp.StatusCode != 404 {
		t.Errorf("unknown node: %d, want 404", resp.StatusCode)
	}

	resp, body = apiGet(t, srv, "/pipeline/artifact?id=fn-bye&attempt=1&file=check-output.txt", "t")
	if resp.StatusCode != 200 || !strings.Contains(string(body), "want bye, got hello") ||
		!strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("artifact = %d %q %v", resp.StatusCode, body, resp.Header)
	}
	for q, want := range map[string]int{
		"id=fn-bye&attempt=1&file=prompt.txt":     400,
		"id=fn-bye&attempt=x&file=reply.txt":      400,
		"id=fn-bye&attempt=0&file=reply.txt":      400,
		"id=fn-bye&attempt=1&file=..%2Freply.txt": 400,
		"id=fn-bye&attempt=7&file=reply.txt":      404,
		"id=fn-late&attempt=1&file=reply.txt":     404,
		"attempt=1&file=reply.txt":                400,
	} {
		if resp, _ := apiGet(t, srv, "/pipeline/artifact?"+q, "t"); resp.StatusCode != want {
			t.Errorf("artifact?%s: %d, want %d", q, resp.StatusCode, want)
		}
	}

	resp, body = apiGet(t, srv, "/pipeline/decisions", "t")
	var db runview.DecisionsBody
	if resp.StatusCode != 200 || json.Unmarshal(body, &db) != nil || len(db.Decisions) != 2 || !strings.Contains(string(db.Decisions[1]), `"rv-fn-bye-1"`) || db.SchemaVersion != 1 {
		t.Fatalf("decisions = %d %s", resp.StatusCode, body)
	}
}

func TestPipelineReportPartialThenVerbatim(t *testing.T) {
	root := t.TempDir()
	f := runviewtest.Build(t, root, runviewtest.Building)
	srv := apiServer(t, root)
	_, body := apiGet(t, srv, "/pipeline/report", "t")
	var rb runview.ReportBody
	if json.Unmarshal(body, &rb) != nil || rb.IsComplete() || rb.Status != "building" {
		t.Fatalf("running report = %s", body)
	}

	root2 := t.TempDir()
	f2 := runviewtest.Build(t, root2, runviewtest.Verified)
	srv2 := apiServer(t, root2)
	resp, body := apiGet(t, srv2, "/pipeline/report", "t")
	want, _ := os.ReadFile(filepath.Join(f2.Dir, "report.json"))
	if resp.StatusCode != 200 || string(body) != string(want) {
		t.Errorf("the finished report must be served byte for byte")
	}
	if resp.Header.Get(PipelineSchemaHeader) != "1" {
		t.Errorf("the report response needs the schema header (its body keeps the report's own version)")
	}
	_ = f
}

func TestPipelineErrorBodiesNeverEchoTheRequest(t *testing.T) {
	root := t.TempDir()
	runviewtest.Build(t, root, runviewtest.Planned)
	srv := apiServer(t, root)
	for _, p := range []string{
		"/pipeline/state?run=..%2F..%2Fetc%2Fpasswd", "/pipeline/node?id=%3Cscript%3E", "/pipeline/node?id=secretnode",
		"/pipeline/artifact?id=fn-bye&attempt=1&file=%2Fetc%2Fpasswd",
	} {
		_, body := apiGet(t, srv, p, "t")
		for _, bad := range []string{"passwd", "script", "secretnode", root} {
			if strings.Contains(string(body), bad) {
				t.Errorf("%s: the error body echoes %q: %s", p, bad, body)
			}
		}
	}
}

func TestPipelineRoutesAreGetOnly(t *testing.T) {
	srv := apiServer(t, t.TempDir())
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/pipeline/state", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer t")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /pipeline/state: %d, want 405", resp.StatusCode)
	}
}

func TestPipelineDefaultRunIsTheMostRecentlyUpdated(t *testing.T) {
	root := t.TempDir()
	older := runviewtest.BuildID(t, root, "gm-2026-10-09-001", runviewtest.Verified)
	runviewtest.BuildID(t, root, "gm-2026-10-01-001", runviewtest.Building)
	old := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(filepath.Join(older.Dir, "report.json"), old, old)
	_ = os.Chtimes(filepath.Join(older.Dir, "brief.md"), old, old)
	srv := apiServer(t, root)
	_, body := apiGet(t, srv, "/pipeline/state", "t")
	var st runview.StateBody
	if json.Unmarshal(body, &st) != nil || st.Run == nil || st.Run.RunID != "gm-2026-10-01-001" {
		t.Fatalf("default run = %s", body)
	}
}
