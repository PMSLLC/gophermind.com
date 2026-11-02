package serve

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"gophermind/gophermind-lib/build/brief"
	"gophermind/gophermind-lib/library"
)

// requiredSections are the headings every brief carries, in the spec's order.
var requiredSections = []string{"Overview", "Features", "Architecture", "Data", "Constraints", "Out of scope", "Acceptance"}

const preambleHeading = "Before the first section"

type previewSection struct {
	Heading string `json:"heading"`
	Body    string `json:"body"`
}

// previewSections splits the body of a stored version at its "## " headings
// (outside code fences). The seven required headings come first in the spec's
// order, a missing one with an empty body. Every other section follows in file
// order, and so does any text ahead of the first heading, so nothing is hidden.
func previewSections(text []byte) []previewSection {
	body := string(text)
	if _, he, ok := frontEnd(text); ok {
		rest := body[he:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			body = rest[nl+1:]
		} else {
			body = ""
		}
	}
	var all []previewSection
	var lines [][]string
	cur := -1
	inFence := false
	hasPre := false
	add := func(h string) {
		all = append(all, previewSection{Heading: h})
		lines = append(lines, nil)
		cur = len(all) - 1
	}
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
		}
		if !inFence && strings.HasPrefix(line, "## ") {
			add(strings.TrimSpace(line[3:]))
			continue
		}
		if cur < 0 {
			if t == "" {
				continue
			}
			add(preambleHeading)
			hasPre = true
		}
		lines[cur] = append(lines[cur], line)
	}
	for i := range all {
		all[i].Body = strings.Trim(strings.Join(lines[i], "\n"), "\n")
	}
	used := make([]bool, len(all))
	out := make([]previewSection, 0, len(all)+len(requiredSections))
	var pre []previewSection
	if len(all) > 0 && hasPre {
		pre = all[:1]
		used[0] = true
	}
	for _, h := range requiredSections {
		s := previewSection{Heading: h}
		for i := range all {
			if !used[i] && all[i].Heading == h {
				s.Body = all[i].Body
				used[i] = true
				break
			}
		}
		out = append(out, s)
	}
	out = append(out, pre...)
	for i := range all {
		if !used[i] {
			out = append(out, all[i])
		}
	}
	return out
}

// previewToken identifies the stored text a preview showed.
func previewToken(text []byte) string {
	sum := sha256.Sum256(text)
	return hex.EncodeToString(sum[:])
}

// provisionalRunName is the id startVersion would assign right now. It is not
// reserved; the real id comes back from the run call.
func (m *briefManager) provisionalRunName(family, version string) (string, error) {
	day := m.now().UTC()
	n, err := m.nextRunCounter(family, version, day.Format("20060102"))
	if err != nil {
		return "", err
	}
	return library.RunName(family, version, day, n), nil
}

// libraryPreview handles GET /app/api/library/{family}/{version}/preview. It
// only reads: no run folder is made and the version stays unlocked.
func (m *briefManager) libraryPreview(w http.ResponseWriter, r *http.Request) {
	family, name := r.PathValue("family"), r.PathValue("version")
	v, text, err := m.lib.Get(family, name)
	if err != nil {
		briefError(w, http.StatusNotFound, "no such version", nil)
		return
	}
	id, err := m.provisionalRunName(family, name)
	if err != nil {
		briefError(w, http.StatusInternalServerError, "cannot read the runs", nil)
		return
	}
	stamped := stampFront(text, id, m.root)
	problems := brief.Check(stamped)
	if problems == nil {
		problems = []brief.Problem{}
	}
	warnings := brief.Lint(stamped)
	if warnings == nil {
		warnings = []brief.Problem{}
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
	briefJSON(w, http.StatusOK, map[string]any{
		"family": family, "version": v.Name, "parent": v.Parent, "children": children,
		"locked":        v.Locked || len(m.runsByVersion()[family+"/"+name]) > 0,
		"preview_token": previewToken(text), "sections": previewSections(text), "problems": problems, "warnings": warnings,
		"ask_default": true, "run_name": id,
	})
}

// libraryRun handles POST /app/api/library/{family}/{version}/run, the call
// behind the preview page's Continue button. The preview token is required, so
// a direct POST cannot skip the preview. The token and the brief's problems are
// checked inside startVersion under the family lock: a changed brief is a 409,
// a brief with problems a 400 before anything starts. Warnings never block.
func (m *briefManager) libraryRun(w http.ResponseWriter, r *http.Request) {
	family, name := r.PathValue("family"), r.PathValue("version")
	var body struct {
		Ask          *bool  `json:"ask"`
		PreviewToken string `json:"preview_token"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		briefError(w, http.StatusBadRequest, "send JSON like {\"ask\": true, \"preview_token\": \"...\"}", nil)
		return
	}
	ask := body.Ask == nil || *body.Ask
	if _, _, err := m.lib.Get(family, name); err != nil {
		briefError(w, http.StatusNotFound, "no such version", nil)
		return
	}
	if body.PreviewToken == "" {
		briefError(w, http.StatusBadRequest, "preview_token is required: open the preview and press Continue", nil)
		return
	}
	rec, warnings, err := m.startVersion(family, name, ask, body.PreviewToken)
	m.startReply(w, rec, warnings, err)
}
