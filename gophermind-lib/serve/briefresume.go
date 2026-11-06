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
// which resets the row, and runs `brief run <id> --repo <repo>` alone, where
// repo is the clone, or the shared root for a run that predates per-run repos.
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
	if err := m.resumeFrom(rec, from); err != nil {
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
// the resume and starts the process. A refusal frees the slot, changes nothing
// in the run folder and is noted in run.log.
func (m *briefManager) resumeFrom(rec briefRecord, from string) error {
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
			// An answer saved while this refusal held the slot continues now.
			go m.resumeAnswered()
		}
	}()
	step, err := resume.ParseStep(from)
	if err != nil {
		return err
	}

	root := runRoot(m.repos, m.root, rec.ID)
	runDir := filepath.Join(root, ".gophermind", rec.ID)
	if fi, err := os.Stat(runDir); err != nil || !fi.IsDir() {
		return errNoSuchRun
	}
	repo := m.repoFor(rec.ID)
	if root == m.root {
		// A run that predates per-run repos builds in the shared root, which
		// the service does not own: keep it, never reset it.
		repo = m.root
	}
	// run.log (which the run page tails) records the planned clone reset before
	// it acts, then what moved and what the reset did, before any process starts.
	logf, err := os.OpenFile(filepath.Join(m.runDir(rec.ID), "run.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return &resumeStartError{err}
	}
	defer func() {
		if !started {
			logf.Close()
		}
	}()
	stamp := m.stamp()
	fmt.Fprintf(logf, "resume from %s at %s\n", from, stamp)
	// record writes what a package call moved and did, also when it refused
	// after a partial change.
	record := func(res resume.Result, err error) error {
		for _, d := range res.Discarded {
			fmt.Fprintf(logf, "discarded: %s\n", d)
		}
		if res.DiscardDir != "" && len(res.Discarded) > 0 {
			fmt.Fprintf(logf, "discard dir: %s\n", res.DiscardDir)
		}
		for _, act := range res.RepoActions {
			fmt.Fprintln(logf, act)
		}
		if err != nil {
			fmt.Fprintf(logf, "resume refused: %v\n", err)
		}
		return err
	}

	var first []string
	now := m.now()
	board := blackboard.NewFS(runfs.Fixed(runDir))
	if step.Node != "" {
		if err := record(resume.FromNode(context.Background(), runDir, board, rec.ID, step.Node, now)); err != nil {
			return err
		}
		rec.Phase = phaseRunning
	} else {
		req := resume.RepoRequest{Repo: repo, Mode: resume.RepoKeep}
		if m.repos != "" && root != m.root {
			req = resume.RepoRequest{Repo: repo, Mode: resume.RepoOwnedClone, OwnedUnder: m.repos}
			branch, base, began, err := resume.ReadExecutorState(runDir)
			if err != nil {
				return record(resume.Result{}, err)
			}
			for _, c := range resume.PlanOwnedClone(repo, resume.RepoState{Branch: branch, Base: base, Started: began}) {
				fmt.Fprintf(logf, "planned: %s\n", c)
			}
		}
		if err := record(resume.FromStage(context.Background(), runDir, board, rec.ID, step.Stage, req, now)); err != nil {
			return err
		}
		first = []string{"brief", "resume", rec.ID, "--yes", "--gate", "file"}
		rec.Phase = phasePlanning
	}

	rec.Error, rec.ExitCode, rec.FinishedAt = "", nil, ""
	rec.ResumedAt, rec.ResumedFrom = stamp, from
	if err := m.writeRecord(rec); err != nil {
		return &resumeStartError{err}
	}
	started = true
	m.log.Info("brief resume from step", "run_id", rec.ID, "step", from)
	// For a node there is no first step: brief run, on the reset row, is the
	// only process.
	go m.runSteps(a, rec, first, repo, logf)
	return nil
}
