package executor

import (
	"gophermind/gophermind-lib/briefv2/blackboard"
)

// stateLeafResults is where the terminal status and reason of every leaf
// that ended are kept. The blackboard row holds the status but has no field
// for the reason, and a resumed run must still report why a leaf failed.
const stateLeafResults = "_state/leaf-results.json"

// LeafResult is one leaf's terminal status (verified, failed or escalated)
// and its reason: a class from a fixed vocabulary, never text.
type LeafResult struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// LoadLeafResults reads _state/leaf-results.json: node id to its result. A
// missing file is an empty map; a damaged one is an error.
func LoadLeafResults(runDir string) (map[string]LeafResult, error) {
	res := map[string]LeafResult{}
	if _, err := readStateJSON(runDir, stateLeafResults, &res); err != nil {
		return nil, err
	}
	if res == nil {
		res = map[string]LeafResult{}
	}
	return res, nil
}

// setResult records the terminal status and reason of a leaf: in the state
// file (atomically, so a restart finds it) and in the report's memory. A
// verified leaf keeps no reason.
func (rc *runCtx) setResult(id string, status blackboard.Status, reason string) error {
	repMu.Lock()
	defer repMu.Unlock()
	res, err := LoadLeafResults(rc.o.RunDir)
	if err != nil {
		return err
	}
	res[id] = LeafResult{Status: string(status), Reason: reason}
	if err := writeStateJSON(rc.o.RunDir, stateLeafResults, res); err != nil {
		return err
	}
	if status == blackboard.StatusVerified || reason == "" {
		delete(rc.rep.reasons, id)
	} else {
		rc.rep.reasons[id] = reason
	}
	return nil
}

// loadReasons fills the report's reasons from the state file, so a run
// rebuilt from disk reports why each failed or escalated leaf ended.
func (rc *runCtx) loadReasons() error {
	res, err := LoadLeafResults(rc.o.RunDir)
	if err != nil {
		return err
	}
	for id, r := range res {
		if r.Reason != "" && r.Status != string(blackboard.StatusVerified) {
			rc.rep.reasons[id] = r.Reason
		}
	}
	return nil
}
