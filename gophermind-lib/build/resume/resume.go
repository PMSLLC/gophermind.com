// Package resume re-enters a finished run at a chosen planner stage. It keeps
// the brief, the requirements, the call ledger, saved replies, events, logs
// and every earlier stage's output, and moves aside (never deletes) the named
// stage's output, every later stage's output, the executor's files and the
// planner's status marks into _state/discarded/<timestamp>/, so the planner
// runs from that stage again and the discarded files stay readable.
package resume

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gophermind/gophermind-lib/build/blackboard"
	"gophermind/gophermind-lib/build/planner"
	"gophermind/gophermind-lib/build/runfs"
)

const (
	executorStatePath = "_state/executor.json"
	leafResultsPath   = "_state/leaf-results.json"
)

// unsafeName is the sanitizer the human gate uses for ESCALATION-<node>.md.
var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// Stages are the planner stages in pipeline order.
var Stages = []string{"clarify", "confirm", "contract", "decompose", "enrich", "coverage", "approve", "testwriter"}

// Step is where to resume: a planner stage, or one node.
type Step struct {
	Stage string
	Node  string
}

// ParseStep reads a stage name or node:<id>.
func ParseStep(s string) (Step, error) {
	for _, st := range Stages {
		if s == st {
			return Step{Stage: s}, nil
		}
	}
	if id := strings.TrimPrefix(s, "node:"); id != s && id != "" {
		return Step{Node: id}, nil
	}
	return Step{}, fmt.Errorf("resume: unknown step %q (a planner stage or node:<id>)", s)
}

// Result is what a resume moved.
type Result struct {
	Discarded  []string // run-relative paths moved, plus files written into DiscardDir (leaf-result and attempts copies), sorted
	DiscardDir string   // run-relative folder they went to
}

func stageIndex(stage string) int {
	for i, s := range Stages {
		if s == stage {
			return i
		}
	}
	return -1
}

func present(runDir, rel string) bool {
	full, err := inside(runDir, rel)
	if err != nil {
		return false
	}
	_, err = os.Lstat(full)
	return err == nil
}

// FromStage moves aside the output of stage and every later stage, the
// executor's files and the planner's error and done marks.
func FromStage(runDir, stage string, now time.Time) (Result, error) {
	idx := stageIndex(stage)
	if idx < 0 {
		return Result{}, fmt.Errorf("resume: unknown step %q (a planner stage or node:<id>)", stage)
	}
	last := -1
	for i, s := range Stages {
		if present(runDir, stageFiles(s)[0]) {
			last = i
		}
	}
	if last < 0 {
		return Result{}, fmt.Errorf("resume: nothing to resume; the run never completed a stage")
	}
	if !present(runDir, stageFiles(stage)[0]) {
		return Result{}, fmt.Errorf("resume: stage %q was never reached; the run completed %q", stage, Stages[last])
	}
	var rels []string
	for _, s := range Stages[idx:] {
		rels = append(rels, stageFiles(s)...)
		if s == "decompose" {
			nodes, err := nodePlanPaths(runDir)
			if err != nil {
				return Result{}, err
			}
			rels = append(rels, nodes...)
		}
	}
	rels = append(rels, executorFiles...)
	rels = append(rels, "_state/planner_error.json")
	rt, err := runtimePaths(runDir)
	if err != nil {
		return Result{}, err
	}
	rels = append(rels, rt...)
	dest := discardDir(runDir, now)
	moved, err := moveAll(runDir, dest, rels)
	res := Result{Discarded: moved, DiscardDir: dest}
	if err != nil {
		return res, err
	}
	return res, planner.ClearPlanned(runDir)
}

// FromNode re-enters a finished run at one executor node. It refuses while any
// node is claimed or in progress, and refuses a node that is verified or has not
// run. Otherwise it first saves the row's status, result and attempts to
// <discard dir>/attempts-<node>.json and the node's leaf-results.json entry to
// <discard dir>/leaf-result-<node>.json, then resets the row to ready, drops the
// node from leaf-results.json, and moves report.json, _state/project.json and the node's escalation file aside.
func FromNode(ctx context.Context, runDir string, board blackboard.Blackboard, runID, node string, now time.Time) (Result, error) {
	if !present(runDir, executorStatePath) {
		return Result{}, fmt.Errorf("resume: the run never reached the executor")
	}
	rows, err := board.List(ctx, runID, blackboard.Filter{})
	if err != nil {
		return Result{}, err
	}
	for _, r := range rows {
		if r.Status == blackboard.StatusClaimed || r.Status == blackboard.StatusInProgress {
			return Result{}, fmt.Errorf("resume: the run is active (node %s is claimed)", r.NodeID)
		}
	}
	row, err := board.Get(ctx, runID, node)
	if err != nil {
		return Result{}, fmt.Errorf("resume: node %s: %w", node, err)
	}
	switch row.Status {
	case blackboard.StatusVerified:
		return Result{}, fmt.Errorf("resume: %s is verified; a verified node cannot be rerun in this round", node)
	case blackboard.StatusFailed, blackboard.StatusEscalated, blackboard.StatusNeedsRevision:
	default:
		return Result{}, fmt.Errorf("resume: %s has not run yet; resume the run without --from", node)
	}
	dest := discardDir(runDir, now)
	attemptsRel := dest + "/attempts-" + unsafeName.ReplaceAllString(node, "_") + ".json"
	attemptsPath, err := inside(runDir, attemptsRel)
	if err != nil {
		return Result{}, err
	}
	if _, err := os.Lstat(attemptsPath); err == nil {
		return Result{}, fmt.Errorf("resume: %s already exists; not overwriting", attemptsRel)
	}
	leaf, hadLeaf, err := readLeafResults(runDir)
	if err != nil {
		return Result{}, err
	}

	// Copies first: nothing is changed until what it would lose is saved in the discard dir.
	safe := unsafeName.ReplaceAllString(node, "_")
	snap, err := json.MarshalIndent(struct {
		Status   blackboard.Status    `json:"status"`
		Result   *blackboard.Result   `json:"result"`
		Attempts []blackboard.Attempt `json:"attempts"`
	}{row.Status, row.Result, nonNilAttempts(row.Attempts)}, "", "  ")
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(attemptsPath), 0o700); err != nil {
		return Result{}, err
	}
	if err := runfs.WriteFileAtomic(attemptsPath, append(snap, '\n')); err != nil {
		return Result{}, err
	}
	written := []string{attemptsRel}
	if raw, ok := leaf[node]; ok {
		rel := dest + "/leaf-result-" + safe + ".json"
		full, err := inside(runDir, rel)
		if err != nil {
			return Result{Discarded: written, DiscardDir: dest}, err
		}
		if err := runfs.WriteFileAtomic(full, append([]byte(raw), '\n')); err != nil {
			return Result{Discarded: written, DiscardDir: dest}, err
		}
		written = append(written, rel)
	}
	if _, err := board.Reset(ctx, runID, node); err != nil {
		return Result{Discarded: written, DiscardDir: dest}, err
	}
	if hadLeaf {
		delete(leaf, node)
		if err := writeLeafResults(runDir, leaf); err != nil {
			return Result{Discarded: written, DiscardDir: dest}, err
		}
	}
	rels := []string{"report.json", "_state/project.json", "ESCALATION-" + safe + ".md"}
	moved, err := moveAll(runDir, dest, rels)
	all := append(moved, written...)
	sort.Strings(all)
	return Result{Discarded: all, DiscardDir: dest}, err
}

func readLeafResults(runDir string) (map[string]json.RawMessage, bool, error) {
	full, err := inside(runDir, leafResultsPath)
	if err != nil {
		return nil, false, err
	}
	data, err := os.ReadFile(full)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	m := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, false, fmt.Errorf("resume: %s: %w", leafResultsPath, err)
	}
	return m, true, nil
}

// writeLeafResults rewrites leaf-results.json atomically.
func writeLeafResults(runDir string, m map[string]json.RawMessage) error {
	full, err := inside(runDir, leafResultsPath)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return runfs.WriteFileAtomic(full, append(data, '\n'))
}

func nonNilAttempts(a []blackboard.Attempt) []blackboard.Attempt {
	if a == nil {
		return []blackboard.Attempt{}
	}
	return a
}
