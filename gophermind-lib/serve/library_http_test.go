//go:build !windows

package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

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
	if _, err := m.lib.Save("csvstat", "v1", "", e.versionBody()); err != nil {
		t.Fatal(err)
	}
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
	if err := m.lib.Lock("csvstat", "v1"); err != nil {
		t.Fatal(err)
	}
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
	if err := m.lib.Lock("csvstat", "v1"); err != nil {
		t.Fatal(err)
	}
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
	if err := m.lib.Update("csvstat", "v1", []byte("a\nb\nc\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.lib.Save("csvstat", "v1", "", []byte("a\nB\nc\nd\n")); err != nil {
		t.Fatal(err)
	}
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
	for _, bad := range []string{"/app/briefs'", "/app/briefs\"", "'/run", `method:"POST"`} {
		if strings.Contains(s, bad) {
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
		{"GET", "/app/library/csvstat/v1"},
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

func TestDiffNearIdenticalLargeIsFast(t *testing.T) {
	e, m := libEnv(t)
	var lines []string
	for i := 0; i < 5000; i++ {
		lines = append(lines, "line "+strings.Repeat("x", i%7)+string(rune('a'+i%26)))
	}
	old := strings.Join(lines, "\n") + "\n"
	lines[2500] = "CHANGED"
	if err := m.lib.Update("csvstat", "v1", []byte(old)); err != nil {
		t.Fatal(err)
	}
	if _, err := m.lib.Save("csvstat", "v1", "", []byte(strings.Join(lines, "\n")+"\n")); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	w := e.get("/app/api/library/csvstat/v1.1/diff?against=v1")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "+ CHANGED\n") || time.Since(start) > 2*time.Second {
		t.Fatalf("%d in %v", w.Code, time.Since(start))
	}
}

func TestDiffFullyDifferentOverCapRefused(t *testing.T) {
	e, m := libEnv(t)
	mk := func(p string) []byte {
		var sb strings.Builder
		for i := 0; i < 3000; i++ {
			sb.WriteString(p + strconv.Itoa(i) + "\n")
		}
		return []byte(sb.String())
	}
	if err := m.lib.Update("csvstat", "v1", mk("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.lib.Save("csvstat", "v1", "", mk("b")); err != nil {
		t.Fatal(err)
	}
	w := e.get("/app/api/library/csvstat/v1.1/diff?against=v1")
	if w.Code != http.StatusRequestEntityTooLarge || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("%d %q", w.Code, w.Header().Get("Content-Type"))
	}
}

func TestRunRecordWithoutLockFlagShowsLocked(t *testing.T) {
	e, m := libEnv(t)
	if _, err := m.lib.Save("csvstat", "v1", "", e.versionBody()); err != nil {
		t.Fatal(err)
	}
	if err := m.lib.Lock("csvstat", "v1.1"); err != nil {
		t.Fatal(err)
	}
	writeRun(t, e, "csvstat-v1-20261101-r001", "csvstat", "v1", "failed", "2026-11-01T10:00:00Z")
	var got struct{ Locked bool }
	w := e.get("/app/api/library/csvstat/v1")
	if json.Unmarshal(w.Body.Bytes(), &got) != nil || !got.Locked {
		t.Errorf("get: %s", w.Body)
	}
	var list struct {
		Families []struct {
			Versions []struct {
				Name, State string
				Locked      bool
			}
		}
	}
	json.Unmarshal(e.get("/app/api/library").Body.Bytes(), &list)
	v := list.Families[0].Versions
	if !v[0].Locked || v[0].State != "failed" {
		t.Errorf("v1 %+v", v[0])
	}
	if !v[1].Locked || v[1].State != "locked" {
		t.Errorf("locked without runs: %+v", v[1])
	}
}

// TestEditorKeepsEditsOnLockedAndConflict: a locked version stays editable (only
// the in-place button hides; "Save as new version" saves the edited text as a
// child), and a 409 from an in-place save reloads only the metadata, never
// overwriting what the user typed.
func TestEditorKeepsEditsOnLockedAndConflict(t *testing.T) {
	page, err := pagesAssets.ReadFile("assets/library_edit.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(page)
	if strings.Contains(s, "readOnly") || strings.Contains(s, "readonly") {
		t.Error("the editor makes the textarea read-only")
	}
	if !strings.Contains(s, "This version is locked, so it cannot change. Saving makes a new version.") {
		t.Error("locked notice missing")
	}
	if !strings.Contains(s, "getElementById('savein').hidden = j.locked") {
		t.Error("in-place button not hidden for a locked version")
	}
	i := strings.Index(s, "r.status === 409")
	if i < 0 {
		t.Fatal("no 409 branch")
	}
	branch := s[i:]
	if j := strings.Index(branch, "} else if"); j > 0 {
		branch = branch[:j]
	}
	if strings.Contains(branch, "text.value =") || strings.Contains(branch, "load()") || !strings.Contains(branch, "load(true)") {
		t.Errorf("409 branch may overwrite the edits:\n%s", branch)
	}
	// The one assignment from the server response is skipped when keeping text.
	if n := strings.Count(s, "text.value ="); n != 1 || !strings.Contains(s, "if (!keepText) text.value = j.text;") {
		t.Errorf("text.value assignments: %d, unguarded", n)
	}
}

// TestSaveLabelCapped: a label over 200 characters is refused with a clear
// message and no version is made; exactly 200 (multi-byte included) is fine.
func TestSaveLabelCapped(t *testing.T) {
	e, m := libEnv(t)
	text := string(e.versionBody()) + "\n- changed\n"
	w := e.post("/app/api/library/csvstat/v1/save", saveBody(text, strings.Repeat("x", 201), false), sameOrigin)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "label is longer than 200 characters") {
		t.Fatalf("201-char label = %d %s", w.Code, w.Body)
	}
	if fams, _ := m.lib.List(); len(fams) != 1 || len(fams[0].Versions) != 1 {
		t.Errorf("a version was made: %+v", fams)
	}
	if w := e.post("/app/api/library/csvstat/v1/save", saveBody(text, strings.Repeat("é", 200), false), sameOrigin); w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Errorf("200-char label = %d %s", w.Code, w.Body)
	}
}
