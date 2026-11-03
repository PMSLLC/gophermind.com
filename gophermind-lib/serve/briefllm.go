package serve

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"gophermind/gophermind-lib/build/ledger"
	"gophermind/gophermind-lib/runview"
)

// BriefLLMCall is one model call of a run as the log page lists it: the ledger
// line plus whether its prompt and reply text was kept.
type BriefLLMCall struct {
	ID               int64  `json:"id"`
	At               string `json:"at,omitempty"`
	Stage            string `json:"stage"`
	NodeID           string `json:"node_id,omitempty"`
	Provider         string `json:"provider,omitempty"`
	ModelServed      string `json:"model_served,omitempty"`
	DurationMS       int64  `json:"duration_ms"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	Outcome          string `json:"outcome"`
	ErrorKind        string `json:"error_kind,omitempty"`
	HasText          bool   `json:"has_text"`
}

// llmTarget validates the run id of a request and returns the id the server
// uses, its ledger and the run folder. ok is false after it has answered 404.
func (m *briefManager) llmTarget(w http.ResponseWriter, r *http.Request) (id string, fs *ledger.FS, dir string, ok bool) {
	id = r.PathValue("id")
	if !runview.ValidRunID(id) {
		briefError(w, http.StatusNotFound, "no such run", nil)
		return "", nil, "", false
	}
	id = m.resolveID(id)
	dir = filepath.Join(runRoot(m.repos, m.root, id), ".gophermind", id)
	_, errRec := os.Stat(m.runDir(id))
	_, errDir := os.Stat(dir)
	if errRec != nil && errDir != nil {
		briefError(w, http.StatusNotFound, "no such run", nil)
		return "", nil, "", false
	}
	fs = ledger.NewFS(func(string) (string, error) { return dir, nil })
	return id, fs, dir, true
}

// isVerbose reports whether the run kept call text: the marker is in the run
// folder, or still only in the server's own folder for the run.
func (m *briefManager) isVerbose(id, dir string) bool {
	for _, d := range []string{dir, m.runDir(id)} {
		if _, err := os.Stat(filepath.Join(d, "_state", "verbose")); err == nil {
			return true
		}
	}
	return false
}

// llmList handles GET /app/briefs/{id}/llm: every model call of the run in
// order, and which of them have text.
func (m *briefManager) llmList(w http.ResponseWriter, r *http.Request) {
	id, fs, dir, ok := m.llmTarget(w, r)
	if !ok {
		return
	}
	have := map[int64]bool{}
	if ids, err := fs.ListText(id); err == nil {
		for _, n := range ids {
			have[n] = true
		}
	}
	calls := []BriefLLMCall{}
	for _, c := range parseCalls(dir) {
		calls = append(calls, BriefLLMCall{
			ID: c.ID, At: c.At, Stage: c.Stage, NodeID: c.NodeID, Provider: c.Provider, ModelServed: c.ModelServed,
			DurationMS: c.DurationMS, PromptTokens: c.PromptTokens, CompletionTokens: c.CompletionTokens,
			Outcome: c.Outcome, ErrorKind: c.ErrorKind, HasText: have[c.ID],
		})
	}
	briefJSON(w, http.StatusOK, map[string]any{"verbose": m.isVerbose(id, dir), "calls": calls})
}

// llmText handles GET /app/briefs/{id}/llm/{call}: the prompt and reply kept
// for one call. The call must be a positive integer.
func (m *briefManager) llmText(w http.ResponseWriter, r *http.Request) {
	id, fs, _, ok := m.llmTarget(w, r)
	if !ok {
		return
	}
	n, err := strconv.ParseInt(r.PathValue("call"), 10, 64)
	if err != nil || n <= 0 || strconv.FormatInt(n, 10) != r.PathValue("call") {
		briefError(w, http.StatusNotFound, "no such call", nil)
		return
	}
	t, err := fs.ReadText(id, n)
	if err != nil {
		briefError(w, http.StatusNotFound, "no text kept for this call", nil)
		return
	}
	briefJSON(w, http.StatusOK, t)
}
