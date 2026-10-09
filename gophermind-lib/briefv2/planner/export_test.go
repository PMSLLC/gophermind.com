package planner

import (
	"context"
	"errors"
	"io"

	"gophermind/gophermind-lib/briefv2/contract"
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

// ReviseCheckEnv is the part of mergeEnv the tests set.
type ReviseCheckEnv struct {
	Class        string
	SecretNames  []string // declared secret names of the brief
	DecisionIDs  []string // settled question ids
	Secrets      []string // secret values, for the reply scan
	N            int
	TestsRewrite bool
}

// ParseReviseReply exposes parseReviseReply: the reply kind ("patch", "contract_change",
// "split"), the paragraph for the last two, and the error.
func ParseReviseReply(text string) (kind, paragraph string, err error) {
	rep, err := parseReviseReply(text)
	if err != nil {
		return "", "", err
	}
	return [...]string{"patch", "contract_change", "split"}[rep.kind], rep.paragraph, nil
}

// CheckRevisePatch runs parse, merge and checks 1 to 7 of the spec on orig.
// The error is a revise error: ReviseErrClass reads its class.
func CheckRevisePatch(orig map[string]any, reply string, contractsJSON []byte, env ReviseCheckEnv) (merged map[string]any, changed []string, err error) {
	c, err := contract.Load(contractsJSON)
	if err != nil {
		return nil, nil, err
	}
	rep, err := parseReviseReply(reply)
	if err != nil {
		return nil, nil, err
	}
	if rep.kind != kindPatch {
		return nil, nil, errors.New("not a patch")
	}
	merged, err = mergePatch(orig, rep.patch, c)
	if err != nil {
		return nil, nil, err
	}
	ge := groupEnv{Secrets: map[string]bool{}, Decisions: map[string]bool{}}
	for _, n := range env.SecretNames {
		ge.Secrets[n] = true
	}
	for _, id := range env.DecisionIDs {
		ge.Decisions[id] = true
	}
	changed, err = checkMerged(orig, merged, rep.patch, mergeEnv{Class: env.Class, Env: ge, Secrets: env.Secrets, N: env.N, TestsRewrite: env.TestsRewrite})
	return merged, changed, err
}

// ReviseErrClass is the class of an error returned by the revise checks, "" for any other error.
func ReviseErrClass(err error) string {
	var re *reviseErr
	if errors.As(err, &re) {
		return re.class
	}
	return ""
}
