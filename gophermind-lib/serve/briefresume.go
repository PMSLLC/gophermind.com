package serve

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"gophermind/gophermind-lib/build/blackboard"
	"gophermind/gophermind-lib/build/resume"
	"gophermind/gophermind-lib/build/runfs"
	"gophermind/gophermind-lib/runview"
)

// resumeHandler handles POST /app/briefs/{id}/resume?from=<step>: it discards
// what the run made from that step on and starts it again from there.
//
// The service owns the per-run clones, so it calls the resume package directly
// and does not use the CLI flags of `brief resume` / `brief run` (those are for
// a person at a terminal). For a planner stage it calls resume.FromStage with
// RepoOwnedClone under the repos directory (a shared repo is only kept, never
// reset), then runs `brief resume <id> --yes --gate file`, after which the
// launcher runs `brief run` as usual. For a node it calls resume.FromNode,
// which resets the row, and runs `brief run <id> --repo <clone>` alone.
func (m *briefManager) resumeHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !runview.ValidRunID(id) {
		briefError(w, http.StatusNotFound, "no such run", nil)
		return
	}
	id = m.resolveID(id)
	rec, ok := m.readRecord(id)
	if !ok {
		briefError(w, http.StatusNotFound, "no such run", nil)
		return
	}
	from := r.URL.Query().Get("from")
	step, err := resume.ParseStep(from)
	if err != nil {
		briefError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	if err := m.resumeFrom(rec, from, step); err != nil {
		var busy *briefBusyError
		switch {
		case errors.As(err, &busy):
			briefError(w, http.StatusConflict, "a run is already active", map[string]any{"active_run_id": busy.activeID})
		case errors.Is(err, errNoSuchRun):
			briefError(w, http.StatusNotFound, "no such run", nil)
		case errors.As(err, new(*resumeStartError)):
			m.log.Error("brief resume not started", "run_id", id, "error", err.Error())
			briefError(w, http.StatusInternalServerError, "cannot resume the run", nil)
		default:
			briefError(w, http.StatusBadRequest, err.Error(), nil)
		}
		return
	}
	briefJSON(w, http.StatusAccepted, map[string]any{"run_id": id, "url": "/app/briefs/" + id})
}

// resumeStartError is a failure of the launcher itself (log, record), not a
// refusal by the resume package.
type resumeStartError struct{ err error }

func (e *resumeStartError) Error() string { return e.err.Error() }
func (e *resumeStartError) Unwrap() error { return e.err }

// resumeFrom takes the slot, discards the run's output from step on, records
// the resume and starts the process. A refusal frees the slot and changes
// nothing in the run folder.
func (m *briefManager) resumeFrom(rec briefRecord, from string, step resume.Step) error {
	m.mu.Lock()
	if m.active != nil {
		busy := &briefBusyError{activeID: m.active.id}
		m.mu.Unlock()
		return busy
	}
	a := &activeBrief{id: rec.ID, done: make(chan struct{})}
	m.active = a
	m.mu.Unlock()
	started := false
	defer func() {
		if !started {
			m.mu.Lock()
			m.active = nil
			m.mu.Unlock()
			close(a.done)
		}
	}()

	root := runRoot(m.repos, m.root, rec.ID)
	runDir := filepath.Join(root, ".gophermind", rec.ID)
	if fi, err := os.Stat(runDir); err != nil || !fi.IsDir() {
		return errNoSuchRun
	}
	repo := m.repoFor(rec.ID)
	var first []string
	now := m.now()
	if step.Node != "" {
		board := blackboard.NewFS(runfs.Fixed(runDir))
		if _, err := resume.FromNode(context.Background(), runDir, board, rec.ID, step.Node, now); err != nil {
			return err
		}
		rec.Phase = phaseRunning
	} else {
		req := resume.RepoRequest{Repo: repo, Mode: resume.RepoKeep}
		if m.repos != "" {
			req = resume.RepoRequest{Repo: repo, Mode: resume.RepoOwnedClone, OwnedUnder: m.repos}
		}
		if _, err := resume.FromStage(runDir, step.Stage, req, now); err != nil {
			return err
		}
		first = []string{"brief", "resume", rec.ID, "--yes", "--gate", "file"}
		rec.Phase = phasePlanning
	}

	logf, err := os.OpenFile(filepath.Join(m.runDir(rec.ID), "run.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return &resumeStartError{err}
	}
	stamp := m.stamp()
	fmt.Fprintf(logf, "resume from %s at %s\n", from, stamp)
	rec.Error, rec.ExitCode, rec.FinishedAt = "", nil, ""
	rec.ResumedAt, rec.ResumedFrom = stamp, from
	if err := m.writeRecord(rec); err != nil {
		logf.Close()
		return &resumeStartError{err}
	}
	started = true
	m.log.Info("brief resume from step", "run_id", rec.ID, "step", from)
	// For a node there is no first step: brief run, on the reset row, is the
	// only process.
	go m.runSteps(a, rec, first, logf)
	return nil
}
