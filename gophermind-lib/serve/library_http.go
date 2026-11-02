package serve

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"

	"gophermind/gophermind-lib/library"
)

// libRun is one run started from a library version.
type libRun struct {
	ID        string `json:"id"`
	Phase     string `json:"phase"`
	StartedAt string `json:"started_at"`
}

// runsByVersion scans the run records once and groups those started from a
// library version by "family/version", newest first.
func (m *briefManager) runsByVersion() map[string][]libRun {
	out := map[string][]libRun{}
	names, err := readDirNames(m.dir)
	if err != nil {
		return out
	}
	for _, id := range names {
		rec, ok := m.readRecord(id)
		if !ok || rec.Family == "" || rec.Version == "" {
			continue
		}
		k := rec.Family + "/" + rec.Version
		out[k] = append(out[k], libRun{ID: id, Phase: rec.Phase, StartedAt: rec.StartedAt})
	}
	for _, rs := range out {
		sort.Slice(rs, func(i, j int) bool {
			if rs[i].StartedAt != rs[j].StartedAt {
				return rs[i].StartedAt > rs[j].StartedAt
			}
			return rs[i].ID > rs[j].ID
		})
	}
	return out
}

type libVersionItem struct {
	Name     string `json:"name"`
	Parent   string `json:"parent"`
	Label    string `json:"label"`
	Created  string `json:"created"`
	Locked   bool   `json:"locked"`
	State    string `json:"state"`
	RunCount int    `json:"run_count"`
}

type libFamilyItem struct {
	Name         string           `json:"name"`
	VersionCount int              `json:"version_count"`
	LastRunAt    string           `json:"last_run_at"`
	Versions     []libVersionItem `json:"versions"`
}

// libraryList handles GET /app/api/library. A version counts as locked when
// its lock flag is set or any run record names it, which is what the save
// route enforces. state is "draft" when it has no runs and is not locked,
// "locked" when it is locked with no runs, else the phase of its most recent
// run.
func (m *briefManager) libraryList(w http.ResponseWriter, r *http.Request) {
	fams, err := m.lib.List()
	if err != nil {
		briefError(w, http.StatusInternalServerError, "cannot read the library", nil)
		return
	}
	runs := m.runsByVersion()
	out := make([]libFamilyItem, 0, len(fams))
	for _, f := range fams {
		fi := libFamilyItem{Name: f.Name, VersionCount: len(f.Versions), Versions: []libVersionItem{}}
		for _, v := range f.Versions {
			rs := runs[f.Name+"/"+v.Name]
			it := libVersionItem{Name: v.Name, Parent: v.Parent, Label: v.Label, Created: v.Created,
				Locked: v.Locked || len(rs) > 0, State: "draft", RunCount: len(rs)}
			if it.Locked {
				it.State = "locked"
			}
			if len(rs) > 0 {
				it.State = rs[0].Phase
				if rs[0].StartedAt > fi.LastRunAt {
					fi.LastRunAt = rs[0].StartedAt
				}
			}
			fi.Versions = append(fi.Versions, it)
		}
		out = append(out, fi)
	}
	briefJSON(w, http.StatusOK, map[string]any{"families": out})
}

// libraryGet handles GET /app/api/library/{family}/{version}.
func (m *briefManager) libraryGet(w http.ResponseWriter, r *http.Request) {
	family, name := r.PathValue("family"), r.PathValue("version")
	v, text, err := m.lib.Get(family, name)
	if err != nil {
		briefError(w, http.StatusNotFound, "no such version", nil)
		return
	}
	children := []string{}
	if fams, err := m.lib.List(); err == nil {
		for _, f := range fams {
			if f.Name != family {
				continue
			}
			for _, c := range f.Versions {
				if c.Parent == name {
					children = append(children, c.Name)
				}
			}
		}
	}
	rs := m.runsByVersion()[family+"/"+name]
	if rs == nil {
		rs = []libRun{}
	}
	briefJSON(w, http.StatusOK, map[string]any{
		"family": family, "name": v.Name, "parent": v.Parent, "children": children,
		"label": v.Label, "created": v.Created, "locked": v.Locked || len(rs) > 0, "text": string(text), "runs": rs,
	})
}

// libraryLockedReply is the 409 for a save that would change a locked version.
func libraryLockedReply(w http.ResponseWriter) {
	briefError(w, http.StatusConflict, "this version has run and is locked",
		map[string]any{"locked": true, "hint": "Save as new version"})
}

// librarySave handles POST /app/api/library/{family}/{version}/save.
func (m *briefManager) librarySave(w http.ResponseWriter, r *http.Request) {
	family, name := r.PathValue("family"), r.PathValue("version")
	var body struct {
		Text    string `json:"text"`
		Label   string `json:"label"`
		InPlace bool   `json:"in_place"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2*library.MaxTextBytes)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		briefError(w, http.StatusBadRequest, "send JSON with text, label and in_place", nil)
		return
	}
	text := stripFront(bytes.TrimPrefix([]byte(body.Text), []byte("\xef\xbb\xbf")))
	if len(text) > library.MaxTextBytes {
		briefError(w, http.StatusRequestEntityTooLarge, "the brief is too large", nil)
		return
	}
	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	v, _, err := m.lib.Get(family, name)
	if err != nil {
		briefError(w, http.StatusNotFound, "no such version", nil)
		return
	}
	if body.InPlace {
		if v.Locked || len(m.runsByVersion()[family+"/"+name]) > 0 {
			libraryLockedReply(w)
			return
		}
		switch err := m.lib.Update(family, name, text); {
		case errors.Is(err, library.ErrLocked):
			libraryLockedReply(w)
		case errors.Is(err, library.ErrNoSuchVersion):
			briefError(w, http.StatusNotFound, "no such version", nil)
		case errors.Is(err, library.ErrTooLarge):
			briefError(w, http.StatusRequestEntityTooLarge, "the brief is too large", nil)
		case err != nil:
			briefError(w, http.StatusInternalServerError, "cannot save", nil)
		default:
			briefJSON(w, http.StatusOK, map[string]any{"name": v.Name, "locked": false, "created": v.Created})
		}
		return
	}
	nv, err := m.lib.Save(family, name, strings.TrimSpace(body.Label), text)
	switch {
	case errors.Is(err, library.ErrTooLarge):
		briefError(w, http.StatusRequestEntityTooLarge, "the brief is too large", nil)
	case errors.Is(err, library.ErrNoSuchVersion):
		briefError(w, http.StatusNotFound, "no such version", nil)
	case err != nil:
		briefError(w, http.StatusInternalServerError, "cannot save", nil)
	default:
		briefJSON(w, http.StatusCreated, map[string]any{"name": nv.Name, "locked": nv.Locked, "created": nv.Created})
	}
}

// maxDiffCells caps the LCS table (lines x lines) left after the common
// prefix and suffix are trimmed: 4M cells of int32 is about 16 MB.
const maxDiffCells = 4 << 20

var errDiffTooLarge = errors.New("too many differing lines to compare")

// lineDiff returns a line diff from a to b: unchanged lines prefixed with two
// spaces, removed with "- " and added with "+ ".
func lineDiff(a, b []string) ([]string, error) {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	head, tail := a[:pre], a[len(a)-suf:]
	a, b = a[pre:len(a)-suf], b[pre:len(b)-suf]
	n, k := len(a), len(b)
	if (n+1)*(k+1) > maxDiffCells {
		return nil, errDiffTooLarge
	}
	l := make([][]int32, n+1)
	for i := range l {
		l[i] = make([]int32, k+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := k - 1; j >= 0; j-- {
			if a[i] == b[j] {
				l[i][j] = l[i+1][j+1] + 1
			} else if l[i+1][j] >= l[i][j+1] {
				l[i][j] = l[i+1][j]
			} else {
				l[i][j] = l[i][j+1]
			}
		}
	}
	var out []string
	for _, l := range head {
		out = append(out, "  "+l)
	}
	i, j := 0, 0
	for i < n && j < k {
		switch {
		case a[i] == b[j]:
			out = append(out, "  "+a[i])
			i++
			j++
		case l[i+1][j] >= l[i][j+1]:
			out = append(out, "- "+a[i])
			i++
		default:
			out = append(out, "+ "+b[j])
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, "- "+a[i])
	}
	for ; j < k; j++ {
		out = append(out, "+ "+b[j])
	}
	for _, l := range tail {
		out = append(out, "  "+l)
	}
	return out, nil
}

func splitLines(b []byte) []string {
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// libraryDiff handles GET /app/api/library/{family}/{version}/diff?against=.
// The output runs from the against version to this one.
func (m *briefManager) libraryDiff(w http.ResponseWriter, r *http.Request) {
	family, name := r.PathValue("family"), r.PathValue("version")
	against := r.URL.Query().Get("against")
	_, cur, err1 := m.lib.Get(family, name)
	_, old, err2 := m.lib.Get(family, against)
	if err1 != nil || err2 != nil {
		briefError(w, http.StatusNotFound, "no such version", nil)
		return
	}
	a, b := splitLines(old), splitLines(cur)
	lines, err := lineDiff(a, b)
	if err != nil {
		http.Error(w, "too many differing lines to compare", http.StatusRequestEntityTooLarge)
		return
	}
	var sb strings.Builder
	sb.WriteString("--- " + family + " " + against + "\n+++ " + family + " " + name + "\n")
	for _, l := range lines {
		sb.WriteString(l + "\n")
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(sb.String()))
}
