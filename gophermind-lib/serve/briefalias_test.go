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
