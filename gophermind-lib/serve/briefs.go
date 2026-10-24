package serve

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gophermind/gophermind-lib/build/brief"
	"gophermind/gophermind-lib/runview"
)

// maxBriefBytes caps an uploaded brief.
const maxBriefBytes = 256 << 10

// Run phases of a record the web interface made. planning and running are
// live; waiting is paused on a person; the rest are final.
const (
	phasePlanning    = "planning"
	phaseWaiting     = "waiting" // planning paused at open questions (file gate); no process runs
	phaseRunning     = "running"
	phaseDone        = "done"
	phaseFailed      = "failed"
	phaseStopped     = "stopped"
	phaseInterrupted = "interrupted"
)

// briefRecord is <BriefsDir>/<id>/run.json: what the server knows about a run
// it launched. The run folder itself (nodes, attempts, report) is written by
// gophermind and read through runview.
type briefRecord struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Phase      string `json:"phase"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at,omitempty"`
	Error      string `json:"error,omitempty"`
	// Ask is set when the run was uploaded with ask=1: planning uses the file
	// gate and stops at open questions instead of taking unattended defaults.
	Ask bool `json:"ask,omitempty"`
}

// exitWaiting is what `gophermind brief plan|resume` exits with when the file
// gate is waiting on a person.
const exitWaiting = 3

// briefManager launches `gophermind brief plan` then `brief run`, one run at
// a time, and keeps a record, a copy of the brief and a log per run under dir.
type briefManager struct {
	dir  string // server-owned: <id>/{brief.md,run.json,run.log}
	root string // repo root whose .gophermind/<id> folders runview reads
	// cmd builds the process for one gophermind invocation (args exclude the
	// program name). The default runs gophermind directly.
	cmd       func(args []string) *exec.Cmd
	signal    func(pgid int, force bool) error
	killGrace time.Duration
	now       func() time.Time
	log       *slog.Logger

	mu       sync.Mutex
	active   *activeBrief
	answerMu sync.Mutex // one answer at a time: file edit, then resume

	// events: open-stream slots and how often one stream looks at the files.
	streams    chan struct{}
	eventEvery time.Duration
}

type activeBrief struct {
	id      string
	stopped bool
	pgid    int
	done    chan struct{}
	// sudoHidden counts the sudoers warnings kept out of run.log so far.
	sudoHidden int
}

type briefBusyError struct{ activeID string }

func (e *briefBusyError) Error() string { return "a run is already active: " + e.activeID }

var (
	errBriefExists = errors.New("a run with this id already exists")
	errNotActive   = errors.New("run is not active")
)

func newBriefManager(opt Options, root string) (*briefManager, error) {
	m := &briefManager{
		dir: opt.BriefsDir, root: root, cmd: opt.BriefCommand, signal: opt.BriefSignal,
		streams: make(chan struct{}, maxEventStreams), eventEvery: defaultEventEvery,
		killGrace: 10 * time.Second, now: func() time.Time { return time.Now().UTC() },
		log: opt.BriefLogger,
	}
	if m.log == nil {
		m.log = slog.Default()
	}
	if m.cmd == nil {
		m.cmd = func(args []string) *exec.Cmd { return exec.Command("gophermind", args...) }
	}
	if m.signal == nil {
		m.signal = signalGroup
	}
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return nil, fmt.Errorf("briefs dir: %w", err)
	}
	// A record still live when the server starts lost its process with the
	// previous server: mark it so the page does not show it running forever.
	ents, _ := os.ReadDir(m.dir)
	for _, e := range ents {
		rec, ok := m.readRecord(e.Name())
		if ok && (rec.Phase == phasePlanning || rec.Phase == phaseRunning) {
			rec.Phase, rec.Error = phaseInterrupted, "the server restarted while this run was live"
			rec.FinishedAt = m.stamp()
			_ = m.writeRecord(rec)
		}
	}
	return m, nil
}

func (m *briefManager) stamp() string { return m.now().Format("2006-01-02T15:04:05Z") }

func (m *briefManager) runDir(id string) string { return filepath.Join(m.dir, id) }

func (m *briefManager) readRecord(id string) (briefRecord, bool) {
	if !runview.ValidRunID(id) {
		return briefRecord{}, false
	}
	b, err := readSmallFile(filepath.Join(m.runDir(id), "run.json"), 64<<10)
	if err != nil {
		return briefRecord{}, false
	}
	var rec briefRecord
	if json.Unmarshal(b, &rec) != nil || rec.ID != id {
		return briefRecord{}, false
	}
	return rec, true
}

func (m *briefManager) writeRecord(rec briefRecord) error {
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(m.runDir(rec.ID), "run.json")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// readSmallFile reads a regular file (never a link) of at most max bytes.
func readSmallFile(path string, max int64) ([]byte, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fs.ErrInvalid
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, max))
}

// start records a new run and launches it in the background. The slot is
// taken before anything is written, so two requests cannot both start.
func (m *briefManager) start(b *brief.Brief, src []byte, ask bool) (briefRecord, error) {
	id := b.Front.ID
	m.mu.Lock()
	if m.active != nil {
		busy := &briefBusyError{activeID: m.active.id}
		m.mu.Unlock()
		return briefRecord{}, busy
	}
	dir := m.runDir(id)
	if err := os.Mkdir(dir, 0o755); err != nil {
		m.mu.Unlock()
		if errors.Is(err, fs.ErrExist) {
			return briefRecord{}, errBriefExists
		}
		return briefRecord{}, err
	}
	rec := briefRecord{ID: id, Title: b.Front.Title, Phase: phasePlanning, StartedAt: m.stamp(), Ask: ask}
	briefPath := filepath.Join(dir, "brief.md")
	logf, err := m.prepare(dir, briefPath, src, rec)
	if err != nil {
		m.mu.Unlock()
		_ = os.RemoveAll(dir)
		return briefRecord{}, err
	}
	a := &activeBrief{id: id, done: make(chan struct{})}
	m.active = a
	m.mu.Unlock()
	m.log.Info("brief upload accepted", "run_id", id, "ask", ask)
	go m.run(a, rec, planArgs(briefPath, ask), logf)
	return rec, nil
}

// planArgs is the first step: unattended defaults, or the file gate when the
// upload asked to be asked. Approval stays unattended (--yes) either way.
func planArgs(briefPath string, ask bool) []string {
	args := []string{"brief", "plan", briefPath, "--yes"}
	if ask {
		args = append(args, "--gate", "file")
	}
	return args
}

// resume continues a run that is waiting on answers: it takes the slot, marks
// the run planning again and runs `brief resume` with the file gate.
func (m *briefManager) resume(rec briefRecord) error {
	m.mu.Lock()
	if m.active != nil {
		busy := &briefBusyError{activeID: m.active.id}
		m.mu.Unlock()
		return busy
	}
	logf, err := os.OpenFile(filepath.Join(m.runDir(rec.ID), "run.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	rec.Phase, rec.Error = phasePlanning, ""
	if err := m.writeRecord(rec); err != nil {
		m.mu.Unlock()
		logf.Close()
		return err
	}
	a := &activeBrief{id: rec.ID, done: make(chan struct{})}
	m.active = a
	m.mu.Unlock()
	m.log.Info("brief resume started", "run_id", rec.ID)
	go m.run(a, rec, []string{"brief", "resume", rec.ID, "--yes", "--gate", "file"}, logf)
	return nil
}

func (m *briefManager) prepare(dir, briefPath string, src []byte, rec briefRecord) (*os.File, error) {
	if err := os.WriteFile(briefPath, src, 0o644); err != nil {
		return nil, err
	}
	if err := m.writeRecord(rec); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, "run.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}

// denyEnv names the server's own secrets: a launched run never inherits them.
// GOPHERMIND_WEB_PASSWORD_HASH is no longer read but may linger in an env file.
var denyEnv = []string{
	"GOPHERMIND_TOKEN", "GOPHERMIND_SERVE_TOKEN", "GOPHERMIND_SERVE_HMAC_SECRET", "GOPHERMIND_SESSION_SECRET",
	"GOPHERMIND_GOOGLE_CLIENT_SECRET", "GOPHERMIND_WEB_PASSWORD_HASH",
}

func childEnv() []string {
	var out []string
outer:
	for _, kv := range os.Environ() {
		for _, d := range denyEnv {
			if strings.HasPrefix(kv, d+"=") {
				continue outer
			}
		}
		out = append(out, kv)
	}
	return out
}

// step runs one gophermind invocation in its own process group with output to
// the run log, and returns its exit code. A stop requested before it starts
// returns errNotActive without starting it.
func (m *briefManager) step(a *activeBrief, logf *os.File, args ...string) (int, error) {
	cmd := m.cmd(args)
	sf := &sudoFilter{w: logf}
	cmd.Stdout, cmd.Stderr, cmd.Stdin = logf, sf, nil
	if cmd.Env == nil {
		cmd.Env = childEnv()
	}
	ownProcessGroup(cmd)
	fmt.Fprintf(logf, "== gophermind %s ==\n", strings.Join(args, " "))
	m.mu.Lock()
	if a.stopped {
		m.mu.Unlock()
		return 0, errNotActive
	}
	err := cmd.Start()
	if err == nil {
		a.pgid = cmd.Process.Pid
	}
	m.mu.Unlock()
	stepName := ""
	if len(args) > 1 {
		stepName = args[1]
	}
	if err != nil {
		m.log.Error("brief process not started", "run_id", a.id, "step", stepName, "error", err.Error())
		return 0, err
	}
	began := time.Now()
	m.log.Info("brief process started", "run_id", a.id, "step", stepName, "pid", cmd.Process.Pid)
	err = cmd.Wait()
	_ = sf.Flush()
	m.mu.Lock()
	a.pgid = 0
	a.sudoHidden += sf.hidden
	m.mu.Unlock()
	var ee *exec.ExitError
	code := 0
	if errors.As(err, &ee) {
		code, err = ee.ExitCode(), nil
	}
	ended := []any{"run_id", a.id, "step", stepName, "exit_code", code, "duration_ms", time.Since(began).Milliseconds()}
	if err != nil {
		m.log.Error("brief process ended", append(ended, "error", err.Error())...)
		return 0, err
	}
	m.log.Info("brief process ended", ended...)
	return code, nil
}

func (m *briefManager) run(a *activeBrief, rec briefRecord, first []string, logf *os.File) {
	defer func() {
		m.mu.Lock()
		hidden := a.sudoHidden
		m.mu.Unlock()
		if hidden > 0 {
			fmt.Fprintf(logf, "(%d sudo warnings hidden)\n", hidden)
		}
		logf.Close()
		m.mu.Lock()
		m.active = nil
		m.mu.Unlock()
		close(a.done)
	}()
	finish := func(phase string, code *int, msg string) {
		m.mu.Lock()
		stopped := a.stopped
		m.mu.Unlock()
		if stopped && phase != phaseDone {
			phase = phaseStopped
		}
		rec.Phase, rec.ExitCode, rec.Error, rec.FinishedAt = phase, code, msg, m.stamp()
		_ = m.writeRecord(rec)
	}
	code, err := m.step(a, logf, first...)
	if err == nil && code == exitWaiting && rec.Ask {
		m.mu.Lock()
		stopped := a.stopped
		m.mu.Unlock()
		if !stopped {
			rec.Phase, rec.Error = phaseWaiting, ""
			_ = m.writeRecord(rec)
			return
		}
	}
	if err != nil || code != 0 {
		finish(phaseFailed, &code, stepMessage("plan", code, err))
		return
	}
	rec.Phase = phaseRunning
	_ = m.writeRecord(rec)
	code, err = m.step(a, logf, "brief", "run", rec.ID, "--repo", m.root)
	if err != nil || code != 0 {
		finish(phaseFailed, &code, stepMessage("run", code, err))
		return
	}
	finish(phaseDone, &code, "")
}

func stepMessage(step string, code int, err error) string {
	if err != nil {
		return fmt.Sprintf("brief %s: %v", step, err)
	}
	return fmt.Sprintf("brief %s exited with code %d", step, code)
}

// stop signals the active run's process group, and kills it if it has not
// exited after the grace period.
func (m *briefManager) stop(id string) error {
	m.mu.Lock()
	a := m.active
	if a == nil || a.id != id {
		m.mu.Unlock()
		return errNotActive
	}
	a.stopped = true
	pgid := a.pgid
	m.mu.Unlock()
	m.log.Info("brief stop requested", "run_id", id, "process_live", pgid > 0)
	if pgid > 0 {
		_ = m.signal(pgid, false)
		go func() {
			select {
			case <-a.done:
			case <-time.After(m.killGrace):
				m.mu.Lock()
				p := a.pgid
				m.mu.Unlock()
				if p > 0 {
					_ = m.signal(p, true)
				}
			}
		}()
	}
	return nil
}

func (m *briefManager) activeID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return ""
	}
	return m.active.id
}

func briefJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func briefError(w http.ResponseWriter, code int, msg string, extra map[string]any) {
	body := map[string]any{"error": msg}
	for k, v := range extra {
		body[k] = v
	}
	briefJSON(w, code, body)
}

// readBriefBody returns the uploaded brief: the "brief" part of a multipart
// form, or the raw body. Either is capped at maxBriefBytes.
func readBriefBody(w http.ResponseWriter, r *http.Request) ([]byte, int, string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBriefBytes+16<<10)
	tooBig := func(err error) bool {
		var mbe *http.MaxBytesError
		return errors.As(err, &mbe)
	}
	var src []byte
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt == "multipart/form-data" {
		mr, err := r.MultipartReader()
		if err != nil {
			return nil, http.StatusBadRequest, "bad multipart body"
		}
		found := false
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				if tooBig(err) {
					return nil, http.StatusRequestEntityTooLarge, "brief is larger than 256 KiB"
				}
				return nil, http.StatusBadRequest, "bad multipart body"
			}
			if p.FormName() != "brief" {
				continue
			}
			src, err = io.ReadAll(io.LimitReader(p, maxBriefBytes+1))
			if err != nil {
				if tooBig(err) {
					return nil, http.StatusRequestEntityTooLarge, "brief is larger than 256 KiB"
				}
				return nil, http.StatusBadRequest, "bad multipart body"
			}
			found = true
			break
		}
		if !found {
			return nil, http.StatusBadRequest, "no \"brief\" file in the form"
		}
	} else {
		var err error
		src, err = io.ReadAll(io.LimitReader(r.Body, maxBriefBytes+1))
		if err != nil {
			if tooBig(err) {
				return nil, http.StatusRequestEntityTooLarge, "brief is larger than 256 KiB"
			}
			return nil, http.StatusBadRequest, "cannot read body"
		}
	}
	if len(src) > maxBriefBytes {
		return nil, http.StatusRequestEntityTooLarge, "brief is larger than 256 KiB"
	}
	if len(bytes.TrimSpace(src)) == 0 {
		return nil, http.StatusBadRequest, "empty brief"
	}
	return src, 0, ""
}

// sameDir reports whether two paths name the same directory, following links
// where both exist.
func sameDir(a, b string) bool {
	clean := func(p string) string {
		p = filepath.Clean(p)
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return p
	}
	return clean(a) == clean(b)
}

// create handles POST /app/briefs.
func (m *briefManager) create(w http.ResponseWriter, r *http.Request) {
	src, code, msg := readBriefBody(w, r)
	if code != 0 {
		briefError(w, code, msg, nil)
		return
	}
	b, err := brief.Parse(src)
	if err != nil {
		briefError(w, http.StatusBadRequest, "invalid brief: "+err.Error(), nil)
		return
	}
	if !runview.ValidRunID(b.Front.ID) {
		briefError(w, http.StatusBadRequest, "invalid brief: id must look like gm-YYYY-MM-DD-NNN", nil)
		return
	}
	// The run folder is made inside the brief's repo; this server only shows
	// folders under its own root, so any other repo would run unseen.
	if !filepath.IsAbs(b.Front.Repo) || !sameDir(b.Front.Repo, m.root) {
		briefError(w, http.StatusBadRequest,
			"invalid brief: repo must be the server's repo root, "+m.root, map[string]any{"repo_root": m.root})
		return
	}
	rec, err := m.start(b, src, r.URL.Query().Get("ask") == "1")
	var busy *briefBusyError
	switch {
	case errors.As(err, &busy):
		briefError(w, http.StatusConflict, "a run is already active", map[string]any{"active_run_id": busy.activeID})
	case errors.Is(err, errBriefExists):
		briefError(w, http.StatusConflict, "a run with this id already exists", map[string]any{"run_id": b.Front.ID})
	case err != nil:
		briefError(w, http.StatusInternalServerError, "cannot start the run", nil)
	default:
		briefJSON(w, http.StatusAccepted, map[string]any{"run_id": rec.ID, "url": "/app/briefs/" + rec.ID})
	}
}

// stopHandler handles POST /app/briefs/{id}/stop.
func (m *briefManager) stopHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !runview.ValidRunID(id) {
		briefError(w, http.StatusNotFound, "no such run", nil)
		return
	}
	if err := m.stop(id); err != nil {
		briefError(w, http.StatusConflict, "run is not active", map[string]any{"active_run_id": m.activeID()})
		return
	}
	briefJSON(w, http.StatusAccepted, map[string]any{"run_id": id, "stopping": true})
}

func readDirNames(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out, nil
}
