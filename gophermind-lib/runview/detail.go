package runview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"gophermind/gophermind-lib/build/blackboard"
	"gophermind/gophermind-lib/build/ledger"
	"gophermind/gophermind-lib/build/planner"
	"gophermind/gophermind-lib/build/report"
	"gophermind/gophermind-lib/build/runfs"
	"gophermind/gophermind-lib/build/tree"
)

func runtimeOf(r blackboard.Row) RuntimeDoc {
	d := RuntimeDoc{
		Status: string(r.Status), Revision: r.Revision, HeartbeatAt: ts(r.HeartbeatAt), UpdatedAt: ts(r.UpdatedAt),
		Attempts: make([]Attempt, 0, len(r.Attempts)),
	}
	if r.Claim != nil {
		d.ClaimedBy = r.Claim.Worker
	}
	for _, a := range r.Attempts {
		d.Attempts = append(d.Attempts, wireAttempt(a))
	}
	if r.Result != nil {
		res := wireResult(r.Result)
		d.Result = &res
	}
	return d
}

// Node is GET /pipeline/node: the node file as stored (all groups, embedded
// decisions), its runtime file, and its saved artifacts.
func (v *View) Node(id string) (NodeBody, error) {
	if !ValidNodeID(id) {
		return NodeBody{}, ErrBadID
	}
	t, err := tree.NewStore(v.dir).Load()
	if err != nil {
		return NodeBody{}, errors.New("runview: the plan cannot be read")
	}
	n, ok := t.Nodes[id]
	if !ok {
		return NodeBody{}, ErrNotFound
	}
	raw, err := n.Marshal()
	if err != nil {
		return NodeBody{}, errors.New("runview: the node cannot be encoded")
	}
	body := NodeBody{SchemaVersion: SchemaVersion, Node: raw, Artifacts: v.ArtifactNames(id)}
	row, err := v.bb.Get(context.Background(), v.runID, id)
	switch {
	case err == nil:
		rt := runtimeOf(row)
		body.Runtime = &rt
	case errors.Is(err, blackboard.ErrNotFound):
	default:
		v.warn("node " + id + ": the runtime file cannot be read")
	}
	return body, nil
}

// ArtifactNames lists the saved attempt folders of a node and which of the four
// servable files each holds. Links are not listed.
func (v *View) ArtifactNames(id string) []ArtifactEntry {
	out := []ArtifactEntry{}
	if !ValidNodeID(id) {
		return out
	}
	attempts := filepath.Join(v.dir, "attempts")
	base := filepath.Join(attempts, id)
	if !realDir(attempts) || !realDir(base) {
		return out
	}
	ents, err := os.ReadDir(base)
	if err != nil {
		return out
	}
	for _, e := range ents {
		n, err := strconv.Atoi(e.Name())
		if err != nil || n < 1 || strconv.Itoa(n) != e.Name() {
			continue
		}
		dir := filepath.Join(base, e.Name())
		if !realDir(dir) {
			continue
		}
		entry := ArtifactEntry{Attempt: n, Files: []string{}}
		for _, f := range artifactFiles {
			if realFile(filepath.Join(dir, f)) {
				entry.Files = append(entry.Files, f)
			}
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Attempt < out[j].Attempt })
	return out
}

// Artifact returns the text of one saved file (already scrubbed by the
// executor). id, attempt and file are validated; links are refused.
func (v *View) Artifact(id string, attempt int, file string) (string, error) {
	if !ValidNodeID(id) || attempt < 1 || !ValidArtifactName(file) {
		return "", ErrBadID
	}
	attempts := filepath.Join(v.dir, "attempts")
	nodeDir := filepath.Join(attempts, id)
	dir := filepath.Join(nodeDir, strconv.Itoa(attempt))
	if !realDir(attempts) || !realDir(nodeDir) || !realDir(dir) {
		return "", ErrNotFound
	}
	data, cut, err := readLimited(filepath.Join(dir, file), maxReadBytes)
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrBadID) {
		return "", err
	}
	if err != nil {
		return "", errors.New("runview: the artifact cannot be read")
	}
	if cut {
		data = append(data, []byte("\n[truncated by the server]\n")...)
	}
	return string(data), nil
}

// revisionDecisionsFile holds the decisions a person made during the build
// (retry, skip, stop on a revised leaf). The planner never writes them into the
// plan tree.
const revisionDecisionsFile = "_state/revisions/decisions.json"

// Decisions returns the root node's records (the planning conversation)
// followed by the build's revision decisions from
// _state/revisions/decisions.json, each source in its stored order. A damaged
// revision file adds a warning and is skipped.
func (v *View) Decisions() (DecisionsBody, error) {
	t, err := tree.NewStore(v.dir).Load()
	if err != nil {
		return DecisionsBody{}, errors.New("runview: the plan cannot be read")
	}
	out := DecisionsBody{SchemaVersion: SchemaVersion, Decisions: []json.RawMessage{}}
	for _, n := range t.Nodes {
		if n.Kind != tree.KindRoot {
			continue
		}
		arr, _ := docOf(n)["decisions"].([]any)
		for _, x := range arr {
			if b, err := json.Marshal(x); err == nil {
				out.Decisions = append(out.Decisions, b)
			}
		}
	}
	if v.exists(revisionDecisionsFile) {
		data, _, err := readLimited(filepath.Join(v.dir, filepath.FromSlash(revisionDecisionsFile)), maxReadBytes)
		var st struct {
			Decisions []json.RawMessage `json:"decisions"`
		}
		if err != nil || json.Unmarshal(data, &st) != nil {
			v.warn("revisions/decisions.json: it cannot be read")
		} else {
			for _, d := range st.Decisions {
				var m map[string]any
				if json.Unmarshal(d, &m) == nil && m != nil {
					out.Decisions = append(out.Decisions, d)
				}
			}
		}
	}
	return out, nil
}

// Report is GET /pipeline/report. A finished run returns report.json byte for
// byte (after it validates); a running one returns a partial report built from
// the call log and the runtime files with "complete": false.
func (v *View) Report() ([]byte, error) {
	if v.exists(report.FileName) {
		data, cut, err := readLimited(filepath.Join(v.dir, report.FileName), maxReadBytes)
		if err != nil || cut || !validReport(data) {
			return nil, errors.New("runview: report.json cannot be read")
		}
		return data, nil
	}
	return v.partialReport()
}

// validReport applies the checks report.Read makes (schema version, known
// fields, exit code against status) to bytes already read, so the bytes served
// are the bytes validated.
func validReport(data []byte) bool {
	var v struct {
		SchemaVersion int `json:"schema_version"`
	}
	if json.Unmarshal(data, &v) != nil || v.SchemaVersion < 1 || v.SchemaVersion > report.SchemaVersion {
		return false
	}
	var r report.Report
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if dec.Decode(&r) != nil {
		return false
	}
	return r.ExitCode == report.ExitCode(r.Status, r.StopReason)
}

// partialReport builds the report of a run still going. Revision and parallel
// totals (revised_leaves, rewritten_leaves, parallel) appear only in the final
// report the executor writes; the partial one never carries them.
func (v *View) partialReport() ([]byte, error) {
	ctx := context.Background()
	l := v.load(ctx)
	rows := make([]blackboard.Row, 0, len(l.rows))
	for _, r := range l.rows {
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].NodeID < rows[j].NodeID })
	calls, err := ledger.NewFS(runfs.Fixed(v.dir)).List(ctx, v.runID, ledger.Filter{})
	if err != nil {
		v.warn("calls.jsonl: it cannot be read")
		calls = nil
	}
	reqs, _ := planner.ReadRequirements(v.dir)
	cov, _ := planner.ReadCoverage(v.dir)
	status, reason := v.status()
	var started = v.now()
	var ex execFile
	if v.readJSON("_state/executor.json", &ex) {
		if at, err := runfs.ParseTS(ex.StartedAt); err == nil && !at.IsZero() {
			started = at
		}
	}
	waves := map[int]bool{}
	for _, n := range l.t.Nodes {
		if n.Kind == tree.KindFunction && n.Wave != nil {
			waves[*n.Wave] = true
		}
	}
	rep, err := report.Build(report.Input{
		RunID: v.runID, StartedAt: started, FinishedAt: v.now(), Status: status, StopReason: reason,
		Calls: calls, Rows: rows, Requirements: reqs, Coverage: cov, Waves: len(waves),
	})
	if err != nil {
		return nil, errors.New("runview: the partial report cannot be built")
	}
	incomplete := false
	return json.MarshalIndent(ReportBody{Report: rep, Complete: &incomplete}, "", "  ")
}
