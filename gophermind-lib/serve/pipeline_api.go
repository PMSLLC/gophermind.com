package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"gophermind/gophermind-lib/runview"
)

// PipelineSchemaHeader is on every pipeline response, errors included. A
// client that sees another number shows "this server and this app disagree"
// instead of decoding a body it does not understand.
const PipelineSchemaHeader = "X-Gophermind-Pipeline-Schema"

// defaultPipelineStreams is how many GET /pipeline/events streams may be open
// at once when PipelineDeps.MaxStreams is zero.
const defaultPipelineStreams = 16

// pipelineAPI serves the run view of the repo at root. One value is shared by
// every route, because the stream slots are shared.
type pipelineAPI struct {
	root  string
	slots chan struct{}
}

func newPipelineAPI(d PipelineDeps) *pipelineAPI {
	n := d.MaxStreams
	if n < 1 {
		n = defaultPipelineStreams
	}
	return &pipelineAPI{root: d.Root, slots: make(chan struct{}, n)}
}

// withPipelineSchema stamps the schema header before anything else can answer,
// so a 401 from the auth layer carries it too.
func withPipelineSchema(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(PipelineSchemaHeader, strconv.Itoa(runview.SchemaVersion))
		next.ServeHTTP(w, r)
	})
}

func pipelineJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// pipelineError answers with a fixed message. It never echoes the request or a
// path: a bad id is "invalid id or parameter", a missing thing "not found".
func pipelineError(w http.ResponseWriter, err error) {
	status, msg := http.StatusInternalServerError, "internal error"
	switch {
	case errors.Is(err, runview.ErrBadID):
		status, msg = http.StatusBadRequest, "invalid id or parameter"
	case errors.Is(err, runview.ErrNotFound):
		status, msg = http.StatusNotFound, "not found"
	}
	pipelineJSON(w, status, map[string]any{"schema_version": runview.SchemaVersion, "error": msg})
}

// view opens the run named by ?run=, or the most recently updated one. The
// default is picked by runview.NewestRunID, which stats files and builds no
// summary of any run.
func (a *pipelineAPI) view(r *http.Request, opts ...runview.Option) (*runview.View, error) {
	id := r.URL.Query().Get("run")
	if id == "" {
		id = runview.NewestRunID(a.root)
		if id == "" {
			return nil, runview.ErrNotFound
		}
	}
	return runview.Open(a.root, id, opts...)
}

func (a *pipelineAPI) runs(w http.ResponseWriter, r *http.Request) {
	runs, err := runview.Runs(a.root)
	if err != nil {
		pipelineError(w, err)
		return
	}
	pipelineJSON(w, http.StatusOK, runview.RunsBody{SchemaVersion: runview.SchemaVersion, Runs: runs})
}

func (a *pipelineAPI) state(w http.ResponseWriter, r *http.Request) {
	v, err := a.view(r)
	if err != nil {
		if errors.Is(err, runview.ErrNotFound) && r.URL.Query().Get("run") == "" {
			pipelineJSON(w, http.StatusOK, runview.StateBody{
				SchemaVersion: runview.SchemaVersion, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
				Nodes: []runview.NodeSummary{},
			})
			return
		}
		pipelineError(w, err)
		return
	}
	pipelineJSON(w, http.StatusOK, v.State())
}

func (a *pipelineAPI) node(w http.ResponseWriter, r *http.Request) {
	v, err := a.view(r)
	if err != nil {
		pipelineError(w, err)
		return
	}
	body, err := v.Node(r.URL.Query().Get("id"))
	if err != nil {
		pipelineError(w, err)
		return
	}
	pipelineJSON(w, http.StatusOK, body)
}

func (a *pipelineAPI) artifact(w http.ResponseWriter, r *http.Request) {
	v, err := a.view(r)
	if err != nil {
		pipelineError(w, err)
		return
	}
	q := r.URL.Query()
	attempt, aerr := strconv.Atoi(q.Get("attempt"))
	if aerr != nil {
		pipelineError(w, runview.ErrBadID)
		return
	}
	text, err := v.Artifact(q.Get("id"), attempt, q.Get("file"))
	if err != nil {
		pipelineError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(text))
}

func (a *pipelineAPI) decisions(w http.ResponseWriter, r *http.Request) {
	v, err := a.view(r)
	if err != nil {
		pipelineError(w, err)
		return
	}
	body, err := v.Decisions()
	if err != nil {
		pipelineError(w, err)
		return
	}
	pipelineJSON(w, http.StatusOK, body)
}

func (a *pipelineAPI) report(w http.ResponseWriter, r *http.Request) {
	v, err := a.view(r)
	if err != nil {
		pipelineError(w, err)
		return
	}
	body, err := v.Report()
	if err != nil {
		pipelineError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}
