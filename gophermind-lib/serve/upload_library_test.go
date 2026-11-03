//go:build !windows

package serve

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const uploadFamily = "acme-registration-api"

func versionNames(t *testing.T, m *briefManager) []string {
	t.Helper()
	fams, err := m.lib.List()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, f := range fams {
		if f.Name != uploadFamily {
			continue
		}
		for _, v := range f.Versions {
			out = append(out, v.Name)
		}
	}
	return out
}

func TestUploadNewTitleMakesV1(t *testing.T) {
	e := newBriefEnv(t)
	m := e.manager()
	rec, _, err := m.createText([]byte(e.briefText("gm-2026-10-23-001")), false, false)
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	if rec.ID != rid(1) || rec.Family != uploadFamily || rec.Version != "v1" {
		t.Fatalf("rec = %+v, want id %s family %s v1", rec, rid(1), uploadFamily)
	}
	if got := versionNames(t, m); len(got) != 1 || got[0] != "v1" {
		t.Fatalf("versions %v", got)
	}
	_, stored := mustGetFam(t, m, uploadFamily, "v1")
	if bytes.Contains(stored, []byte("\nid:")) || bytes.Contains(stored, []byte("\nrepo:")) {
		t.Fatalf("stored version carries id or repo:\n%s", stored)
	}
}

func mustGetFam(t *testing.T, m *briefManager, family, v string) (any, []byte) {
	t.Helper()
	ver, b, err := m.lib.Get(family, v)
	if err != nil {
		t.Fatal(err)
	}
	return ver, b
}

func TestUploadIdenticalReusesVersion(t *testing.T) {
	e := newBriefEnv(t)
	m := e.manager()
	r1, _, err := m.createText([]byte(e.briefText("gm-2026-10-23-001")), false, false)
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	// A different pasted id and repo must not matter.
	r2, _, err := m.createText([]byte(e.briefText("gm-2026-10-23-777")), false, false)
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	if r1.ID != rid(1) || r2.ID != rid(2) {
		t.Fatalf("ids %s, %s; want %s, %s", r1.ID, r2.ID, rid(1), rid(2))
	}
	if r2.Version != "v1" {
		t.Fatalf("second version %q", r2.Version)
	}
	if got := versionNames(t, m); len(got) != 1 {
		t.Fatalf("versions %v, want one", got)
	}
}

func TestUploadChangedTextMakesChild(t *testing.T) {
	e := newBriefEnv(t)
	m := e.manager()
	first := e.briefText("gm-2026-10-23-001")
	if _, _, err := m.createText([]byte(first), false, false); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	_, v1 := mustGetFam(t, m, uploadFamily, "v1")
	edited := strings.Replace(first, "## Overview", "## Overview\n\nExtra.", 1)
	if edited == first {
		t.Fatal("edit did not change the text")
	}
	rec, _, err := m.createText([]byte(edited), false, false)
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	if rec.Version != "v1.1" {
		t.Fatalf("version %q, want v1.1", rec.Version)
	}
	if want := "acme-registration-api-v1.1-"; !strings.HasPrefix(rec.ID, want) || !strings.HasSuffix(rec.ID, "-r001") {
		t.Fatalf("run id %q", rec.ID)
	}
	_, again := mustGetFam(t, m, uploadFamily, "v1")
	if !bytes.Equal(v1, again) {
		t.Fatal("v1 bytes changed")
	}
}

func TestUploadIgnoresClientID(t *testing.T) {
	e := newBriefEnv(t)
	m := e.manager()
	rec, _, err := m.createText([]byte(e.briefText("gm-2026-10-23-042")), false, false)
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	if strings.HasPrefix(rec.ID, "gm-") || rec.ID != rid(1) {
		t.Fatalf("run id %q came from the client", rec.ID)
	}
	if _, err := os.Stat(filepath.Join(e.dir, "gm-2026-10-23-042")); err == nil {
		t.Fatal("a folder named by the client id exists")
	}
	run, _ := os.ReadFile(filepath.Join(e.dir, rec.ID, "brief.md"))
	if !bytes.Contains(run, []byte("id: "+rec.ID)) || bytes.Contains(run, []byte("gm-2026-10-23-042")) {
		t.Fatalf("run copy not stamped with the server id:\n%s", run)
	}
}

func TestUploadEmptyTitleIsInvalid(t *testing.T) {
	e := newBriefEnv(t)
	m := e.manager()
	src := strings.Replace(e.briefText("gm-2026-10-23-001"), "title: Acme Registration API", "title: '!!!'", 1)
	_, _, err := m.createText([]byte(src), false, false)
	var inv *briefInvalidError
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want *briefInvalidError", err)
	}
}

func TestResubmitNewRunUsesSameVersion(t *testing.T) {
	e := newBriefEnv(t)
	m := e.manager()
	r1, _, err := m.createText([]byte(e.briefText("gm-2026-10-23-001")), true, false)
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	r2, _, err := m.resubmitWarn(r1.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	if r2.ID != rid(2) || r2.Version != "v1" || r2.Family != uploadFamily || !r2.Ask {
		t.Fatalf("rec = %+v", r2)
	}
	if got := versionNames(t, m); len(got) != 1 {
		t.Fatalf("versions %v, want one", got)
	}
}

func TestResubmitOldRunMigratesThroughCreate(t *testing.T) {
	e := newBriefEnv(t)
	m := e.manager()
	old := "gm-2026-10-23-001"
	if err := os.MkdirAll(filepath.Join(e.dir, old), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.dir, old, "brief.md"), []byte(e.briefText(old)), 0o644); err != nil {
		t.Fatal(err)
	}
	rec, _, err := m.resubmitWarn(old)
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	if rec.ID != rid(1) || rec.Version != "v1" || rec.Family != uploadFamily {
		t.Fatalf("rec = %+v", rec)
	}
	if got := versionNames(t, m); len(got) != 1 {
		t.Fatalf("versions %v", got)
	}
}

func TestUploadIgnoresClientIDAndRepo(t *testing.T) {
	e := newBriefEnv(t)
	src := strings.Replace(e.briefText("../etc"), "repo: "+e.root, "repo: /somewhere/else", 1)
	w := e.post("/app/briefs", src, nil)
	if w.Code != 202 || decodeMap(t, w)["run_id"] != rid(1) {
		t.Fatalf("= %d %s", w.Code, w.Body)
	}
	e.waitPhase(rid(1), phaseDone)
	run, _ := os.ReadFile(filepath.Join(e.dir, rid(1), "brief.md"))
	if !bytes.Contains(run, []byte("repo: "+e.root)) || bytes.Contains(run, []byte("/somewhere/else")) {
		t.Fatalf("run copy keeps the client repo:\n%s", run)
	}
}

func TestUploadBOMCannotChooseIDOrRepo(t *testing.T) {
	e := newBriefEnv(t)
	m := e.manager()
	src := strings.Replace(e.briefText("gm-2026-10-23-042"), "repo: "+e.root, "repo: /somewhere/else", 1)
	bom := []byte("\xef\xbb\xbf" + src)
	rec, _, err := m.createText(bom, false, false)
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	if rec.ID != rid(1) {
		t.Fatalf("run id %q came from the client, want %s", rec.ID, rid(1))
	}
	run, _ := os.ReadFile(filepath.Join(e.dir, rec.ID, "brief.md"))
	if !bytes.Contains(run, []byte("repo: "+e.root)) || bytes.Contains(run, []byte("/somewhere/else")) || bytes.Contains(run, []byte("gm-2026-10-23-042")) {
		t.Fatalf("run copy keeps client id or repo:\n%s", run)
	}
	_, stored := mustGetFam(t, m, uploadFamily, "v1")
	if regexpLine(stored, "id") || regexpLine(stored, "repo") {
		t.Fatalf("stored version carries id or repo:\n%s", stored)
	}
	r2, _, err := m.createText(bom, false, false)
	if err != nil {
		t.Fatalf("second identical BOM upload: %v", err)
	}
	waitIdle(t, m)
	if r2.ID != rid(2) || r2.Version != "v1" {
		t.Fatalf("second rec = %+v", r2)
	}
	if got := versionNames(t, m); len(got) != 1 {
		t.Fatalf("versions %v, want one", got)
	}
}

func regexpLine(b []byte, key string) bool {
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, key+":") {
			return true
		}
	}
	return false
}

// A stored version whose frontmatter stampFront cannot rewrite must not start
// a run under the client's id or repo.
func TestStartVersionRefusesWhenParsedIDDiffers(t *testing.T) {
	e := newBriefEnv(t)
	m := e.manager()
	bad := "\xef\xbb\xbf" + strings.Replace(e.briefText("gm-2026-10-23-042"), "repo: "+e.root, "repo: /somewhere/else", 1)
	if _, err := m.lib.Save(uploadFamily, "", "", []byte(bad)); err != nil {
		t.Fatal(err)
	}
	_, _, err := m.startVersion(uploadFamily, "v1", false, false, "")
	var inv *briefInvalidError
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want *briefInvalidError", err)
	}
	v, _, gerr := m.lib.Get(uploadFamily, "v1")
	if gerr != nil || v.Locked {
		t.Fatalf("version %+v err %v: want unlocked", v, gerr)
	}
	ents, _ := os.ReadDir(e.dir)
	for _, en := range ents {
		if en.Name() != "library" {
			t.Fatalf("a run folder %s was made", en.Name())
		}
	}
}

func TestConcurrentUploadsMakeOneVersion(t *testing.T) {
	e := newBriefEnv(t)
	m := e.manager()
	src := []byte(e.briefText("gm-2026-10-23-001"))
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := m.createText(src, false, false)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	started := 0
	for err := range errs {
		var busy *briefBusyError
		switch {
		case err == nil:
			started++
		case errors.As(err, &busy):
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	waitIdle(t, m)
	if started == 0 {
		t.Error("no upload started")
	}
	if got := versionNames(t, m); len(got) != 1 {
		t.Fatalf("versions %v, want one", got)
	}
}

// v1b.1 is last in tree order but v1.2 is the newest: the next upload is a
// child of the newest.
func TestUploadChildOfNewestVersion(t *testing.T) {
	e := newBriefEnv(t)
	m := e.manager()
	base := []byte(e.briefText("gm-2026-10-23-001"))
	text := func(n string) []byte {
		return []byte(strings.Replace(string(base), "## Overview", "## Overview\n\n"+n, 1))
	}
	for _, step := range []struct{ parent, label string }{{"", "a"}, {"v1", "b"}, {"v1", "c"}, {"v1.1", "d"}} {
		if _, err := m.lib.Save(uploadFamily, step.parent, "", text(step.label)); err != nil {
			t.Fatal(err)
		}
	}
	rec, _, err := m.createText(text("e"), false, false)
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	v, _, err := m.lib.Get(uploadFamily, rec.Version)
	if err != nil || v.Parent != "v1.2" {
		t.Fatalf("version %+v err %v, want a child of the newest, v1.1.1", v, err)
	}
}

// A "--- " line is not the end of the frontmatter for the parser, so id and
// repo lines after it are still frontmatter and must not survive.
func TestUploadSecondYAMLDocCannotChooseIDOrRepo(t *testing.T) {
	e := newBriefEnv(t)
	m := e.manager()
	src := strings.Replace(e.briefText("gm-2026-10-23-001"), "\n---\n", "\n--- \nid: gm-evil-001\nrepo: /somewhere/else\n---\n", 1)
	rec, _, err := m.createText([]byte(src), false, false)
	if err != nil {
		t.Logf("refused: %v", err) // refused is acceptable
		return
	}
	waitIdle(t, m)
	if rec.ID != rid(1) {
		t.Fatalf("run id %q came from the client", rec.ID)
	}
	_, stored := mustGetFam(t, m, uploadFamily, rec.Version)
	if bytes.Contains(stored, []byte("gm-evil-001")) || bytes.Contains(stored, []byte("/somewhere/else")) {
		t.Fatalf("stored version keeps client id or repo:\n%s", stored)
	}
	run, _ := os.ReadFile(filepath.Join(e.dir, rec.ID, "brief.md"))
	if bytes.Contains(run, []byte("gm-evil-001")) || bytes.Contains(run, []byte("/somewhere/else")) || !bytes.Contains(run, []byte("repo: "+e.root)) {
		t.Fatalf("run copy wrong:\n%s", run)
	}
}
