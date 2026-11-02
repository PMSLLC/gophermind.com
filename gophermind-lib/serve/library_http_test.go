//go:build !windows

package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/library"
)

var sameOrigin = map[string]string{"Origin": "https://gophermind.example", "Sec-Fetch-Site": "same-origin"}

func saveBody(text, label string, inPlace bool) string {
	b, _ := json.Marshal(map[string]any{"text": text, "label": label, "in_place": inPlace})
	return string(b)
}

func writeRun(t *testing.T, e *briefEnv, id, family, version, phase, started string) {
	t.Helper()
	dir := filepath.Join(e.dir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(briefRecord{ID: id, Phase: phase, StartedAt: started, Family: family, Version: version})
	if err := os.WriteFile(filepath.Join(dir, "run.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLibraryListJSON(t *testing.T) {
	e, m := libEnv(t)
	if _, err := m.lib.Save("csvstat", "v1", "second", e.versionBody()); err != nil {
		t.Fatal(err)
	}
	if err := m.lib.Lock("csvstat", "v1"); err != nil {
		t.Fatal(err)
	}
	writeRun(t, e, "csvstat-v1-20261101-r001", "csvstat", "v1", "failed", "2026-11-01T10:00:00Z")
	writeRun(t, e, "csvstat-v1-20261101-r002", "csvstat", "v1", "done", "2026-11-01T11:00:00Z")
	w := e.get("/app/api/library")
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var got struct {
		Families []struct {
			Name         string `json:"name"`
			VersionCount int    `json:"version_count"`
			LastRunAt    string `json:"last_run_at"`
			Versions     []struct {
				Name, Parent, Label, State string
				Locked                     bool
				RunCount                   int `json:"run_count"`
			} `json:"versions"`
		} `json:"families"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Families) != 1 {
		t.Fatalf("%+v", got)
	}
	f := got.Families[0]
	if f.Name != "csvstat" || f.VersionCount != 2 || f.LastRunAt != "2026-11-01T11:00:00Z" || len(f.Versions) != 2 {
		t.Fatalf("family %+v", f)
	}
	v1, v2 := f.Versions[0], f.Versions[1]
	if v1.Name != "v1" || !v1.Locked || v1.State != "done" || v1.RunCount != 2 || v1.Label != "first" {
		t.Errorf("v1 %+v", v1)
	}
	if v2.Name != "v1.1" || v2.Parent != "v1" || v2.Locked || v2.State != "draft" || v2.RunCount != 0 {
		t.Errorf("v2 %+v", v2)
	}
}

func TestLibraryGetJSON(t *testing.T) {
	e, m := libEnv(t)
	m.lib.Save("csvstat", "v1", "", e.versionBody())
	w := e.get("/app/api/library/csvstat/v1")
	var got struct {
		Family, Name, Text string
		Children           []string
		Runs               []libRun
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Name != "v1" || len(got.Children) != 1 || got.Text == "" || got.Runs == nil {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	for _, p := range []string{"/app/api/library/csvstat/v9", "/app/api/library/nope/v1", "/app/api/library/csvstat/..%2f..", "/app/api/library/Bad_Fam/v1"} {
		if w := e.get(p); w.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, w.Code)
		}
	}
}

func TestSaveLockedMakesChild(t *testing.T) {
	e, m := libEnv(t)
	m.lib.Lock("csvstat", "v1")
	_, before, _ := m.lib.Get("csvstat", "v1")
	w := e.post("/app/api/library/csvstat/v1/save", saveBody(string(e.versionBody())+"\nmore\n", "tweak", false), sameOrigin)
	if w.Code != http.StatusCreated {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var got struct {
		Name   string
		Locked bool
	}
	json.Unmarshal(w.Body.Bytes(), &got)
	if got.Name != "v1.1" || got.Locked {
		t.Fatalf("%+v", got)
	}
	v, text, err := m.lib.Get("csvstat", "v1.1")
	if err != nil || v.Parent != "v1" || v.Label != "tweak" || !strings.HasSuffix(string(text), "more\n") {
		t.Fatalf("%+v %v", v, err)
	}
	_, after, _ := m.lib.Get("csvstat", "v1")
	if string(after) != string(before) {
		t.Error("the parent changed")
	}
}

func TestSaveDraftInPlace(t *testing.T) {
	e, m := libEnv(t)
	w := e.post("/app/api/library/csvstat/v1/save", saveBody("changed\n", "", true), sameOrigin)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	_, text, _ := m.lib.Get("csvstat", "v1")
	if string(text) != "changed\n" {
		t.Errorf("text %q", text)
	}
	fams, _ := m.lib.List()
	if len(fams[0].Versions) != 1 {
		t.Error("in_place made a new version")
	}
}

func TestSaveInPlaceRefusedWhenItHasRuns(t *testing.T) {
	e, m := libEnv(t)
	writeRun(t, e, "csvstat-v1-20261101-r001", "csvstat", "v1", "failed", "2026-11-01T10:00:00Z")
	_, before, _ := m.lib.Get("csvstat", "v1")
	w := e.post("/app/api/library/csvstat/v1/save", saveBody("changed\n", "", true), sameOrigin)
	if w.Code != http.StatusConflict {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	_, after, _ := m.lib.Get("csvstat", "v1")
	if string(after) != string(before) {
		t.Error("bytes changed")
	}
}

func TestSaveStaleLockRefused(t *testing.T) {
	e, m := libEnv(t)
	// The editor loaded a draft; before it saved, the version was locked.
	if w := e.get("/app/api/library/csvstat/v1"); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if err := m.lib.Lock("csvstat", "v1"); err != nil {
		t.Fatal(err)
	}
	_, before, _ := m.lib.Get("csvstat", "v1")
	w := e.post("/app/api/library/csvstat/v1/save", saveBody("late edit\n", "", true), sameOrigin)
	if w.Code != http.StatusConflict {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var got map[string]any
	json.Unmarshal(w.Body.Bytes(), &got)
	if got["locked"] != true || !strings.Contains(got["error"].(string), "locked") || got["hint"] != "Save as new version" {
		t.Errorf("body %v", got)
	}
	_, after, _ := m.lib.Get("csvstat", "v1")
	if string(after) != string(before) {
		t.Error("a locked version changed")
	}
}

func TestSaveRefusesIDRepoLines(t *testing.T) {
	e, m := libEnv(t)
	pasted := "\xef\xbb\xbf---\nid: gm-2026-01-01-001\nrepo: /etc\ntitle: x\n---\nbody\n"
	if w := e.post("/app/api/library/csvstat/v1/save", saveBody(pasted, "", true), sameOrigin); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	_, text, _ := m.lib.Get("csvstat", "v1")
	if strings.Contains(string(text), "id:") || strings.Contains(string(text), "repo:") || strings.HasPrefix(string(text), "\xef\xbb\xbf") {
		t.Errorf("stored %q", text)
	}
	m.lib.Lock("csvstat", "v1")
	if w := e.post("/app/api/library/csvstat/v1/save", saveBody(pasted, "", false), sameOrigin); w.Code != http.StatusCreated {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	_, text, _ = m.lib.Get("csvstat", "v1.1")
	if strings.Contains(string(text), "repo:") {
		t.Errorf("child stored %q", text)
	}
}

func TestSaveLimitsAndUnknowns(t *testing.T) {
	e, _ := libEnv(t)
	big := strings.Repeat("x", library.MaxTextBytes+1)
	if w := e.post("/app/api/library/csvstat/v1/save", saveBody(big, "", true), sameOrigin); w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("big = %d", w.Code)
	}
	if w := e.post("/app/api/library/csvstat/v9/save", saveBody("x", "", false), sameOrigin); w.Code != http.StatusNotFound {
		t.Errorf("unknown = %d", w.Code)
	}
	if w := e.post("/app/api/library/csvstat/v1/save", "not json", sameOrigin); w.Code != http.StatusBadRequest {
		t.Errorf("bad json = %d", w.Code)
	}
}

func TestDiffPlain(t *testing.T) {
	e, m := libEnv(t)
	m.lib.Update("csvstat", "v1", []byte("a\nb\nc\n"))
	m.lib.Save("csvstat", "v1", "", []byte("a\nB\nc\nd\n"))
	w := e.get("/app/api/library/csvstat/v1.1/diff?against=v1")
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("%d %q", w.Code, w.Header().Get("Content-Type"))
	}
	want := "--- csvstat v1\n+++ csvstat v1.1\n  a\n- b\n+ B\n  c\n+ d\n"
	if w.Body.String() != want {
		t.Errorf("got\n%s\nwant\n%s", w.Body, want)
	}
	if w := e.get("/app/api/library/csvstat/v1.1/diff?against=v7"); w.Code != 404 {
		t.Errorf("unknown against = %d", w.Code)
	}
}

func TestLibraryPagesServed(t *testing.T) {
	e, _ := libEnv(t)
	for _, p := range []string{"/app/library", "/app/library/csvstat/v1"} {
		w := e.get(p)
		body := w.Body.String()
		if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("%s = %d", p, w.Code)
		}
		if !strings.Contains(body, "Content-Security-Policy") || !strings.Contains(body, "default-src 'none'") {
			t.Errorf("%s lacks a CSP", p)
		}
		for _, bad := range []string{"http://", "https://", "<link", "src=", "innerHTML"} {
			if strings.Contains(body, bad) {
				t.Errorf("%s contains %q", p, bad)
			}
		}
		if !strings.Contains(body, `href="/app/library"`) {
			t.Errorf("%s: no Library in the rail", p)
		}
	}
}

func TestEditorRunButtonOnlyLinksToPreview(t *testing.T) {
	page, err := pagesAssets.ReadFile("assets/library_edit.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(page)
	if !strings.Contains(s, "/preview") {
		t.Error("no link to the preview page")
	}
	if strings.Contains(s, "<form") {
		t.Error("the editor has a form")
	}
	for _, bad := range []string{"/app/briefs'", "/app/briefs\"", "'/run", "method: 'POST'", `method:"POST"`} {
		if i := strings.Index(s, bad); i >= 0 && bad != "method: 'POST'" {
			t.Errorf("editor references %q", bad)
		}
	}
	// The only POST in the page is the save call.
	if n := strings.Count(s, "'POST'"); n != 1 || !strings.Contains(s, "/save'") {
		t.Errorf("POST count %d", n)
	}
	if strings.Count(s, "fetch(") > 4 {
		t.Error("unexpected number of fetches")
	}
}

func TestLibraryRoutesNeedAuth(t *testing.T) {
	e, _ := libEnv(t)
	for _, tc := range []struct{ method, path string }{
		{"GET", "/app/api/library"},
		{"GET", "/app/api/library/csvstat/v1"},
		{"POST", "/app/api/library/csvstat/v1/save"},
		{"GET", "/app/api/library/csvstat/v1/diff?against=v1"},
	} {
		if w := do(e.h, tc.method, tc.path, nil); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", tc.method, tc.path, w.Code)
		}
	}
	w := do(e.h, "GET", "/app/library", map[string]string{"Accept": "text/html"})
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/auth/login" {
		t.Errorf("page = %d %s", w.Code, w.Header().Get("Location"))
	}
	if w := e.post("/app/api/library/csvstat/v1/save", saveBody("x", "", true), map[string]string{"Sec-Fetch-Site": "cross-site"}); w.Code != http.StatusForbidden {
		t.Errorf("cross-site save = %d", w.Code)
	}
}

func TestLibraryHiddenWithoutBriefsDir(t *testing.T) {
	h := pagesMux(t, nil)
	for _, p := range []string{"/app/library", "/app/api/library"} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", p, nil))
		if r.Code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", p, r.Code)
		}
	}
}
