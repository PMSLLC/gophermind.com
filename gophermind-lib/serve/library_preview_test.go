//go:build !windows

package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

type previewJSON struct {
	Family   string   `json:"family"`
	Version  string   `json:"version"`
	Parent   string   `json:"parent"`
	Children []string `json:"children"`
	Locked   bool     `json:"locked"`
	Sections []struct {
		Heading string `json:"heading"`
		Body    string `json:"body"`
	} `json:"sections"`
	Problems   []map[string]any `json:"problems"`
	Warnings   []map[string]any `json:"warnings"`
	AskDefault bool             `json:"ask_default"`
	RunName    string           `json:"run_name"`
	TextBytes  int              `json:"text_bytes"`
}

func getPreview(t *testing.T, e *briefEnv, v string) previewJSON {
	t.Helper()
	w := e.get("/app/api/library/csvstat/" + v + "/preview")
	if w.Code != http.StatusOK {
		t.Fatalf("preview = %d %s", w.Code, w.Body)
	}
	var p previewJSON
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func runDirCount(t *testing.T, e *briefEnv) int {
	t.Helper()
	ents, err := os.ReadDir(e.dir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, x := range ents {
		if x.IsDir() && strings.HasPrefix(x.Name(), "csvstat-") {
			n++
		}
	}
	return n
}

// noAcceptance saves a version of csvstat without its Acceptance section.
func noAcceptance(t *testing.T, e *briefEnv, m *briefManager) string {
	t.Helper()
	body := string(e.versionBody())
	i := strings.Index(body, "## Acceptance")
	if i < 0 {
		t.Fatal("fixture has no Acceptance")
	}
	v, err := m.lib.Save("csvstat", "v1", "no acceptance", []byte(body[:i]))
	if err != nil {
		t.Fatal(err)
	}
	return v.Name
}

func runPost(e *briefEnv, v, body string) *httptest.ResponseRecorder {
	return e.post("/app/api/library/csvstat/"+v+"/run", body, sameOrigin)
}

func TestPreviewSections(t *testing.T) {
	e, _ := libEnv(t)
	p := getPreview(t, e, "v1")
	want := []string{"Overview", "Features", "Architecture", "Data", "Constraints", "Out of scope", "Acceptance"}
	if len(p.Sections) != len(want) {
		t.Fatalf("%d sections: %+v", len(p.Sections), p.Sections)
	}
	for i, h := range want {
		if p.Sections[i].Heading != h {
			t.Errorf("section %d = %q, want %q", i, p.Sections[i].Heading, h)
		}
		if strings.TrimSpace(p.Sections[i].Body) == "" {
			t.Errorf("section %q has no body", h)
		}
	}
	if !strings.Contains(p.Sections[0].Body, "HTTP service") || strings.Contains(p.Sections[0].Body, "## ") {
		t.Errorf("overview body %q", p.Sections[0].Body)
	}
	if len(p.Problems) != 0 || !p.AskDefault || p.Family != "csvstat" || p.Version != "v1" || p.Locked || p.TextBytes == 0 {
		t.Errorf("%+v", p)
	}
}

func TestPreviewShowsProblems(t *testing.T) {
	e, m := libEnv(t)
	v := noAcceptance(t, e, m)
	p := getPreview(t, e, v)
	if len(p.Problems) == 0 {
		t.Fatal("no problems for a brief without Acceptance")
	}
	if !strings.Contains(strings.ToLower(jsonString(p.Problems)), "acceptance") {
		t.Errorf("problems %v do not name Acceptance", p.Problems)
	}
	last := p.Sections[6]
	if last.Heading != "Acceptance" || last.Body != "" {
		t.Errorf("section 7 = %+v", last)
	}
	before := runDirCount(t, e)
	w := runPost(e, v, `{"ask":false}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("run = %d %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "problems") {
		t.Errorf("no problems in %s", w.Body)
	}
	if n := runDirCount(t, e); n != before {
		t.Errorf("run folders %d -> %d", before, n)
	}
	if got := getPreview(t, e, v); got.Locked {
		t.Error("refused run locked the version")
	}
}

func jsonString(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestPreviewRunNameMatchesNext(t *testing.T) {
	e, _ := libEnv(t)
	p := getPreview(t, e, "v1")
	if !strings.HasSuffix(p.RunName, "-r001") || !strings.HasPrefix(p.RunName, "csvstat-v1-") {
		t.Fatalf("name %q", p.RunName)
	}
	w := runPost(e, "v1", `{"ask":false}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("run = %d %s", w.Code, w.Body)
	}
	var got struct {
		RunID string `json:"run_id"`
		URL   string `json:"url"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.RunID != p.RunName || got.URL != "/app/briefs/"+got.RunID {
		t.Errorf("run %+v, preview said %q", got, p.RunName)
	}
	e.waitNotActive(got.RunID)
	p2 := getPreview(t, e, "v1")
	if !strings.HasSuffix(p2.RunName, "-r002") || !p2.Locked {
		t.Errorf("after a run: %q locked=%v", p2.RunName, p2.Locked)
	}
	w = runPost(e, "v1", `{"ask":false}`)
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), p2.RunName) {
		t.Errorf("second run = %d %s, want %s", w.Code, w.Body, p2.RunName)
	}
}

func TestCancelStartsNothing(t *testing.T) {
	e, _ := libEnv(t)
	before := runDirCount(t, e)
	for _, p := range []string{"/app/api/library/csvstat/v1/preview", "/app/library/csvstat/v1/preview"} {
		if w := e.get(p); w.Code != http.StatusOK {
			t.Fatalf("%s = %d", p, w.Code)
		}
	}
	if n := runDirCount(t, e); n != before {
		t.Errorf("GET made run folders: %d -> %d", before, n)
	}
	if getPreview(t, e, "v1").Locked {
		t.Error("GET locked the version")
	}
	page, err := pagesAssets.ReadFile("assets/library_preview.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(page)
	if !strings.Contains(s, `<a class="btn" id="cancel">`) || !strings.Contains(s, "getElementById('cancel').href = edit") {
		t.Error("Cancel is not a plain link to the editor")
	}
	if strings.Contains(s, "getElementById('cancel').addEventListener") || strings.Count(s, "fetch(") != 2 || strings.Count(s, "'POST'") != 1 {
		t.Error("the page posts or fetches more than preview and run")
	}
}

func TestRunEndpointBusy(t *testing.T) {
	e, m := libEnv(t)
	e.script = func(args []string) string { return "sleep 30" }
	if _, err := m.lib.Save("csvstat", "v1", "b", mustGet(t, m, "v1")); err != nil {
		t.Fatal(err)
	}
	w := runPost(e, "v1", `{"ask":false}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("first run = %d %s", w.Code, w.Body)
	}
	var first struct {
		RunID string `json:"run_id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &first)
	defer e.stopAndDrain(first.RunID)
	w = runPost(e, "v1.1", `{}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("busy run = %d %s", w.Code, w.Body)
	}
	var j struct {
		Active string `json:"active_run_id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &j)
	if j.Active != first.RunID {
		t.Errorf("active_run_id %q, want %q", j.Active, first.RunID)
	}
	if getPreview(t, e, "v1.1").Locked {
		t.Error("busy run locked the version")
	}
}

func TestRunEndpointAskDefault(t *testing.T) {
	for _, tc := range []struct {
		body string
		want bool
	}{{``, true}, {`{}`, true}, {`{"ask":true}`, true}, {`{"ask":false}`, false}} {
		e, m := libEnv(t)
		w := runPost(e, "v1", tc.body)
		if w.Code != http.StatusAccepted {
			t.Fatalf("%q: run = %d %s", tc.body, w.Code, w.Body)
		}
		var j struct {
			RunID string `json:"run_id"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &j)
		rec, ok := m.readRecord(j.RunID)
		if !ok || rec.Ask != tc.want {
			t.Errorf("body %q: record ask = %v (found %v), want %v", tc.body, rec.Ask, ok, tc.want)
		}
		e.stopAndDrain(j.RunID)
		e.waitNotActive(j.RunID)
	}
}

func TestPreviewPageNoInnerHTML(t *testing.T) {
	page, err := pagesAssets.ReadFile("assets/library_preview.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"innerHTML", "outerHTML", "document.write", "insertAdjacentHTML", "http://", "https://", "<link"} {
		if strings.Contains(string(page), bad) {
			t.Errorf("page contains %q", bad)
		}
	}
}

func TestPreviewRoutesNeedAuth(t *testing.T) {
	e, _ := libEnv(t)
	for _, tc := range []struct{ method, path string }{
		{"GET", "/app/api/library/csvstat/v1/preview"},
		{"POST", "/app/api/library/csvstat/v1/run"},
	} {
		if w := do(e.h, tc.method, tc.path, nil); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", tc.method, tc.path, w.Code)
		}
	}
	w := do(e.h, "GET", "/app/library/csvstat/v1/preview", map[string]string{"Accept": "text/html"})
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/auth/login" {
		t.Errorf("page = %d %s", w.Code, w.Header().Get("Location"))
	}
	before := runDirCount(t, e)
	if w := e.post("/app/api/library/csvstat/v1/run", `{}`, map[string]string{"Sec-Fetch-Site": "cross-site"}); w.Code != http.StatusForbidden {
		t.Errorf("cross-site run = %d", w.Code)
	}
	if runDirCount(t, e) != before {
		t.Error("cross-site run made a folder")
	}
}

func TestPreviewExtraSectionsKept(t *testing.T) {
	e, m := libEnv(t)
	body := string(e.versionBody()) + "\n## Notes\n\nKeep this.\n\n```\n## not a heading\n```\n"
	v, err := m.lib.Save("csvstat", "v1", "extra", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	p := getPreview(t, e, v.Name)
	if len(p.Sections) != 8 || p.Sections[7].Heading != "Notes" {
		t.Fatalf("sections %+v", p.Sections)
	}
	if !strings.Contains(p.Sections[7].Body, "Keep this.") || !strings.Contains(p.Sections[7].Body, "## not a heading") {
		t.Errorf("notes body %q", p.Sections[7].Body)
	}
	if p.Sections[0].Heading != "Overview" || p.Parent != "v1" {
		t.Errorf("%+v", p)
	}
}
