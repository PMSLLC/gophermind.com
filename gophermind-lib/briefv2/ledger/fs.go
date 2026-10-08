package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gophermind/gophermind-lib/briefv2/runfs"
)

// RunDirFunc maps a run id to the run's folder.
type RunDirFunc func(runID string) (string, error)

// FS is the ledger over append-only files in the run folder:
// _state/calls.jsonl holds one JSON line per call and one per amend (an amend
// changes the outcome of an earlier call by id and is folded in when the file
// is read, so no line is ever rewritten); _state/calls.seq holds the last id
// handed out; _state/calls.lock serialises id assignment across processes.
type FS struct {
	resolve RunDirFunc
	wait    time.Duration
	stale   time.Duration
}

var _ Ledger = (*FS)(nil)

// NewFS returns the filesystem ledger. resolve finds a run's folder.
func NewFS(resolve RunDirFunc) *FS {
	return &FS{resolve: resolve, wait: 10 * time.Second, stale: 30 * time.Second}
}

// wire is one line of calls.jsonl. Kind "amend" lines carry only ID, Outcome
// and ErrorKind.
type wire struct {
	Kind             string `json:"kind"`
	ID               int64  `json:"id"`
	RunID            string `json:"run_id,omitempty"`
	At               string `json:"at,omitempty"`
	Stage            string `json:"stage,omitempty"`
	TaskType         string `json:"task_type,omitempty"`
	NodeClass        string `json:"node_class,omitempty"`
	NodeID           string `json:"node_id,omitempty"`
	Revision         int    `json:"revision,omitempty"`
	Scope            string `json:"scope,omitempty"`
	Tier             string `json:"tier,omitempty"`
	ChainPos         int    `json:"chain_pos,omitempty"`
	Provider         string `json:"provider,omitempty"`
	ModelRequested   string `json:"model_requested,omitempty"`
	ModelServed      string `json:"model_served,omitempty"`
	PromptTokens     int    `json:"prompt_tokens,omitempty"`
	CompletionTokens int    `json:"completion_tokens,omitempty"`
	PromptBytes      int    `json:"prompt_bytes,omitempty"`
	PromptSHA256     string `json:"prompt_sha256,omitempty"`
	ResponseBytes    int    `json:"response_bytes,omitempty"`
	ResponseSHA256   string `json:"response_sha256,omitempty"`
	DurationMS       int64  `json:"duration_ms,omitempty"`
	Outcome          string `json:"outcome"`
	ErrorKind        string `json:"error_kind,omitempty"`
	RetryAfterS      int    `json:"retry_after_s,omitempty"`
}

func toWire(c Call) wire {
	return wire{
		Kind: "call", ID: c.ID, RunID: c.RunID, At: runfs.TS(c.At), Stage: c.Stage, TaskType: c.TaskType, NodeClass: c.NodeClass,
		NodeID: c.NodeID, Revision: c.Revision, Scope: c.Scope, Tier: c.Tier, ChainPos: c.ChainPos, Provider: c.Provider,
		ModelRequested: c.ModelRequested, ModelServed: c.ModelServed, PromptTokens: c.PromptTokens, CompletionTokens: c.CompletionTokens,
		PromptBytes: c.PromptBytes, PromptSHA256: c.PromptSHA256, ResponseBytes: c.ResponseBytes, ResponseSHA256: c.ResponseSHA256,
		DurationMS: c.DurationMS, Outcome: string(c.Outcome), ErrorKind: c.ErrorKind, RetryAfterS: c.RetryAfterS,
	}
}

func (w wire) call() Call {
	at, _ := runfs.ParseTS(w.At)
	return Call{
		ID: w.ID, RunID: w.RunID, At: at, Stage: w.Stage, TaskType: w.TaskType, NodeClass: w.NodeClass, NodeID: w.NodeID,
		Revision: w.Revision, Scope: w.Scope, Tier: w.Tier, ChainPos: w.ChainPos, Provider: w.Provider,
		ModelRequested: w.ModelRequested, ModelServed: w.ModelServed, PromptTokens: w.PromptTokens, CompletionTokens: w.CompletionTokens,
		PromptBytes: w.PromptBytes, PromptSHA256: w.PromptSHA256, ResponseBytes: w.ResponseBytes, ResponseSHA256: w.ResponseSHA256,
		DurationMS: w.DurationMS, Outcome: Outcome(w.Outcome), ErrorKind: w.ErrorKind, RetryAfterS: w.RetryAfterS,
	}
}

func (f *FS) stateDir(runID string) (string, error) {
	dir, err := f.resolve(runID)
	if err != nil {
		return "", fmt.Errorf("ledger: %w", err)
	}
	return filepath.Join(dir, "_state"), nil
}

// nextID reads, bumps and stores the sequence. The sequence is written before
// the call line is appended: a crash between the two leaves a gap in the ids,
// never a duplicate.
func nextID(stateDir string) (int64, error) {
	p := filepath.Join(stateDir, "calls.seq")
	var last int64
	if b, err := os.ReadFile(p); err == nil {
		last, _ = strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	} else if !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	last++
	if err := runfs.WriteFileAtomic(p, []byte(strconv.FormatInt(last, 10)+"\n")); err != nil {
		return 0, err
	}
	return last, nil
}

func (f *FS) Record(ctx context.Context, c *Call) error {
	if c.RunID == "" || c.Stage == "" || c.Provider == "" || c.Outcome == "" {
		return errors.New("ledger: run, stage, provider and outcome are required")
	}
	if c.At.IsZero() {
		c.At = time.Now()
	}
	dir, err := f.stateDir(c.RunID)
	if err != nil {
		return err
	}
	unlock, err := runfs.Lock(filepath.Join(dir, "calls.lock"), f.wait, f.stale)
	if err != nil {
		return fmt.Errorf("ledger: %w", err)
	}
	defer unlock()
	id, err := nextID(dir)
	if err != nil {
		return fmt.Errorf("ledger: %w", err)
	}
	c.ID = id
	line, err := json.Marshal(toWire(*c))
	if err != nil {
		return fmt.Errorf("ledger: %w", err)
	}
	if err := runfs.AppendLine(filepath.Join(dir, "calls.jsonl"), line); err != nil {
		return fmt.Errorf("ledger: %w", err)
	}
	return nil
}

func (f *FS) Amend(ctx context.Context, runID string, id int64, o Outcome, errorKind string) error {
	dir, err := f.stateDir(runID)
	if err != nil {
		return err
	}
	unlock, err := runfs.Lock(filepath.Join(dir, "calls.lock"), f.wait, f.stale)
	if err != nil {
		return fmt.Errorf("ledger: %w", err)
	}
	defer unlock()
	calls, err := f.readAll(dir)
	if err != nil {
		return err
	}
	found := false
	for _, c := range calls {
		if c.ID == id {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("ledger: no call with id %d", id)
	}
	line, err := json.Marshal(wire{Kind: "amend", ID: id, Outcome: string(o), ErrorKind: errorKind})
	if err != nil {
		return fmt.Errorf("ledger: %w", err)
	}
	if err := runfs.AppendLine(filepath.Join(dir, "calls.jsonl"), line); err != nil {
		return fmt.Errorf("ledger: %w", err)
	}
	return nil
}

// readAll returns every call of the run with its amends folded in, in file order.
func (f *FS) readAll(stateDir string) ([]Call, error) {
	lines, _, err := runfs.ReadLines(filepath.Join(stateDir, "calls.jsonl"), 0)
	if err != nil {
		return nil, fmt.Errorf("ledger: %w", err)
	}
	var calls []Call
	index := map[int64]int{}
	for _, ln := range lines {
		if len(bytes.TrimSpace(ln)) == 0 {
			continue
		}
		var w wire
		if err := json.Unmarshal(ln, &w); err != nil {
			return nil, errors.New("ledger: calls.jsonl holds a line that is not JSON")
		}
		switch w.Kind {
		case "call":
			index[w.ID] = len(calls)
			calls = append(calls, w.call())
		case "amend":
			if i, ok := index[w.ID]; ok {
				calls[i].Outcome, calls[i].ErrorKind = Outcome(w.Outcome), w.ErrorKind
			}
		}
	}
	return calls, nil
}

func (f *FS) List(ctx context.Context, runID string, flt Filter) ([]Call, error) {
	dir, err := f.stateDir(runID)
	if err != nil {
		return nil, err
	}
	all, err := f.readAll(dir)
	if err != nil {
		return nil, err
	}
	var out []Call
	for _, c := range all {
		if c.RunID != runID ||
			(flt.Stage != "" && c.Stage != flt.Stage) ||
			(flt.TaskType != "" && c.TaskType != flt.TaskType) ||
			(flt.NodeID != "" && c.NodeID != flt.NodeID) ||
			(flt.Provider != "" && c.Provider != flt.Provider) ||
			(flt.Outcome != "" && c.Outcome != flt.Outcome) {
			continue
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.Before(out[j].At)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (f *FS) Summary(ctx context.Context, runID string) ([]ModelSummary, error) {
	calls, err := f.List(ctx, runID, Filter{})
	if err != nil {
		return nil, err
	}
	byKey := map[string]*ModelSummary{}
	for _, c := range calls {
		model := c.ModelServed
		if model == "" {
			model = c.ModelRequested
		}
		k := c.TaskType + "\x00" + c.NodeClass + "\x00" + c.Provider + "\x00" + model
		m := byKey[k]
		if m == nil {
			m = &ModelSummary{TaskType: c.TaskType, NodeClass: c.NodeClass, Provider: c.Provider, Model: model, Outcomes: map[Outcome]int{}}
			byKey[k] = m
		}
		m.Calls++
		m.PromptTokens += int64(c.PromptTokens)
		m.CompletionTokens += int64(c.CompletionTokens)
		m.TotalMS += c.DurationMS
		m.Outcomes[c.Outcome]++
	}
	out := make([]ModelSummary, 0, len(byKey))
	for _, m := range byKey {
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.TaskType != b.TaskType {
			return a.TaskType < b.TaskType
		}
		if a.NodeClass != b.NodeClass {
			return a.NodeClass < b.NodeClass
		}
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		return a.Model < b.Model
	})
	return out, nil
}
