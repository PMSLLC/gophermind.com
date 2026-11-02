//go:build !windows

package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/library"
)

const (
	aliasOld = "gm-2026-10-29-001"
	aliasNew = "csvstat-v1-20261029-r001"
)

func writeAliases(t *testing.T, briefsDir string, a library.Aliases) {
	t.Helper()
	dir := filepath.Join(briefsDir, "library")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := library.SaveAliases(dir, a); err != nil {
		t.Fatal(err)
	}
}

func TestAliasResolvedByRoutes(t *testing.T) {
	e := newBriefEnv(t)
	writeRunRecord(t, e.dir, briefRecord{ID: aliasNew, Title: "csvstat", Phase: phaseDone, StartedAt: "2026-10-29T10:00:00Z", Family: "csvstat", Version: "v1"}, "the log\n")

	// Before any alias exists the old id is unknown, exactly as before.
	if w := e.get("/app/briefs/" + aliasOld + "/state"); w.Code != http.StatusNotFound {
		t.Fatalf("old id before aliases = %d", w.Code)
	}
	writeAliases(t, e.dir, library.Aliases{aliasOld: aliasNew})
	w := e.get("/app/briefs/" + aliasOld + "/state")
	if w.Code != http.StatusOK {
		t.Fatalf("old id state = %d %s", w.Code, w.Body)
	}
	var st BriefState
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Run.ID != aliasNew || st.Run.Title != "csvstat" {
		t.Errorf("run %+v", st.Run)
	}
	if w := e.get("/app/briefs/gm-2026-10-29-777/state"); w.Code != http.StatusNotFound {
		t.Errorf("unknown old id = %d", w.Code)
	}
	if w := e.post("/app/briefs/"+aliasOld+"/stop", "", nil); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "not active") {
		t.Errorf("stop of an old id = %d %s", w.Code, w.Body)
	}

	bm, err := newBriefManager(Options{BriefsDir: e.dir}, e.root)
	if err != nil {
		t.Fatal(err)
	}
	c := &chatTools{bm: bm}
	raw, _ := json.Marshal(map[string]string{"id": aliasOld})
	out, err := findTool(t, c, "brief_state").Run(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "id: "+aliasNew) {
		t.Errorf("brief_state of the old id:\n%s", out)
	}
	if _, err := findTool(t, c, "brief_artifact").Run(context.Background(), raw); err != nil {
		t.Errorf("brief_artifact of the old id: %v", err)
	}
}

func TestAliasReloadedWhenFileChanges(t *testing.T) {
	c, dir := chatToolsEnv(t)
	writeRunRecord(t, dir, briefRecord{ID: aliasNew, Title: "csvstat", Phase: phaseDone}, "")
	writeRunRecord(t, dir, briefRecord{ID: "csvstat-v1-20261029-r002", Title: "csvstat", Phase: phaseDone}, "")
	writeAliases(t, dir, library.Aliases{aliasOld: aliasNew})
	if got := c.bm.resolveID(aliasOld); got != aliasNew {
		t.Fatalf("resolve = %q", got)
	}
	if got := c.bm.resolveID("gm-2026-10-29-002"); got != "gm-2026-10-29-002" {
		t.Fatalf("unknown resolved to %q", got)
	}
	writeAliases(t, dir, library.Aliases{aliasOld: aliasNew, "gm-2026-10-29-002": "csvstat-v1-20261029-r002"})
	// Make sure the mtime moves even on a coarse clock.
	later := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(filepath.Join(dir, "library", library.AliasesFile), later, later)
	if got := c.bm.resolveID("gm-2026-10-29-002"); got != "csvstat-v1-20261029-r002" {
		t.Errorf("after reload resolve = %q", got)
	}
	// An id that is itself a run is never redirected, and a bad id stays bad.
	if got := c.bm.resolveID(aliasNew); got != aliasNew {
		t.Errorf("new id resolved to %q", got)
	}
	if got := c.bm.resolveID("../x"); got != "../x" {
		t.Errorf("bad id resolved to %q", got)
	}
}

// Every route and tool that takes a run id resolves an old one: removing
// resolveID from any of them fails here.
func TestAliasResolvedByEveryRoute(t *testing.T) {
	e := newBriefEnv(t)
	writeRunRecord(t, e.dir, briefRecord{ID: aliasNew, Title: "csvstat", Phase: phaseDone, StartedAt: "2026-10-29T10:00:00Z"}, "the log\n")
	if err := os.WriteFile(filepath.Join(e.dir, aliasNew, "brief.md"), []byte(e.briefText(aliasNew)), 0o644); err != nil {
		t.Fatal(err)
	}
	writeAliases(t, e.dir, library.Aliases{aliasOld: aliasNew})

	// events: the first state event names the new run, then the stream ends.
	w := e.get("/app/briefs/" + aliasOld + "/events")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"id":"`+aliasNew+`"`) || !strings.Contains(w.Body.String(), "event: end") {
		t.Errorf("events of the old id = %d %s", w.Code, w.Body)
	}
	// answer: an unresolved id is 404 no such run; the resolved run is done, so 409.
	w = e.post("/app/briefs/"+aliasOld+"/answer", `{"question_id":"clarify-q1","answer":"yes"}`, nil)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "not waiting") {
		t.Errorf("answer to the old id = %d %s", w.Code, w.Body)
	}
	// resubmit: an unresolved id has no stored brief (404); the resolved run's brief starts.
	w = e.post("/app/briefs/"+aliasOld+"/resubmit", "", nil)
	if w.Code != http.StatusAccepted {
		t.Fatalf("resubmit of the old id = %d %s", w.Code, w.Body)
	}
	var started struct {
		RunID string `json:"run_id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &started)
	if started.RunID == "" || started.RunID == aliasNew {
		t.Fatalf("resubmit started %q: %s", started.RunID, w.Body)
	}
	e.waitPhase(started.RunID, phaseDone)

	bm, err := newBriefManager(Options{BriefsDir: e.dir}, e.root)
	if err != nil {
		t.Fatal(err)
	}
	c := &chatTools{bm: bm}
	raw, _ := json.Marshal(map[string]string{"id": aliasOld, "question_id": "clarify-q1", "text": "yes"})
	out, err := findTool(t, c, "brief_answer").Run(context.Background(), raw)
	if err != nil || !strings.Contains(out, "not waiting") {
		t.Errorf("brief_answer of the old id: %q %v", out, err)
	}
	// brief_stop: make the new id the active run (no process, so no signal).
	bm.mu.Lock()
	bm.active = &activeBrief{id: aliasNew, done: make(chan struct{})}
	bm.mu.Unlock()
	raw, _ = json.Marshal(map[string]string{"id": aliasOld})
	out, err = findTool(t, c, "brief_stop").Run(context.Background(), raw)
	if err != nil || !strings.Contains(out, "stop requested for run "+aliasNew) {
		t.Errorf("brief_stop of the old id: %q %v", out, err)
	}
}

// TestFreshIDSkipsAliasedOldIDs: after the library migration renamed
// gm-<day>-001 and -002, a harness run started the same day must not get either
// id back, or the alias would point the old id at a different run.
func TestFreshIDSkipsAliasedOldIDs(t *testing.T) {
	dir := t.TempDir()
	m, err := newBriefManager(Options{BriefsDir: dir}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.now = func() time.Time { return time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC) }
	writeAliases(t, dir, library.Aliases{aliasOld: aliasNew, "gm-2026-10-29-002": "csvstat-v1-20261029-r002"})
	id, err := m.freshID()
	if err != nil {
		t.Fatal(err)
	}
	if id != "gm-2026-10-29-003" {
		t.Errorf("freshID = %q, want gm-2026-10-29-003", id)
	}
}
