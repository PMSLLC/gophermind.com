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
	"regexp"
	"strings"
	"sync"
	"time"

	"gophermind/gophermind-lib/build/brief"
	"gophermind/gophermind-lib/library"
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
	// Family and Version name the library version the run was started from;
	// empty for a run uploaded as plain text.
	Family  string `json:"family,omitempty"`
	Version string `json:"version,omitempty"`
	// Verbose is set when the run was started with the verbose option: its
	// model calls keep prompt and reply text under _state/llm.
	Verbose bool `json:"verbose,omitempty"`
}

// exitWaiting is what `gophermind brief plan|resume` exits with when the file
// gate is waiting on a person.
const exitWaiting = 3

// briefManager launches `gophermind brief plan` then `brief run`, one run at
// a time, and keeps a record, a copy of the brief and a log per run under dir.
type briefManager struct {
	dir  string // server-owned: <id>/{brief.md,run.json,run.log}
	root string // repo root whose .gophermind/<id> folders runview reads
	// repos, when set, holds one fresh repo per run (<repos>/<id>); a run then
	// never shares a working tree with another, and its brief names no repo.
	repos string
	// cmd builds the process for one gophermind invocation (args exclude the
	// program name). The default runs gophermind directly.
	cmd       func(args []string) *exec.Cmd
	signal    func(pgid int, force bool) error
	killGrace time.Duration
	now       func() time.Time
	saveMu    sync.Mutex // serialises find-or-save of library versions
	log       *slog.Logger
	lib       *library.Store // <dir>/library: saved brief versions

	mu       sync.Mutex
	active   *activeBrief
	answerMu sync.Mutex // one answer at a time: file edit, then resume

	// events: open-stream slots and how often one stream looks at the files.
	streams    chan struct{}
	eventEvery time.Duration

	// aliases caches <dir>/library/aliases.json (old run id to new), reloaded
	// when the file's mtime or size changes.
	aliasMu    sync.Mutex
	aliases    library.Aliases
	aliasMtime time.Time
	aliasSize  int64

	// beforeStartLock is a test hook run by startVersion just before it takes
	// the family lock.
	beforeStartLock func()
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
		dir: opt.BriefsDir, root: root, repos: opt.ReposDir, cmd: opt.BriefCommand, signal: opt.BriefSignal,
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
	lib, err := library.Open(filepath.Join(m.dir, "library"))
	if err != nil {
		return nil, fmt.Errorf("brief library: %w", err)
	}
	m.lib = lib
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

// resolveID maps an old run id renamed by `gophermind brief migrate-library`
// to its new id through <dir>/library/aliases.json. An id that is itself a run
// folder, an invalid id and an unknown id are returned unchanged.
func (m *briefManager) resolveID(id string) string {
	if !runview.ValidRunID(id) {
		return id
	}
	if _, err := os.Lstat(m.runDir(id)); err == nil {
		return id
	}
	path := filepath.Join(m.dir, "library", library.AliasesFile)
	m.aliasMu.Lock()
	defer m.aliasMu.Unlock()
	fi, err := os.Stat(path)
	if err != nil {
		m.aliases, m.aliasMtime, m.aliasSize = nil, time.Time{}, 0
		return id
	}
	if m.aliases == nil || !fi.ModTime().Equal(m.aliasMtime) || fi.Size() != m.aliasSize {
		a, err := library.LoadAliases(filepath.Dir(path))
		if err != nil {
			m.log.Warn("brief aliases unreadable", "path", path, "error", err.Error())
			return id
		}
		m.aliases, m.aliasMtime, m.aliasSize = a, fi.ModTime(), fi.Size()
	}
	if n := m.aliases.Resolve(id); runview.ValidRunID(n) {
		return n
	}
	return id
}

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

// startRec records a new run and launches it in the background. The slot is
// taken before anything is written, so two requests cannot both start. base
// carries the record fields the caller already knows (Ask, Family, Version).
func (m *briefManager) startRec(b *brief.Brief, src []byte, base briefRecord) (briefRecord, error) {
	ask := base.Ask
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
	if m.repos != "" {
		repo := filepath.Join(m.repos, id)
		if out, err := m.cmd([]string{"brief", "init-repo", repo}).CombinedOutput(); err != nil {
			m.mu.Unlock()
			_ = os.RemoveAll(dir)
			m.log.Error("run repo not made", "run_id", id, "error", err.Error(), "output", strings.TrimSpace(string(out)))
			return briefRecord{}, err
		}
		src = withRepo(src, repo)
	}
	if base.Verbose {
		if err := writeVerboseMarker(dir); err != nil {
			m.mu.Unlock()
			_ = os.RemoveAll(dir)
			m.log.Error("verbose marker not written", "run_id", id, "error", err.Error())
			return briefRecord{}, err
		}
	}
	rec := base
	rec.ID, rec.Title, rec.Phase, rec.StartedAt = id, b.Front.Title, phasePlanning, m.stamp()
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

// writeVerboseMarker makes the empty <dir>/_state/verbose file that tells the
// planner to keep prompt and reply text for the run.
func writeVerboseMarker(dir string) error {
	state := filepath.Join(dir, "_state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(state, "verbose"), nil, 0o644)
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
		// A run answered while this one held the slot continues now.
		go m.resumeAnswered()
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
	code, err = m.step(a, logf, "brief", "run", rec.ID, "--repo", m.repoFor(rec.ID))
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

var (
	frontIDLine   = regexp.MustCompile(`(?m)^id:[ \t]*.*$`)
	frontRepoLine = regexp.MustCompile(`(?m)^repo:[ \t]*.*$`)
)

// withRepo sets the repo line of the frontmatter to repo, adding it after the
// id line when the brief has none: the server, not the brief, says where a run
// builds.
func withRepo(src []byte, repo string) []byte {
	end := bytes.Index(src, []byte("\n---"))
	if end < 0 {
		end = len(src)
	}
	line := []byte("repo: " + repo)
	if loc := frontRepoLine.FindIndex(src[:end]); loc != nil {
		return append(append(append([]byte{}, src[:loc[0]]...), line...), src[loc[1]:]...)
	}
	if loc := frontIDLine.FindIndex(src[:end]); loc != nil {
		return append(append(append(append([]byte{}, src[:loc[1]]...), '\n'), line...), src[loc[1]:]...)
	}
	return src
}

// Errors the create and resubmit helpers return, so a caller (the HTTP
// handlers or the chat tools) can tell the cases apart.
var (
	errNoSuchRun     = errors.New("no such run")
	errNoStoredBrief = errors.New("no stored brief for this run")
	// errPreviewChanged: the stored version no longer matches the preview token.
	errPreviewChanged = errors.New("the brief changed since you previewed it, reload the preview")
)

// briefInvalidError is a brief that failed validation. extra is the JSON
// detail the HTTP reply carries (problems, repo_root), nil when none.
type briefInvalidError struct {
	msg   string
	extra map[string]any
}

func (e *briefInvalidError) Error() string { return e.msg }

// resubmitHandler handles POST /app/briefs/{id}/resubmit: the version of an
// earlier run starts again as a new run.
func (m *briefManager) resubmitHandler(w http.ResponseWriter, r *http.Request) {
	rec, warnings, err := m.resubmitWarn(r.PathValue("id"))
	m.startReply(w, rec, warnings, err)
}

// resubmitID starts the version of an earlier run again as a new run.
func (m *briefManager) resubmitID(id string) (briefRecord, error) {
	rec, _, err := m.resubmitWarn(id)
	return rec, err
}

func (m *briefManager) resubmitWarn(id string) (briefRecord, []brief.Problem, error) {
	if !runview.ValidRunID(id) {
		return briefRecord{}, nil, errNoSuchRun
	}
	id = m.resolveID(id)
	old, _ := m.readRecord(id)
	if old.Family != "" && old.Version != "" {
		return m.startVersion(old.Family, old.Version, old.Ask, old.Verbose, "")
	}
	// An older run with no library version: its stored text goes through the
	// same flow as an upload, which migrates it into the library.
	src, err := readSmallFile(filepath.Join(m.runDir(id), "brief.md"), maxBriefBytes)
	if err != nil {
		return briefRecord{}, nil, errNoStoredBrief
	}
	return m.createText(src, old.Ask, old.Verbose)
}

// startReply writes the response for create and resubmit: the error mapped to
// its status, or 202 with the new run.
func (m *briefManager) startReply(w http.ResponseWriter, rec briefRecord, warnings []brief.Problem, err error) {
	var busy *briefBusyError
	var invalid *briefInvalidError
	switch {
	case errors.Is(err, errNoSuchRun):
		briefError(w, http.StatusNotFound, "no such run", nil)
	case errors.Is(err, errNoStoredBrief):
		briefError(w, http.StatusNotFound, "no stored brief for this run", nil)
	case errors.Is(err, errPreviewChanged):
		briefError(w, http.StatusConflict, errPreviewChanged.Error(), nil)
	case errors.As(err, &invalid):
		briefError(w, http.StatusBadRequest, invalid.msg, invalid.extra)
	case errors.As(err, &busy):
		briefError(w, http.StatusConflict, "a run is already active", map[string]any{"active_run_id": busy.activeID})
	case errors.Is(err, errBriefExists): // can't happen: run ids are server-assigned and the counter skips used ones
		briefError(w, http.StatusConflict, "a run with this id already exists", map[string]any{"run_id": rec.ID})
	case err != nil:
		briefError(w, http.StatusInternalServerError, "cannot start the run", nil)
	default:
		reply := map[string]any{"run_id": rec.ID, "url": "/app/briefs/" + rec.ID}
		if len(warnings) > 0 {
			reply["warnings"] = warnings
		}
		briefJSON(w, http.StatusAccepted, reply)
	}
}

// create handles POST /app/briefs. A ?resubmit=1 query from older clients is
// ignored: run ids are always server-assigned.
func (m *briefManager) create(w http.ResponseWriter, r *http.Request) {
	src, code, msg := readBriefBody(w, r)
	if code != 0 {
		briefError(w, code, msg, nil)
		return
	}
	rec, warnings, err := m.createText(src, r.URL.Query().Get("ask") == "1", r.URL.Query().Get("verbose") == "1")
	m.startReply(w, rec, warnings, err)
}

// invalidFromProblems is the refusal for a brief whose Check list is not
// empty: the first problem plus a count, with the full list attached.
func invalidFromProblems(probs []brief.Problem) *briefInvalidError {
	summary := "invalid brief: " + probs[0].String()
	if n := len(probs) - 1; n > 0 {
		summary += fmt.Sprintf(" (and %d more)", n)
	}
	return &briefInvalidError{summary, map[string]any{"problems": probs}}
}

// createText validates a brief and starts its run through the library: a new
// title saves v1, text identical to a stored version reuses it, and changed
// text is saved as a child of the newest version. Invalid briefs come back as
// *briefInvalidError, a busy slot as *briefBusyError, and a used run id as
// errBriefExists (which cannot happen: ids are server-assigned).
func (m *briefManager) createText(src []byte, ask, verbose bool) (briefRecord, []brief.Problem, error) {
	src = bytes.TrimPrefix(src, []byte("\xef\xbb\xbf"))
	stripped := stripFront(src)
	// Validate a provisionally stamped copy, so invalid briefs are refused
	// with the same messages as before.
	prov := stampFront(stripped, "provisional-v1-"+m.now().UTC().Format("20060102")+"-r001", m.root)
	if probs := brief.Check(prov); len(probs) > 0 {
		return briefRecord{}, nil, invalidFromProblems(probs)
	}
	b, err := brief.Parse(prov)
	if err != nil {
		return briefRecord{}, nil, &briefInvalidError{"invalid brief: " + err.Error(), nil}
	}
	family, err := library.Slug(b.Front.Title)
	if err != nil {
		return briefRecord{}, nil, &briefInvalidError{"invalid brief: title cannot name a family: " + err.Error(), nil}
	}
	version, err := m.saveVersion(family, stripped)
	if err != nil {
		return briefRecord{}, nil, err
	}
	return m.startVersion(family, version, ask, verbose, "")
}

// saveVersion returns the version of family holding exactly text: an existing
// identical one, a first v1, or a child of the newest version.
func (m *briefManager) saveVersion(family string, text []byte) (string, error) {
	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	if v, ok := m.lib.FindIdentical(family, text); ok {
		return v.Name, nil
	}
	fams, err := m.lib.List()
	if err != nil {
		return "", err
	}
	newest, created := "", ""
	for _, f := range fams {
		if f.Name != family {
			continue
		}
		for _, v := range f.Versions {
			if v.Created >= created {
				newest, created = v.Name, v.Created
			}
		}
	}
	v, err := m.lib.Save(family, newest, "", text)
	if err != nil {
		return "", err
	}
	return v.Name, nil
}

// stopHandler handles POST /app/briefs/{id}/stop.
func (m *briefManager) stopHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !runview.ValidRunID(id) {
		briefError(w, http.StatusNotFound, "no such run", nil)
		return
	}
	id = m.resolveID(id)
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
