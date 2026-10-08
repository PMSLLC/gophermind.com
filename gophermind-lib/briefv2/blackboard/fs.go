package blackboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gophermind/gophermind-lib/briefv2/runfs"
	"gophermind/gophermind-lib/briefv2/tree"
)

// RunDirFunc maps a run id to the run's folder.
type RunDirFunc func(runID string) (string, error)

// FS is the blackboard over files in the run folder. Each node has one
// runtime file beside its plan file (comp/fn-a.json has comp/fn-a.runtime.json);
// the file holds the node's status, claim, attempts and result. A change reads
// the file, edits it and writes it back under an exclusive lock file, so a
// claim has exactly one winner even across processes. Every change also
// appends one line to <run>/_state/events.jsonl, which Watch follows.
type FS struct {
	resolve RunDirFunc
	flat    bool
	now     func() time.Time
	poll    time.Duration
	wait    time.Duration
	stale   time.Duration

	mu    sync.Mutex
	paths map[string]map[string]string // run folder -> node id -> runtime file
}

var _ Blackboard = (*FS)(nil)

// FSOption configures NewFS.
type FSOption func(*FS)

// WithFlatLayout keeps every runtime file under <run>/nodes/<id>.runtime.json
// instead of beside the plan file. Tests use it for node ids that have no
// plan file; production never does.
func WithFlatLayout() FSOption { return func(f *FS) { f.flat = true } }

// NewFS returns the filesystem blackboard. resolve finds a run's folder.
func NewFS(resolve RunDirFunc, opts ...FSOption) *FS {
	f := &FS{
		resolve: resolve,
		now:     func() time.Time { return time.Now().UTC() },
		poll:    250 * time.Millisecond,
		wait:    10 * time.Second,
		stale:   30 * time.Second,
		paths:   map[string]map[string]string{},
	}
	for _, o := range opts {
		o(f)
	}
	return f
}

var nodeIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// The wire types use snake_case names, matching docs/briefv2/handoff/examples/runtime.
type fsClaim struct {
	Worker    string `json:"worker"`
	ClaimedAt string `json:"claimed_at"`
}

type fsAttempt struct {
	Model         string   `json:"model"`
	Provider      string   `json:"provider"`
	Revision      int      `json:"revision"`
	Order         int      `json:"order"`
	StartedAt     string   `json:"started_at"`
	DurationMS    int64    `json:"duration_ms"`
	Verdict       Verdict  `json:"verdict"`
	TestsPassed   int      `json:"tests_passed"`
	TestsTotal    int      `json:"tests_total"`
	FailedTests   []string `json:"failed_tests,omitempty"`
	FailureReason string   `json:"failure_reason,omitempty"`
	ReplySHA256   string   `json:"reply_sha256,omitempty"`
}

type fsResult struct {
	FilesChanged []string `json:"files_changed"`
	Commit       string   `json:"commit"`
}

type fsDoc struct {
	NodeID      string      `json:"node_id"`
	RunID       string      `json:"run_id"`
	Status      Status      `json:"status"`
	Revision    int         `json:"revision"`
	Wave        int         `json:"wave"`
	Claim       *fsClaim    `json:"claim,omitempty"`
	HeartbeatAt string      `json:"heartbeat_at,omitempty"`
	Attempts    []fsAttempt `json:"attempts"`
	Result      *fsResult   `json:"result,omitempty"`
	UpdatedAt   string      `json:"updated_at"`
}

func (d fsDoc) row() (Row, error) {
	r := Row{RunID: d.RunID, NodeID: d.NodeID, Status: d.Status, Revision: d.Revision}
	r.HeartbeatAt, _ = runfs.ParseTS(d.HeartbeatAt)
	r.UpdatedAt, _ = runfs.ParseTS(d.UpdatedAt)
	if d.Claim != nil {
		at, _ := runfs.ParseTS(d.Claim.ClaimedAt)
		r.Claim = &Claim{Worker: d.Claim.Worker, ClaimedAt: at}
	}
	for _, a := range d.Attempts {
		at, _ := runfs.ParseTS(a.StartedAt)
		r.Attempts = append(r.Attempts, Attempt{
			Model: a.Model, Provider: a.Provider, Revision: a.Revision, Order: a.Order, StartedAt: at,
			DurationMS: a.DurationMS, Verdict: a.Verdict, TestsPassed: a.TestsPassed, TestsTotal: a.TestsTotal,
			FailedTests: a.FailedTests, FailureReason: a.FailureReason, ReplySHA256: a.ReplySHA256,
		})
	}
	if d.Result != nil {
		r.Result = &Result{FilesChanged: d.Result.FilesChanged, Commit: d.Result.Commit}
	}
	return r, nil
}

func wireAttempt(a Attempt) fsAttempt {
	return fsAttempt{
		Model: a.Model, Provider: a.Provider, Revision: a.Revision, Order: a.Order, StartedAt: runfs.TS(a.StartedAt),
		DurationMS: a.DurationMS, Verdict: a.Verdict, TestsPassed: a.TestsPassed, TestsTotal: a.TestsTotal,
		FailedTests: a.FailedTests, FailureReason: a.FailureReason, ReplySHA256: a.ReplySHA256,
	}
}

func readDoc(path string) (fsDoc, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fsDoc{}, ErrNotFound
	}
	if err != nil {
		return fsDoc{}, err
	}
	var d fsDoc
	if err := json.Unmarshal(b, &d); err != nil {
		return fsDoc{}, fmt.Errorf("blackboard: damaged runtime file %s", filepath.Base(path))
	}
	return d, nil
}

func writeDoc(path string, d fsDoc) error {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return runfs.WriteFileAtomic(path, append(b, '\n'))
}

// runtimePath finds the run folder and the node's runtime file. In the tree
// layout the location comes from the plan files: a node stored at Path() has
// its runtime file at Path() with .json replaced by .runtime.json.
func (f *FS) runtimePath(runID, nodeID string) (runDir, path string, err error) {
	if !nodeIDRE.MatchString(nodeID) {
		return "", "", ErrNotFound
	}
	runDir, err = f.resolve(runID)
	if err != nil {
		return "", "", err
	}
	if f.flat {
		return runDir, filepath.Join(runDir, "nodes", nodeID+".runtime.json"), nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.paths[runDir][nodeID]; ok {
		return runDir, p, nil
	}
	t, err := tree.NewStore(runDir).Load()
	if err != nil {
		return "", "", fmt.Errorf("blackboard: reading the plan of run %s: %w", runID, err)
	}
	m := make(map[string]string, len(t.Nodes))
	for id, n := range t.Nodes {
		m[id] = filepath.Join(runDir, filepath.FromSlash(strings.TrimSuffix(n.Path(), ".json")+".runtime.json"))
	}
	f.paths[runDir] = m
	if p, ok := m[nodeID]; ok {
		return runDir, p, nil
	}
	return "", "", ErrNotFound
}

// mutate runs fn on the node's document under the node's lock. fn returns
// whether to write the document back and which event to record, if any.
func (f *FS) mutate(ctx context.Context, runID, nodeID string, fn func(d *fsDoc) (write bool, ev EventKind, err error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	runDir, path, err := f.runtimePath(runID, nodeID)
	if err != nil {
		return err
	}
	unlock, err := runfs.Lock(path+".lock", f.wait, f.stale)
	if err != nil {
		return err
	}
	defer unlock()
	d, err := readDoc(path)
	if err != nil {
		return err
	}
	write, ev, err := fn(&d)
	if err != nil || !write {
		return err
	}
	d.UpdatedAt = runfs.TS(f.now())
	if err := writeDoc(path, d); err != nil {
		return err
	}
	if ev != "" {
		f.emit(runDir, ev, runID, nodeID)
	}
	return nil
}

type eventLine struct {
	RunID  string `json:"run_id"`
	NodeID string `json:"node_id"`
	Kind   string `json:"kind"`
	At     string `json:"at"`
}

// emit records a change. A lost notification must never fail the write it describes.
func (f *FS) emit(runDir string, kind EventKind, runID, nodeID string) {
	b, err := json.Marshal(eventLine{RunID: runID, NodeID: nodeID, Kind: string(kind), At: runfs.TS(f.now())})
	if err != nil {
		return
	}
	_ = runfs.AppendLine(filepath.Join(runDir, "_state", "events.jsonl"), b)
}

func (f *FS) InitRun(ctx context.Context, runID string, nodeIDs []string, waves map[string]int) error {
	for _, id := range nodeIDs {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, path, err := f.runtimePath(runID, id)
		if err != nil {
			return err
		}
		unlock, err := runfs.Lock(path+".lock", f.wait, f.stale)
		if err != nil {
			return err
		}
		if _, serr := os.Stat(path); serr == nil {
			unlock()
			continue
		}
		werr := writeDoc(path, fsDoc{
			NodeID: id, RunID: runID, Status: StatusPending, Wave: waves[id],
			Attempts: []fsAttempt{}, UpdatedAt: runfs.TS(f.now()),
		})
		unlock()
		if werr != nil {
			return werr
		}
	}
	return nil
}

func (f *FS) Get(ctx context.Context, runID, nodeID string) (Row, error) {
	if err := ctx.Err(); err != nil {
		return Row{}, err
	}
	_, path, err := f.runtimePath(runID, nodeID)
	if err != nil {
		return Row{}, err
	}
	d, err := readDoc(path)
	if err != nil {
		return Row{}, err
	}
	return d.row()
}

func (f *FS) List(ctx context.Context, runID string, flt Filter) ([]Row, error) {
	runDir, err := f.resolve(runID)
	if err != nil {
		return nil, err
	}
	var rows []Row
	err = filepath.WalkDir(runDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if d.IsDir() {
			// Directly under the run folder these hold run artifacts, never
			// node runtime files.
			if filepath.Dir(p) == runDir && (d.Name() == "_state" || tree.IsReservedComponentID(d.Name())) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".runtime.json") {
			return nil
		}
		doc, err := readDoc(p)
		if err != nil {
			return err
		}
		if doc.RunID != runID {
			return nil
		}
		if len(flt.Statuses) > 0 {
			match := false
			for _, s := range flt.Statuses {
				if s == doc.Status {
					match = true
				}
			}
			if !match {
				return nil
			}
		}
		if flt.Wave != nil && doc.Wave != *flt.Wave {
			return nil
		}
		r, err := doc.row()
		if err != nil {
			return err
		}
		rows = append(rows, r)
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].NodeID < rows[j].NodeID })
	return rows, nil
}

func (f *FS) Claim(ctx context.Context, runID, nodeID, worker string) (bool, error) {
	won := false
	err := f.mutate(ctx, runID, nodeID, func(d *fsDoc) (bool, EventKind, error) {
		if d.Status != StatusReady {
			return false, "", nil
		}
		now := runfs.TS(f.now())
		d.Status = StatusClaimed
		d.Claim = &fsClaim{Worker: worker, ClaimedAt: now}
		d.HeartbeatAt = now
		won = true
		return true, EventStatus, nil
	})
	if errors.Is(err, ErrNotFound) {
		return false, nil // a lost race and a missing row both read as "not yours"
	}
	return won, err
}

func owns(d *fsDoc, worker string) bool {
	return d.Claim != nil && d.Claim.Worker == worker && (d.Status == StatusClaimed || d.Status == StatusInProgress)
}

func (f *FS) Heartbeat(ctx context.Context, runID, nodeID, worker string) error {
	return f.mutate(ctx, runID, nodeID, func(d *fsDoc) (bool, EventKind, error) {
		if !owns(d, worker) {
			return false, "", ErrNotOwner
		}
		d.HeartbeatAt = runfs.TS(f.now())
		return true, "", nil
	})
}

func (f *FS) Release(ctx context.Context, runID, nodeID, worker string) error {
	return f.mutate(ctx, runID, nodeID, func(d *fsDoc) (bool, EventKind, error) {
		if !owns(d, worker) {
			return false, "", ErrNotOwner
		}
		d.Status, d.Claim, d.HeartbeatAt = StatusReady, nil, ""
		return true, EventStatus, nil
	})
}

func (f *FS) SetStatus(ctx context.Context, runID, nodeID string, from, to Status) error {
	if !transitionAllowed(from, to) {
		return ErrBadTransition
	}
	return f.mutate(ctx, runID, nodeID, func(d *fsDoc) (bool, EventKind, error) {
		if d.Status != from {
			return false, "", ErrBadTransition
		}
		d.Status = to
		if to != StatusClaimed && to != StatusInProgress {
			d.Claim, d.HeartbeatAt = nil, ""
		}
		return true, EventStatus, nil
	})
}

func (f *FS) SetRevision(ctx context.Context, runID, nodeID string, revision int) error {
	return f.mutate(ctx, runID, nodeID, func(d *fsDoc) (bool, EventKind, error) {
		d.Revision = revision
		return true, "", nil
	})
}

func (f *FS) AppendAttempt(ctx context.Context, runID, nodeID string, a Attempt) error {
	return f.mutate(ctx, runID, nodeID, func(d *fsDoc) (bool, EventKind, error) {
		d.Attempts = append(d.Attempts, wireAttempt(a))
		return true, EventAttempt, nil
	})
}

func (f *FS) SetResult(ctx context.Context, runID, nodeID string, r Result) error {
	return f.mutate(ctx, runID, nodeID, func(d *fsDoc) (bool, EventKind, error) {
		d.Result = &fsResult{FilesChanged: r.FilesChanged, Commit: r.Commit}
		return true, EventResult, nil
	})
}

func (f *FS) ReleaseStale(ctx context.Context, runID string, olderThan time.Duration) ([]string, error) {
	cutoff := f.now().Add(-olderThan)
	held, err := f.List(ctx, runID, Filter{Statuses: []Status{StatusClaimed, StatusInProgress}})
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, r := range held {
		if !r.HeartbeatAt.Before(cutoff) {
			continue
		}
		released := false
		err := f.mutate(ctx, runID, r.NodeID, func(d *fsDoc) (bool, EventKind, error) {
			hb, _ := runfs.ParseTS(d.HeartbeatAt)
			if (d.Status != StatusClaimed && d.Status != StatusInProgress) || !hb.Before(cutoff) {
				return false, "", nil // changed since we listed it
			}
			d.Status, d.Claim, d.HeartbeatAt = StatusReady, nil, ""
			released = true
			return true, EventStatus, nil
		})
		if err != nil {
			return nil, err
		}
		if released {
			ids = append(ids, r.NodeID)
		}
	}
	return ids, nil
}

func (f *FS) Watch(ctx context.Context, runID string) (<-chan Event, error) {
	runDir, err := f.resolve(runID)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(runDir, "_state", "events.jsonl")
	off := runfs.Size(path)
	ch := make(chan Event)
	go func() {
		defer close(ch)
		tick := time.NewTicker(f.poll)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			lines, next, err := runfs.ReadLines(path, off)
			if err != nil {
				continue // transient; try again on the next tick
			}
			off = next
			for _, ln := range lines {
				var e eventLine
				if json.Unmarshal(ln, &e) != nil || e.RunID != runID {
					continue
				}
				row, err := f.Get(ctx, runID, e.NodeID)
				if err != nil {
					continue
				}
				at, _ := runfs.ParseTS(e.At)
				select {
				case ch <- Event{Kind: EventKind(e.Kind), Row: row, At: at}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return ch, nil
}

func (f *FS) Close() error { return nil }
