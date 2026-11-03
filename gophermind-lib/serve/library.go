package serve

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gophermind/gophermind-lib/build/brief"
	"gophermind/gophermind-lib/library"
	"gophermind/gophermind-lib/runview"
)

// stampedLine matches one id: or repo: frontmatter line with its line end.
var stampedLine = regexp.MustCompile(`(?m)^(?:id|repo):.*\r?\n?`)

// frontEnd finds the frontmatter of src the way build/brief does: the first
// line is exactly --- (a trailing CR aside) and the block ends at the next
// such line. head is src[headStart:headEnd]; headEnd is where that closing
// line begins.
func frontEnd(src []byte) (headStart, headEnd int, ok bool) {
	isDash := func(l []byte) bool { return string(bytes.TrimSuffix(l, []byte("\r"))) == "---" }
	nl := bytes.IndexByte(src, '\n')
	if nl < 0 || !isDash(src[:nl]) {
		return 0, 0, false
	}
	pos := nl + 1
	for pos <= len(src) {
		eol := bytes.IndexByte(src[pos:], '\n')
		line := src[pos:]
		if eol >= 0 {
			line = src[pos : pos+eol]
		}
		if isDash(line) {
			return nl + 1, pos, true
		}
		if eol < 0 {
			break
		}
		pos += eol + 1
	}
	return 0, 0, false
}

// stampFront removes any id and repo lines from the frontmatter of src and
// puts the given ones right after the opening --- line. The stored version
// carries neither; the run copy needs both.
func stampFront(src []byte, id, repo string) []byte {
	hs, he, ok := frontEnd(src)
	if !ok {
		return src
	}
	nl := hs - 1
	head, rest := src[hs:he], src[he:]
	head = stampedLine.ReplaceAll(head, nil)
	eol := "\n"
	if nl > 0 && src[nl-1] == '\r' {
		eol = "\r\n"
	}
	var out bytes.Buffer
	out.Write(src[:nl+1])
	fmt.Fprintf(&out, "id: %s%srepo: %s%s", id, eol, repo, eol)
	out.Write(head)
	out.Write(rest)
	return out.Bytes()
}

// nextRunCounter is one more than the highest rNNN among run folders named
// <family>-<version>-<day>-rNNN.
func (m *briefManager) nextRunCounter(family, version, day string) (int, error) {
	ents, err := os.ReadDir(m.dir)
	if err != nil {
		return 0, err
	}
	prefix := family + "-" + version + "-" + day + "-r"
	max := 0
	for _, e := range ents {
		rest, ok := strings.CutPrefix(e.Name(), prefix)
		if !ok {
			continue
		}
		if n, err := strconv.Atoi(rest); err == nil && n > max {
			max = n
		}
	}
	return max + 1, nil
}

// startVersion starts a run from a library version. The run id, the repo line
// and the lock all happen under the family lock, so a version is locked only
// when its run folder exists. Lock order: family lock, then m.mu (inside start).
//
// wantSum is the preview token (previewToken of the stored text) the preview
// page showed, or "" for callers with their own flow (chat, upload, resubmit).
// When set, the stored text must still hash to it (else errPreviewChanged) and
// the stamped text must pass brief.Check (else the problems), both checked
// under the family lock so an edit cannot slip in between check and start.
func (m *briefManager) startVersion(family, version string, ask, verbose bool, wantSum string) (briefRecord, []brief.Problem, error) {
	var rec briefRecord
	var warnings []brief.Problem
	if m.beforeStartLock != nil {
		m.beforeStartLock()
	}
	err := m.lib.LockForRun(family, version, func(v library.Version, text []byte) error {
		day := m.now().UTC()
		n, err := m.nextRunCounter(family, version, day.Format("20060102"))
		if err != nil {
			return err
		}
		id := library.RunName(family, version, day, n)
		if !runview.ValidRunID(id) {
			return &briefInvalidError{"run id " + id + " is not a valid run id (family and version names are too long)", nil}
		}
		if wantSum != "" && previewToken(text) != wantSum {
			return errPreviewChanged
		}
		stamped := stampFront(text, id, m.root)
		if wantSum != "" {
			if probs := brief.Check(stamped); len(probs) > 0 {
				return invalidFromProblems(probs)
			}
		}
		b, err := brief.Parse(stamped)
		if err != nil {
			return &briefInvalidError{"invalid brief: " + err.Error(), nil}
		}
		// Backstop: the run must carry the id the server assigned and, with no
		// per-run repos, the server's root, whatever the stored text says.
		if b.Front.ID != id {
			return &briefInvalidError{"invalid brief: the stored version cannot be stamped with a run id", nil}
		}
		if m.repos == "" && (!filepath.IsAbs(b.Front.Repo) || !sameDir(b.Front.Repo, m.root)) {
			return &briefInvalidError{"invalid brief: the stored version cannot be stamped with the server's repo", nil}
		}
		rec, err = m.startRec(b, stamped, briefRecord{Ask: ask, Verbose: verbose, Family: family, Version: version})
		if errors.Is(err, errBriefExists) {
			rec.ID = id
		}
		if err != nil {
			return err
		}
		warnings = brief.Lint(stamped)
		return nil
	})
	if err != nil && rec.Phase != "" {
		// The run launched but the lock was not written: not silent.
		m.log.Error("run started but version not locked", "run_id", rec.ID, "family", family, "version", version, "error", err.Error())
		err = nil
	}
	return rec, warnings, err
}

// stripFront removes any id and repo lines from the frontmatter of src.
func stripFront(src []byte) []byte {
	hs, he, ok := frontEnd(src)
	if !ok {
		return src
	}
	head := stampedLine.ReplaceAll(src[hs:he], nil)
	var out bytes.Buffer
	out.Write(src[:hs])
	out.Write(head)
	out.Write(src[he:])
	return out.Bytes()
}

// StripStamp removes any id and repo lines from the frontmatter of src, the
// way the server does before it stores a library version.
func StripStamp(src []byte) []byte { return stripFront(src) }

// CheckRun loads run id the way the brief routes do: its <briefsDir>/<id>/
// run.json record must read back with that id and, when the run's own repo
// under reposDir holds a run folder, runview must open it and the state must
// build. `gophermind brief migrate-library` uses it on every renamed run.
func CheckRun(briefsDir, reposDir, id string) error {
	m := &briefManager{dir: briefsDir, repos: reposDir, root: briefsDir,
		now: func() time.Time { return time.Now().UTC() }, log: slog.Default()}
	if _, ok := m.readRecord(id); !ok {
		return fmt.Errorf("%s: run.json does not load with this id", id)
	}
	if reposDir != "" {
		root := filepath.Join(reposDir, id)
		if fi, err := os.Lstat(filepath.Join(root, ".gophermind", id)); err == nil && fi.IsDir() {
			if _, err := runview.Open(root, id); err != nil {
				return fmt.Errorf("%s: run folder does not open: %w", id, err)
			}
		}
	}
	if _, err := m.buildState(id); err != nil {
		return fmt.Errorf("%s: state does not build: %w", id, err)
	}
	return nil
}
