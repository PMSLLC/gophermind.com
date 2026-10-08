package main

import (
	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
)

// runDirOf finds a run's folder through the run record the planner writes at
// Load (<config dir>/runs/<run-id>.json).
func runDirOf(runID string) (string, error) {
	rec, err := planner.LookupRun(runID)
	if err != nil {
		return "", err
	}
	return rec.RunDir, nil
}

// briefBackends returns the blackboard and the ledger over the run folders.
// They hold no open files, so there is nothing to close.
func briefBackends() (blackboard.Blackboard, ledger.Ledger) {
	return blackboard.NewFS(runDirOf), ledger.NewFS(runDirOf)
}
