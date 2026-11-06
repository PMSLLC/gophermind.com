// Package resume re-enters a finished run at a chosen planner stage. It keeps
// the brief, the requirements, the call ledger, saved replies, events, logs
// and every earlier stage's output, and moves aside (never deletes) the named
// stage's output, every later stage's output, the executor's files and the
// planner's status marks into _state/discarded/<timestamp>/, so the planner
// runs from that stage again and the discarded files stay readable.
package resume

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gophermind/gophermind-lib/build/planner"
)

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
	Discarded  []string // run-relative paths moved, sorted
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
