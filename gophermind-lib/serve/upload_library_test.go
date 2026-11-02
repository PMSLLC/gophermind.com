//go:build !windows

package serve

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	rec, _, err := m.createText([]byte(e.briefText("gm-2026-10-23-001")), false)
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
	r1, _, err := m.createText([]byte(e.briefText("gm-2026-10-23-001")), false)
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	// A different pasted id and repo must not matter.
	r2, _, err := m.createText([]byte(e.briefText("gm-2026-10-23-777")), false)
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
	if _, _, err := m.createText([]byte(first), false); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	_, v1 := mustGetFam(t, m, uploadFamily, "v1")
	edited := strings.Replace(first, "## Overview", "## Overview\n\nExtra.", 1)
	if edited == first {
		t.Fatal("edit did not change the text")
	}
	rec, _, err := m.createText([]byte(edited), false)
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
	rec, _, err := m.createText([]byte(e.briefText("gm-2026-10-23-042")), false)
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
	_, _, err := m.createText([]byte(src), false)
	var inv *briefInvalidError
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want *briefInvalidError", err)
	}
}

func TestResubmitNewRunUsesSameVersion(t *testing.T) {
	e := newBriefEnv(t)
	m := e.manager()
	r1, _, err := m.createText([]byte(e.briefText("gm-2026-10-23-001")), true)
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
