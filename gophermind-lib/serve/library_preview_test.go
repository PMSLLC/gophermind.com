//go:build !windows

package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
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

// runPostTok posts like the preview page: fields (a JSON object body without
// braces, may be empty) plus the version's current preview token.
func runPostTok(t *testing.T, e *briefEnv, v, fields string) *httptest.ResponseRecorder {
	t.Helper()
	tok := getPreviewTok(t, e, v)
	if fields != "" {
		fields += ","
	}
	return runPost(e, v, `{`+fields+`"preview_token":"`+tok+`"}`)
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
	if len(p.Problems) != 0 || !p.AskDefault || p.Family != "csvstat" || p.Version != "v1" || p.Locked {
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
	w := runPostTok(t, e, v, `"ask":false`)
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
	w := runPostTok(t, e, "v1", `"ask":false`)
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
	w = runPostTok(t, e, "v1", `"ask":false`)
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
	w := runPostTok(t, e, "v1", `"ask":false`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("first run = %d %s", w.Code, w.Body)
	}
	var first struct {
		RunID string `json:"run_id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &first)
	defer e.stopAndDrain(first.RunID)
	w = runPostTok(t, e, "v1.1", ``)
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
	}{{``, true}, {`"ask":true`, true}, {`"ask":false`, false}} {
		e, m := libEnv(t)
		w := runPostTok(t, e, "v1", tc.body)
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

func TestPreviewSectionsLargeBodyIsFast(t *testing.T) {
	text := []byte("---\ntitle: x\n---\n\n## Overview\n\n" + strings.Repeat("x\n", 125000) + "\n## Features\n\nf\n")
	start := time.Now()
	got := previewSections(text)
	if d := time.Since(start); d > time.Second {
		t.Fatalf("previewSections took %v", d)
	}
	if len(got[0].Body) < 249999 || got[1].Body != "f" {
		t.Errorf("bodies %d / %q", len(got[0].Body), got[1].Body)
	}
}

func TestPreviewSectionsCases(t *testing.T) {
	heads := func(s []previewSection) string {
		var h []string
		for _, x := range s {
			h = append(h, x.Heading)
		}
		return strings.Join(h, "|")
	}
	const req = "Overview|Features|Architecture|Data|Constraints|Out of scope|Acceptance"
	t.Run("crlf", func(t *testing.T) {
		got := previewSections([]byte("---\r\ntitle: x\r\n---\r\n\r\n## Overview\r\n\r\nhello\r\nthere\r\n\r\n## Data\r\n\r\nd\r\n"))
		if got[0].Body != "hello\nthere" || got[3].Body != "d" || heads(got) != req {
			t.Errorf("%+v", got)
		}
	})
	t.Run("lowercase heading", func(t *testing.T) {
		got := previewSections([]byte("## out of scope\n\nnope\n"))
		if heads(got) != req+"|out of scope" || got[5].Body != "" || got[7].Body != "nope" {
			t.Errorf("%+v", got)
		}
	})
	t.Run("duplicate headings", func(t *testing.T) {
		got := previewSections([]byte("## Data\n\nfirst\n\n## Data\n\nsecond\n"))
		if heads(got) != req+"|Data" || got[3].Body != "first" || got[7].Body != "second" {
			t.Errorf("%+v", got)
		}
	})
	t.Run("text before first heading", func(t *testing.T) {
		got := previewSections([]byte("---\ntitle: x\n---\n\nstray words\n\n## Overview\n\no\n"))
		if heads(got) != req+"|"+preambleHeading || got[7].Body != "stray words" || got[0].Body != "o" {
			t.Errorf("%+v", got)
		}
	})
	t.Run("hash hash in fence", func(t *testing.T) {
		got := previewSections([]byte("## Overview\n\n```\n## Features\n```\n"))
		if heads(got) != req || got[0].Body != "```\n## Features\n```" || got[1].Body != "" {
			t.Errorf("%+v", got)
		}
	})
}

func TestRunPreviewTokenMatchStartsRun(t *testing.T) {
	e, _ := libEnv(t)
	p := getPreviewTok(t, e, "v1")
	if len(p) != 64 {
		t.Fatalf("token %q", p)
	}
	w := runPost(e, "v1", `{"ask":false,"preview_token":"`+p+`"}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("run = %d %s", w.Code, w.Body)
	}
}

func TestRunPreviewTokenStale(t *testing.T) {
	e, m := libEnv(t)
	tok := getPreviewTok(t, e, "v1")
	if err := m.lib.Update("csvstat", "v1", append(e.versionBody(), []byte("\n- more\n")...)); err != nil {
		t.Fatal(err)
	}
	before := runDirCount(t, e)
	w := runPost(e, "v1", `{"preview_token":"`+tok+`"}`)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "the brief changed since you previewed it, reload the preview") {
		t.Fatalf("run = %d %s", w.Code, w.Body)
	}
	if runDirCount(t, e) != before || getPreview(t, e, "v1").Locked {
		t.Error("stale token started or locked")
	}
}

func getPreviewTok(t *testing.T, e *briefEnv, v string) string {
	t.Helper()
	var j struct {
		Token string `json:"preview_token"`
	}
	w := e.get("/app/api/library/csvstat/" + v + "/preview")
	_ = json.Unmarshal(w.Body.Bytes(), &j)
	return j.Token
}

// TestRunRequiresPreviewToken: a direct POST without the preview's token
// cannot skip the preview. Nothing starts and the version stays unlocked.
func TestRunRequiresPreviewToken(t *testing.T) {
	e, _ := libEnv(t)
	before := runDirCount(t, e)
	for _, body := range []string{``, `{}`, `{"ask":false}`, `{"preview_token":""}`} {
		w := runPost(e, "v1", body)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "preview_token is required") {
			t.Errorf("body %q: run = %d %s", body, w.Code, w.Body)
		}
	}
	if runDirCount(t, e) != before || getPreview(t, e, "v1").Locked {
		t.Error("a run without a token started or locked")
	}
}

// TestRunPreviewTokenCheckedInsideLock: an in-place edit that lands after the
// handler read the version but before the family lock is caught by the token
// check inside the lock.
func TestRunPreviewTokenCheckedInsideLock(t *testing.T) {
	e, m := libEnv(t)
	tok := getPreviewTok(t, e, "v1")
	edited := append(e.versionBody(), []byte("\n- more\n")...)
	m.beforeStartLock = func() {
		if err := m.lib.Update("csvstat", "v1", edited); err != nil {
			t.Error(err)
		}
	}
	before := runDirCount(t, e)
	// Call m's handler directly: the hook lives on m, not on the mux's manager.
	r := httptest.NewRequest(http.MethodPost, "/app/api/library/csvstat/v1/run", strings.NewReader(`{"ask":false,"preview_token":"`+tok+`"}`))
	r.SetPathValue("family", "csvstat")
	r.SetPathValue("version", "v1")
	w := httptest.NewRecorder()
	m.libraryRun(w, r)
	m.beforeStartLock = nil
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "the brief changed since you previewed it, reload the preview") {
		t.Fatalf("run = %d %s", w.Code, w.Body)
	}
	if runDirCount(t, e) != before || getPreview(t, e, "v1").Locked {
		t.Error("an edit between token and start started or locked")
	}
}
