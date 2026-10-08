package planner

import (
	"context"
	"errors"
	"io"
)

// SetBeforeTestWrite installs a hook run after a reply is parsed and before
// its test file is checked and written, and returns the previous one.
func SetBeforeTestWrite(f func()) func() {
	old := beforeTestWrite
	beforeTestWrite = f
	return old
}

// ParseOutline exposes the outline parser to the external tests.
func ParseOutline(text, briefID string) (map[string]any, []Dependency, error) {
	return parseOutline(text, briefID)
}

// parseOutline reads a one-pass outline reply; only tests use it, the planner
// goes through mergeOutline.
func parseOutline(text, briefID string) (map[string]any, []Dependency, error) {
	doc, deps, _, _, err := mergeOutline(nil, nil, text, briefID, true)
	if err == nil && len(objects(doc["components"])) == 0 {
		return nil, nil, errors.New("contract outline lists no component")
	}
	return doc, deps, err
}

// MaxEmittedTextBytes exposes the cap on the declared-ids list to the external tests.
const MaxEmittedTextBytes = maxEmittedTextBytes

// SetDebugOut replaces where the debug dump notices go and returns the restore.
func SetDebugOut(w io.Writer) func() {
	old := debugOut
	debugOut = w
	return func() { debugOut = old }
}

// ApproveStage runs only the approve stage of an existing run, the way Run
// would reach it, so a test can hand it a run that is not in a state the
// earlier stages would let through.
func ApproveStage(p *Planner, runID string) error {
	ctx := context.Background()
	r, err := p.load(ctx, Options{RunID: runID})
	if err != nil {
		return err
	}
	return p.approve(ctx, r)
}
