package serve

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gophermind/gophermind-lib/build/brief"
	"gophermind/gophermind-lib/library"
	"gophermind/gophermind-lib/runview"
)

// stampedLine matches one id: or repo: frontmatter line with its line end.
var stampedLine = regexp.MustCompile(`(?m)^(?:id|repo):.*\r?\n?`)

// stampFront removes any id and repo lines from the frontmatter of src and
// puts the given ones right after the opening --- line. The stored version
// carries neither; the run copy needs both.
func stampFront(src []byte, id, repo string) []byte {
	nl := bytes.IndexByte(src, '\n')
	if nl < 0 || strings.TrimSpace(string(src[:nl])) != "---" {
		return src
	}
	end := bytes.Index(src[nl:], []byte("\n---"))
	if end < 0 {
		return src
	}
	head, rest := src[nl+1:nl+end+1], src[nl+end+1:]
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
func (m *briefManager) startVersion(family, version string, ask bool) (briefRecord, []brief.Problem, error) {
	var rec briefRecord
	var warnings []brief.Problem
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
		stamped := stampFront(text, id, m.root)
		b, err := brief.Parse(stamped)
		if err != nil {
			return &briefInvalidError{"invalid brief: " + err.Error(), nil}
		}
		rec, err = m.startRec(b, stamped, briefRecord{Ask: ask, Family: family, Version: version})
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
