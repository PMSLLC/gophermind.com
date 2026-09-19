# plantree M1: tree store Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A crash-safe, file-based plan tree that reports its own next unfinished action, so a run can resume from disk without any conversation history.

**Architecture:** A new package `gophermind-lib/plantree`. Nodes are JSON files laid out as in the v4 spec (`plan.json`, `phases/phase-001/tasks/task-001/steps/step-001/meta.json`). Membership is the directory listing, so there are no index files. Writes take an exclusive flock and use temp-file-plus-rename. `NextActions` is derived from the committed state, never from a stored cursor.

**Tech Stack:** Go (module `gophermind/gophermind-lib`, go 1.25), standard library plus the existing `gophermind-lib/lockfile`.

**Spec:** `docs/superpowers/specs/2026-09-19-brief-workflow-design.md` (approved design), `docs/superpowers/specs/2026-09-19-assignments-tree-v4-spec.md` sections 4, 5.1, 7.1 (layout, envelope, statuses), and `docs/superpowers/plans/2026-09-19-brief-workflow-roadmap.md` (contracts later milestones rely on).

## Global Constraints

- All commands run from `/Users/jbrahy/OtherProjects/PMSLLC/gophermind.com/gophermind-lib`.
- Test command: `go test ./plantree/... -count=1`. Race check: `go test -race ./plantree/... -count=1`. Run `gofmt -w plantree` before every check, then `gofmt -l plantree` must print nothing. `go vet ./plantree/...` must be clean.
- Package `plantree` must NOT import `gophermind/gophermind-lib/phaseflow` (import cycle risk with M6).
- The only project import allowed is `gophermind/gophermind-lib/lockfile`.
- `schema_version` is `4`. Unknown JSON fields are rejected on decode.
- Ids use exactly three digits per segment: `phase-NNN.task-NNN.step-NNN`, numbers `001` to `999`.
- Steps are the only leaves. Structural nodes (plan, phase, task) have an empty `status` and a nil `work`.
- No em dashes and no emojis in code, comments or commit messages.
- Commit messages end with these two lines:
  `Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>`
  `Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr`

---

## File structure

| File | Responsibility |
|---|---|
| `plantree/ids.go` | id grammar, parent/child ids, relative paths, parent_ref |
| `plantree/node.go` | `Node`, `Status`, `Stage`, `Work`, `Validate`, `Encode`, `Decode` |
| `plantree/store.go` | `Repo`: `Open`, `Init`, `Create`, `Get`, `Children`, `Update`, `Walk` |
| `plantree/verify.go` | `Verify`: depends_on references resolve, no dependency cycles |
| `plantree/summary.go` | `Summarize`: leaf counts and derived structural status |
| `plantree/next.go` | `NextActions`: runnable and blocked actions from committed state |
| `plantree/*_test.go` | one test file per source file, plus `testhelp_test.go` |

---

### Task 1: Ids and paths

**Files:**
- Create: `gophermind-lib/plantree/ids.go`
- Test: `gophermind-lib/plantree/ids_test.go`

**Interfaces:**
- Produces:
  - `type Kind string` with `KindPlan`, `KindPhase`, `KindTask`, `KindStep`
  - `const RootID = "plan"`
  - `func ParseID(id string) (Kind, error)`
  - `func ParentID(id string) (string, error)` (returns `""` for the root)
  - `func ChildID(parent string, n int) (string, error)`
  - `func RelDir(id string) (string, error)` (slash-separated, relative to the plan dir)
  - `func MetaPath(id string) (string, error)`
  - `func ParentRef(id string) (string, error)` (returns `""` for the root)
  - `func ContainerDir(id string) (rel string, ok bool, err error)`
  - `var segmentRE *regexp.Regexp` (package-private, used by `store.go`)

- [ ] **Step 1: Write the failing test**

Create `plantree/ids_test.go`:

```go
package plantree

import "testing"

func TestParseID(t *testing.T) {
	good := map[string]Kind{
		"plan":                        KindPlan,
		"phase-001":                   KindPhase,
		"phase-001.task-002":          KindTask,
		"phase-001.task-002.step-999": KindStep,
	}
	for id, want := range good {
		got, err := ParseID(id)
		if err != nil || got != want {
			t.Errorf("ParseID(%q) = %q, %v; want %q", id, got, err, want)
		}
	}
	bad := []string{
		"", "phase-1", "phase-000", "task-001", "phase-001.step-001",
		"phase-001.task-001.step-001.substep-001", "phase-001.task-001.",
		"Phase-001", "phase-001.task-1000", "plan.phase-001",
	}
	for _, id := range bad {
		if _, err := ParseID(id); err == nil {
			t.Errorf("ParseID(%q) accepted an invalid id", id)
		}
	}
}

func TestParentID(t *testing.T) {
	cases := map[string]string{
		"plan":                        "",
		"phase-001":                   "plan",
		"phase-001.task-002":          "phase-001",
		"phase-001.task-002.step-003": "phase-001.task-002",
	}
	for id, want := range cases {
		got, err := ParentID(id)
		if err != nil || got != want {
			t.Errorf("ParentID(%q) = %q, %v; want %q", id, got, err, want)
		}
	}
}

func TestChildID(t *testing.T) {
	cases := []struct {
		parent string
		n      int
		want   string
	}{
		{"plan", 1, "phase-001"},
		{"phase-001", 12, "phase-001.task-012"},
		{"phase-001.task-002", 999, "phase-001.task-002.step-999"},
	}
	for _, c := range cases {
		got, err := ChildID(c.parent, c.n)
		if err != nil || got != c.want {
			t.Errorf("ChildID(%q, %d) = %q, %v; want %q", c.parent, c.n, got, err, c.want)
		}
	}
	if _, err := ChildID("phase-001.task-001.step-001", 1); err == nil {
		t.Error("a step must not have children")
	}
	if _, err := ChildID("plan", 0); err == nil {
		t.Error("sibling number 0 must be rejected")
	}
	if _, err := ChildID("plan", 1000); err == nil {
		t.Error("sibling number 1000 must be rejected")
	}
}

func TestPaths(t *testing.T) {
	const step = "phase-001.task-002.step-003"
	if got, _ := RelDir(step); got != "phases/phase-001/tasks/task-002/steps/step-003" {
		t.Errorf("RelDir(step) = %q", got)
	}
	if got, _ := RelDir(RootID); got != "" {
		t.Errorf("RelDir(plan) = %q, want empty", got)
	}
	if got, _ := MetaPath(RootID); got != "plan.json" {
		t.Errorf("MetaPath(plan) = %q", got)
	}
	if got, _ := MetaPath("phase-001"); got != "phases/phase-001/meta.json" {
		t.Errorf("MetaPath(phase) = %q", got)
	}
	refs := map[string]string{
		"plan":                        "",
		"phase-001":                   "../../plan.json",
		"phase-001.task-001":          "../../meta.json",
		"phase-001.task-001.step-001": "../../meta.json",
	}
	for id, want := range refs {
		if got, err := ParentRef(id); err != nil || got != want {
			t.Errorf("ParentRef(%q) = %q, %v; want %q", id, got, err, want)
		}
	}
	rel, ok, err := ContainerDir("phase-001.task-002")
	if err != nil || !ok || rel != "phases/phase-001/tasks/task-002/steps" {
		t.Errorf("ContainerDir(task) = %q, %v, %v", rel, ok, err)
	}
	rel, ok, err = ContainerDir(RootID)
	if err != nil || !ok || rel != "phases" {
		t.Errorf("ContainerDir(plan) = %q, %v, %v", rel, ok, err)
	}
	if _, ok, _ := ContainerDir(step); ok {
		t.Error("a step has no child container")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./plantree/... -count=1`
Expected: build FAIL with `undefined: ParseID` (and the other names).

- [ ] **Step 3: Write the implementation**

Create `plantree/ids.go`:

```go
// Package plantree is a file-based plan tree: phases, tasks and steps stored
// as JSON documents so a planning run can resume from disk with no
// conversation history. Layout and ids follow the v4 assignment-tree spec.
package plantree

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Kind is the level of a node in the tree.
type Kind string

const (
	KindPlan  Kind = "plan"
	KindPhase Kind = "phase"
	KindTask  Kind = "task"
	KindStep  Kind = "step"
)

// RootID is the reserved id of the root node.
const RootID = "plan"

// maxSiblings is the largest segment number; ids use exactly three digits.
const maxSiblings = 999

var (
	levelKinds = []Kind{KindPhase, KindTask, KindStep}
	segmentRE  = regexp.MustCompile(`^([a-z]+)-(\d{3})$`)
	// containers maps a parent kind to the directory holding its children.
	containers = map[Kind]string{KindPlan: "phases", KindPhase: "tasks", KindTask: "steps"}
)

// ParseID validates id and returns its kind.
func ParseID(id string) (Kind, error) {
	if id == RootID {
		return KindPlan, nil
	}
	segs := strings.Split(id, ".")
	if len(segs) > len(levelKinds) {
		return "", fmt.Errorf("plantree: id %q is too deep", id)
	}
	for i, s := range segs {
		m := segmentRE.FindStringSubmatch(s)
		if m == nil || Kind(m[1]) != levelKinds[i] {
			return "", fmt.Errorf("plantree: id %q: segment %q must be %s-NNN", id, s, levelKinds[i])
		}
		if n, _ := strconv.Atoi(m[2]); n < 1 {
			return "", fmt.Errorf("plantree: id %q: segment numbers start at 001", id)
		}
	}
	return levelKinds[len(segs)-1], nil
}

// ParentID returns the id of id's parent, or "" for the root.
func ParentID(id string) (string, error) {
	kind, err := ParseID(id)
	if err != nil {
		return "", err
	}
	switch kind {
	case KindPlan:
		return "", nil
	case KindPhase:
		return RootID, nil
	}
	return id[:strings.LastIndex(id, ".")], nil
}

// ChildID returns the id of the n-th child of parent.
func ChildID(parent string, n int) (string, error) {
	kind, err := ParseID(parent)
	if err != nil {
		return "", err
	}
	if n < 1 || n > maxSiblings {
		return "", fmt.Errorf("plantree: sibling number %d out of range 1..%d", n, maxSiblings)
	}
	var child Kind
	switch kind {
	case KindPlan:
		child = KindPhase
	case KindPhase:
		child = KindTask
	case KindTask:
		child = KindStep
	default:
		return "", fmt.Errorf("plantree: a step cannot have children")
	}
	seg := fmt.Sprintf("%s-%03d", child, n)
	if kind == KindPlan {
		return seg, nil
	}
	return parent + "." + seg, nil
}

// RelDir is the slash-separated directory of id relative to the plan
// directory. The root's directory is "".
func RelDir(id string) (string, error) {
	kind, err := ParseID(id)
	if err != nil {
		return "", err
	}
	if kind == KindPlan {
		return "", nil
	}
	parent := KindPlan
	var parts []string
	for i, seg := range strings.Split(id, ".") {
		parts = append(parts, containers[parent], seg)
		parent = levelKinds[i]
	}
	return path.Join(parts...), nil
}

// MetaPath is the slash-separated path of id's document relative to the plan
// directory.
func MetaPath(id string) (string, error) {
	rel, err := RelDir(id)
	if err != nil {
		return "", err
	}
	if id == RootID {
		return "plan.json", nil
	}
	return path.Join(rel, "meta.json"), nil
}

// ParentRef is the metadata-relative reference to id's parent document, or ""
// for the root (v4 spec section 4.3).
func ParentRef(id string) (string, error) {
	kind, err := ParseID(id)
	if err != nil {
		return "", err
	}
	switch kind {
	case KindPlan:
		return "", nil
	case KindPhase:
		return "../../plan.json", nil
	}
	return "../../meta.json", nil
}

// ContainerDir is the directory, relative to the plan directory, that holds
// id's children. ok is false for a step, which has none.
func ContainerDir(id string) (rel string, ok bool, err error) {
	kind, err := ParseID(id)
	if err != nil {
		return "", false, err
	}
	if kind == KindStep {
		return "", false, nil
	}
	dir, err := RelDir(id)
	if err != nil {
		return "", false, err
	}
	return path.Join(dir, containers[kind]), true, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./plantree/... -count=1 && gofmt -l plantree && go vet ./plantree/...`
Expected: `ok` for the package, `gofmt -l` prints nothing, vet is clean.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/plantree/ids.go gophermind-lib/plantree/ids_test.go
git commit -m "feat(plantree): id grammar and tree paths

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---

### Task 2: Node model, validation and codec

**Files:**
- Create: `gophermind-lib/plantree/node.go`
- Test: `gophermind-lib/plantree/node_test.go`

**Interfaces:**
- Consumes: `ParseID`, `ParentRef`, `KindStep` from Task 1.
- Produces:
  - `const SchemaVersion = 4`
  - `type Status string` with `StatusUntouched`, `StatusReviewed`, `StatusInProgress`, `StatusNeedsRevision`, `StatusCompleted`, `StatusBlocked`, `StatusDelayed`, `StatusSkipped`, `StatusFailed`, `StatusEscalated`
  - `type Stage string` with `StageSkeleton`, `StageInspected`, `StageDrafted`, `StageAwaitingAnswers`, `StageNeedsReconciliation`, `StageApproved`
  - `type Planning struct{ Stage Stage }`
  - `type Work struct{ Description string; TargetPaths, AcceptanceCriteria, TestCommand []string }`
  - `type Node struct{ SchemaVersion int; ID, Title string; NodeRevision int; ContextDigest string; ParentRef *string; DependsOn []string; Status Status; Reason string; Planning Planning; ResumeNote, Objective string; Work *Work }`
  - `func (n Node) Kind() Kind`
  - `func Validate(n Node) error`
  - `func Encode(n Node) ([]byte, error)`
  - `func Decode(b []byte) (Node, error)`

- [ ] **Step 1: Write the failing test**

Create `plantree/testhelp_test.go`:

```go
package plantree

import "testing"

// mk returns a valid skeleton node for id.
func mk(t *testing.T, id string) Node {
	t.Helper()
	kind, err := ParseID(id)
	if err != nil {
		t.Fatal(err)
	}
	ref, _ := ParentRef(id)
	n := Node{
		SchemaVersion: SchemaVersion,
		ID:            id,
		Title:         "title " + id,
		NodeRevision:  1,
		ContextDigest: "digest for " + id,
		DependsOn:     []string{},
		Planning:      Planning{Stage: StageSkeleton},
	}
	if ref != "" {
		n.ParentRef = &ref
	}
	if kind == KindStep {
		n.Status = StatusUntouched
	}
	return n
}

// draftedWork is a complete Work value.
func draftedWork() *Work {
	return &Work{
		Description:        "do the thing",
		TargetPaths:        []string{"a.go"},
		AcceptanceCriteria: []string{"it works"},
		TestCommand:        []string{"go", "test", "./..."},
	}
}
```

Create `plantree/node_test.go`:

```go
package plantree

import (
	"strings"
	"testing"
)

const step1 = "phase-001.task-001.step-001"

func TestValidateAcceptsSkeletons(t *testing.T) {
	for _, id := range []string{RootID, "phase-001", "phase-001.task-001", step1} {
		if err := Validate(mk(t, id)); err != nil {
			t.Errorf("Validate(skeleton %s): %v", id, err)
		}
	}
}

func TestValidateRejects(t *testing.T) {
	cases := map[string]func(*Node){
		"wrong schema version":       func(n *Node) { n.SchemaVersion = 3 },
		"empty title":                func(n *Node) { n.Title = " " },
		"zero revision":              func(n *Node) { n.NodeRevision = 0 },
		"empty digest":               func(n *Node) { n.ContextDigest = "" },
		"wrong parent_ref":           func(n *Node) { r := "../meta.json"; n.ParentRef = &r },
		"missing parent_ref":         func(n *Node) { n.ParentRef = nil },
		"self dependency":            func(n *Node) { n.DependsOn = []string{n.ID} },
		"duplicate dependency":       func(n *Node) { n.DependsOn = []string{"phase-001", "phase-001"} },
		"malformed dependency id":    func(n *Node) { n.DependsOn = []string{"nope"} },
		"unknown stage":              func(n *Node) { n.Planning.Stage = "done" },
		"unknown status":             func(n *Node) { n.Status = "finished" },
		"blocked without reason":     func(n *Node) { n.Status = StatusBlocked },
		"drafted without work":       func(n *Node) { n.Planning.Stage = StageDrafted },
		"drafted without criteria":   func(n *Node) { n.Planning.Stage = StageDrafted; n.Work = &Work{Description: "d"} },
		"drafted empty description":  func(n *Node) { n.Planning.Stage = StageDrafted; n.Work = &Work{AcceptanceCriteria: []string{"x"}} },
	}
	for name, mutate := range cases {
		n := mk(t, step1)
		mutate(&n)
		if err := Validate(n); err == nil {
			t.Errorf("%s: Validate accepted an invalid node", name)
		}
	}

	structural := map[string]func(*Node){
		"structural status": func(n *Node) { n.Status = StatusUntouched },
		"structural work":   func(n *Node) { n.Work = draftedWork() },
	}
	for name, mutate := range structural {
		n := mk(t, "phase-001")
		mutate(&n)
		if err := Validate(n); err == nil {
			t.Errorf("%s: Validate accepted an invalid structural node", name)
		}
	}

	root := mk(t, RootID)
	r := "../../plan.json"
	root.ParentRef = &r
	if err := Validate(root); err == nil {
		t.Error("root with a parent_ref must be rejected")
	}
}

func TestValidateAcceptsDraftedAndHeldSteps(t *testing.T) {
	n := mk(t, step1)
	n.Planning.Stage = StageDrafted
	n.Work = draftedWork()
	if err := Validate(n); err != nil {
		t.Errorf("drafted step: %v", err)
	}
	n.Status = StatusBlocked
	n.Reason = "waiting on a decision"
	if err := Validate(n); err != nil {
		t.Errorf("blocked step with reason: %v", err)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	n := mk(t, step1)
	n.Planning.Stage = StageDrafted
	n.Work = draftedWork()
	n.DependsOn = []string{"phase-001.task-001.step-002"}
	b, err := Encode(n)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(b)
	if err != nil {
		t.Fatalf("Decode: %v\n%s", err, b)
	}
	if got.ID != n.ID || got.Work == nil || got.Work.Description != "do the thing" || got.DependsOn[0] != n.DependsOn[0] {
		t.Errorf("round trip lost data: %+v", got)
	}
}

func TestEncodeWritesEmptyArraysNotNull(t *testing.T) {
	n := mk(t, step1)
	n.DependsOn = nil
	n.Work = &Work{Description: "d"}
	b, err := Encode(n)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, field := range []string{`"depends_on": []`, `"target_paths": []`, `"acceptance_criteria": []`, `"test_command": []`} {
		if !strings.Contains(s, field) {
			t.Errorf("encoded node missing %s:\n%s", field, s)
		}
	}
}

func TestDecodeRejects(t *testing.T) {
	if _, err := Decode([]byte(`{"schema_version":9}`)); err == nil || !strings.Contains(err.Error(), "schema_version 9") {
		t.Errorf("unsupported version: %v", err)
	}
	valid, _ := Encode(mk(t, RootID))
	withExtra := strings.Replace(string(valid), `"title"`, `"titel_typo": 1, "title"`, 1)
	if _, err := Decode([]byte(withExtra)); err == nil {
		t.Error("an unknown field must be rejected")
	}
	if _, err := Decode(append(valid, []byte(`{"x":1}`)...)); err == nil {
		t.Error("trailing data must be rejected")
	}
	if _, err := Decode([]byte(`not json`)); err == nil {
		t.Error("malformed JSON must be rejected")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./plantree/... -count=1`
Expected: build FAIL with `undefined: Node` (and the other names).

- [ ] **Step 3: Write the implementation**

Create `plantree/node.go`:

```go
package plantree

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// SchemaVersion is written into every node. It matches the v4 spec so later
// layers extend these documents rather than replace them.
const SchemaVersion = 4

// Status is a leaf's lifecycle state. Structural nodes derive theirs and
// store an empty status.
type Status string

const (
	StatusUntouched     Status = "untouched"
	StatusReviewed      Status = "reviewed"
	StatusInProgress    Status = "in_progress"
	StatusNeedsRevision Status = "needs_revision"
	StatusCompleted     Status = "completed"
	StatusBlocked       Status = "blocked"
	StatusDelayed       Status = "delayed"
	StatusSkipped       Status = "skipped"
	StatusFailed        Status = "failed"
	StatusEscalated     Status = "escalated"
)

var validStatuses = map[Status]bool{
	StatusUntouched: true, StatusReviewed: true, StatusInProgress: true,
	StatusNeedsRevision: true, StatusCompleted: true, StatusBlocked: true,
	StatusDelayed: true, StatusSkipped: true, StatusFailed: true, StatusEscalated: true,
}

// needsReason reports whether the status must carry a non-empty reason.
func (s Status) needsReason() bool {
	switch s {
	case StatusBlocked, StatusDelayed, StatusSkipped, StatusFailed, StatusEscalated, StatusNeedsRevision:
		return true
	}
	return false
}

// Stage is planning progress, separate from lifecycle status.
type Stage string

const (
	StageSkeleton            Stage = "skeleton"
	StageInspected           Stage = "inspected"
	StageDrafted             Stage = "drafted"
	StageAwaitingAnswers     Stage = "awaiting_answers"
	StageNeedsReconciliation Stage = "needs_reconciliation"
	StageApproved            Stage = "approved"
)

var validStages = map[Stage]bool{
	StageSkeleton: true, StageInspected: true, StageDrafted: true,
	StageAwaitingAnswers: true, StageNeedsReconciliation: true, StageApproved: true,
}

// Planning records planning progress for a node.
type Planning struct {
	Stage Stage `json:"stage"`
}

// Work is a step's generic specification.
type Work struct {
	Description        string   `json:"description"`
	TargetPaths        []string `json:"target_paths"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	TestCommand        []string `json:"test_command"`
}

// Node is one document in the tree. Its kind comes from its id.
type Node struct {
	SchemaVersion int      `json:"schema_version"`
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	NodeRevision  int      `json:"node_revision"`
	ContextDigest string   `json:"context_digest"`
	ParentRef     *string  `json:"parent_ref"`
	DependsOn     []string `json:"depends_on"`
	Status        Status   `json:"status"`
	Reason        string   `json:"reason"`
	Planning      Planning `json:"planning"`
	ResumeNote    string   `json:"resume_note"`
	Objective     string   `json:"objective"`
	Work          *Work    `json:"work"`
}

// Kind returns the node's level, or "" if its id is malformed.
func (n Node) Kind() Kind {
	k, _ := ParseID(n.ID)
	return k
}

// Validate checks a node's own fields. Cross-node rules (references
// resolving, cycles) belong to Repo.Verify.
func Validate(n Node) error {
	if n.SchemaVersion != SchemaVersion {
		return fmt.Errorf("plantree: %s: unsupported schema_version %d (want %d)", n.ID, n.SchemaVersion, SchemaVersion)
	}
	kind, err := ParseID(n.ID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(n.Title) == "" {
		return fmt.Errorf("plantree: %s: title is required", n.ID)
	}
	if n.NodeRevision < 1 {
		return fmt.Errorf("plantree: %s: node_revision must be at least 1", n.ID)
	}
	if strings.TrimSpace(n.ContextDigest) == "" {
		return fmt.Errorf("plantree: %s: context_digest is required", n.ID)
	}
	wantRef, err := ParentRef(n.ID)
	if err != nil {
		return err
	}
	switch {
	case wantRef == "" && n.ParentRef != nil:
		return fmt.Errorf("plantree: %s: parent_ref must be null at the root", n.ID)
	case wantRef != "" && (n.ParentRef == nil || *n.ParentRef != wantRef):
		return fmt.Errorf("plantree: %s: parent_ref must be %q", n.ID, wantRef)
	}
	seen := map[string]bool{}
	for _, d := range n.DependsOn {
		if _, err := ParseID(d); err != nil {
			return fmt.Errorf("plantree: %s: depends_on: %w", n.ID, err)
		}
		if d == n.ID {
			return fmt.Errorf("plantree: %s: depends on itself", n.ID)
		}
		if seen[d] {
			return fmt.Errorf("plantree: %s: duplicate dependency %s", n.ID, d)
		}
		seen[d] = true
	}
	if !validStages[n.Planning.Stage] {
		return fmt.Errorf("plantree: %s: unknown planning stage %q", n.ID, n.Planning.Stage)
	}
	if kind != KindStep {
		if n.Status != "" {
			return fmt.Errorf("plantree: %s: structural nodes derive their status; it must be empty", n.ID)
		}
		if n.Work != nil {
			return fmt.Errorf("plantree: %s: only steps carry work", n.ID)
		}
		return nil
	}
	if !validStatuses[n.Status] {
		return fmt.Errorf("plantree: %s: unknown status %q", n.ID, n.Status)
	}
	if n.Status.needsReason() && strings.TrimSpace(n.Reason) == "" {
		return fmt.Errorf("plantree: %s: status %s requires a reason", n.ID, n.Status)
	}
	if n.Planning.Stage == StageDrafted || n.Planning.Stage == StageApproved {
		return validateWork(n)
	}
	return nil
}

// validateWork requires a complete work spec once a step is drafted.
func validateWork(n Node) error {
	if n.Work == nil || strings.TrimSpace(n.Work.Description) == "" {
		return fmt.Errorf("plantree: %s: a %s step needs a work description", n.ID, n.Planning.Stage)
	}
	for _, c := range n.Work.AcceptanceCriteria {
		if strings.TrimSpace(c) != "" {
			return nil
		}
	}
	return fmt.Errorf("plantree: %s: a %s step needs at least one acceptance criterion", n.ID, n.Planning.Stage)
}

// Encode renders n as indented JSON with a trailing newline. Nil slices are
// written as empty arrays so readers never see null.
func Encode(n Node) ([]byte, error) {
	if n.DependsOn == nil {
		n.DependsOn = []string{}
	}
	if n.Work != nil {
		w := *n.Work
		if w.TargetPaths == nil {
			w.TargetPaths = []string{}
		}
		if w.AcceptanceCriteria == nil {
			w.AcceptanceCriteria = []string{}
		}
		if w.TestCommand == nil {
			w.TestCommand = []string{}
		}
		n.Work = &w
	}
	b, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Decode parses and validates a node. It rejects an unsupported schema
// version with an actionable message, unknown fields, and trailing data.
func Decode(b []byte) (Node, error) {
	var head struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return Node{}, fmt.Errorf("plantree: decode: %w", err)
	}
	if head.SchemaVersion != SchemaVersion {
		return Node{}, fmt.Errorf("plantree: unsupported schema_version %d (want %d)", head.SchemaVersion, SchemaVersion)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var n Node
	if err := dec.Decode(&n); err != nil {
		return Node{}, fmt.Errorf("plantree: decode: %w", err)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return Node{}, errors.New("plantree: decode: trailing data after the node")
	}
	if err := Validate(n); err != nil {
		return Node{}, err
	}
	return n, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./plantree/... -count=1 && gofmt -l plantree && go vet ./plantree/...`
Expected: `ok`, no gofmt output, vet clean.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/plantree/node.go gophermind-lib/plantree/node_test.go gophermind-lib/plantree/testhelp_test.go
git commit -m "feat(plantree): node model, validation and strict codec

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---

### Task 3: Repository (locked, optimistic, atomic)

**Files:**
- Create: `gophermind-lib/plantree/store.go`
- Test: `gophermind-lib/plantree/store_test.go`
- Modify: `gophermind-lib/plantree/testhelp_test.go` (add `newRepo`)

**Interfaces:**
- Consumes: everything from Tasks 1 and 2, plus `lockfile.Acquire(path string) (func(), error)` and `lockfile.WriteAtomic(path string, data []byte, perm os.FileMode) error`.
- Produces:
  - `var ErrNotFound, ErrExists error`
  - `type ConflictError struct{ ID string; Expected, Actual int }` (pointer receiver `Error`)
  - `type Repo struct`
  - `func Open(planningDir string) *Repo` (the plan directory is `<planningDir>/plan`)
  - `func (r *Repo) Init(root Node) error`
  - `func (r *Repo) Create(n Node) error`
  - `func (r *Repo) Get(id string) (Node, error)`
  - `func (r *Repo) Children(id string) ([]Node, error)`
  - `func (r *Repo) Update(id string, expectedRevision int, mutate func(*Node) error) (Node, error)`
  - `func (r *Repo) Walk(fn func(Node) error) error`

- [ ] **Step 1: Write the failing test**

Append to `plantree/testhelp_test.go`:

```go
// newRepo returns a repo holding plan, phase-001, task-001 and one skeleton
// step, plus the directory so a test can reopen it.
func newRepo(t *testing.T) (*Repo, string) {
	t.Helper()
	dir := t.TempDir()
	r := Open(dir)
	root := mk(t, RootID)
	root.Objective = "ship it"
	if err := r.Init(root); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"phase-001", "phase-001.task-001", "phase-001.task-001.step-001"} {
		if err := r.Create(mk(t, id)); err != nil {
			t.Fatalf("Create(%s): %v", id, err)
		}
	}
	return r, dir
}
```

Create `plantree/store_test.go`:

```go
package plantree

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRoundTripSurvivesReopen(t *testing.T) {
	_, dir := newRepo(t)
	r2 := Open(dir) // a new process would do exactly this
	got, err := r2.Get(step1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "title "+step1 || got.Status != StatusUntouched {
		t.Errorf("reopened node = %+v", got)
	}
	root, err := r2.Get(RootID)
	if err != nil || root.Objective != "ship it" {
		t.Errorf("root = %+v, %v", root, err)
	}
}

func TestInitTwiceFails(t *testing.T) {
	r, _ := newRepo(t)
	if err := r.Init(mk(t, RootID)); !errors.Is(err, ErrExists) {
		t.Errorf("second Init = %v, want ErrExists", err)
	}
}

func TestCreateRejects(t *testing.T) {
	r, _ := newRepo(t)
	if err := r.Create(mk(t, step1)); !errors.Is(err, ErrExists) {
		t.Errorf("duplicate Create = %v, want ErrExists", err)
	}
	if err := r.Create(mk(t, "phase-002.task-001")); !errors.Is(err, ErrNotFound) {
		t.Errorf("Create under a missing parent = %v, want ErrNotFound", err)
	}
	n := mk(t, "phase-001.task-001.step-002")
	n.NodeRevision = 2
	if err := r.Create(n); err == nil {
		t.Error("a new node must start at revision 1")
	}
	bad := mk(t, "phase-001.task-001.step-003")
	bad.Title = ""
	if err := r.Create(bad); err == nil {
		t.Error("an invalid node must be rejected")
	}
	if err := r.Create(mk(t, RootID)); err == nil {
		t.Error("Create must not accept the root")
	}
}

func TestGetMissing(t *testing.T) {
	r, _ := newRepo(t)
	if _, err := r.Get("phase-009"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(missing) = %v, want ErrNotFound", err)
	}
}

func TestUpdateBumpsRevisionAndPersists(t *testing.T) {
	r, dir := newRepo(t)
	got, err := r.Update(step1, 1, func(n *Node) error {
		n.ResumeNote = "half done"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.NodeRevision != 2 || got.ResumeNote != "half done" {
		t.Errorf("Update returned %+v", got)
	}
	again, _ := Open(dir).Get(step1)
	if again.NodeRevision != 2 || again.ResumeNote != "half done" {
		t.Errorf("persisted %+v", again)
	}
}

func TestUpdateRejectsStaleRevisionAndKeepsNewerData(t *testing.T) {
	r, _ := newRepo(t)
	if _, err := r.Update(step1, 1, func(n *Node) error { n.ResumeNote = "first"; return nil }); err != nil {
		t.Fatal(err)
	}
	_, err := r.Update(step1, 1, func(n *Node) error { n.ResumeNote = "second"; return nil })
	var ce *ConflictError
	if !errors.As(err, &ce) || ce.Expected != 1 || ce.Actual != 2 {
		t.Fatalf("stale Update = %v, want ConflictError{1,2}", err)
	}
	got, _ := r.Get(step1)
	if got.ResumeNote != "first" {
		t.Errorf("stale write overwrote newer data: %+v", got)
	}
}

func TestUpdateRejectsIdentityChangeAndInvalidResult(t *testing.T) {
	r, _ := newRepo(t)
	if _, err := r.Update(step1, 1, func(n *Node) error { n.ID = "phase-001.task-001.step-002"; return nil }); err == nil {
		t.Error("changing the id must be rejected")
	}
	if _, err := r.Update(step1, 1, func(n *Node) error { n.Status = StatusBlocked; return nil }); err == nil {
		t.Error("a blocked step without a reason must be rejected")
	}
	mutateErr := errors.New("boom")
	if _, err := r.Update(step1, 1, func(n *Node) error { return mutateErr }); !errors.Is(err, mutateErr) {
		t.Errorf("a mutate error must be returned, got %v", err)
	}
	got, _ := r.Get(step1)
	if got.NodeRevision != 1 {
		t.Errorf("a failed Update changed the node: %+v", got)
	}
}

func TestConcurrentUpdatesExactlyOneWins(t *testing.T) {
	r, _ := newRepo(t)
	const workers = 8
	var wg sync.WaitGroup
	var wins, conflicts int32
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := r.Update(step1, 1, func(n *Node) error {
				n.ResumeNote = fmt.Sprintf("worker %d", i)
				return nil
			})
			var ce *ConflictError
			switch {
			case err == nil:
				atomic.AddInt32(&wins, 1)
			case errors.As(err, &ce):
				atomic.AddInt32(&conflicts, 1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 || conflicts != workers-1 {
		t.Errorf("wins=%d conflicts=%d, want 1 and %d", wins, conflicts, workers-1)
	}
}

func TestChildrenNumericOrderAndMembership(t *testing.T) {
	r, dir := newRepo(t)
	for _, id := range []string{"phase-001.task-001.step-010", "phase-001.task-001.step-002"} {
		if err := r.Create(mk(t, id)); err != nil {
			t.Fatal(err)
		}
	}
	steps := filepath.Join(dir, "plan", "phases", "phase-001", "tasks", "task-001", "steps")
	if err := os.MkdirAll(filepath.Join(steps, "step-003"), 0o755); err != nil { // no meta.json
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(steps, "notes"), 0o755); err != nil { // stray directory
		t.Fatal(err)
	}
	kids, err := r.Children("phase-001.task-001")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, k := range kids {
		got = append(got, k.ID)
	}
	want := []string{"phase-001.task-001.step-001", "phase-001.task-001.step-002", "phase-001.task-001.step-010"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Children = %v, want %v", got, want)
	}
	if kids, err := r.Children(step1); err != nil || len(kids) != 0 {
		t.Errorf("Children(step) = %v, %v", kids, err)
	}
}

func TestWalkIsPreOrder(t *testing.T) {
	r, _ := newRepo(t)
	if err := r.Create(mk(t, "phase-002")); err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := r.Walk(func(n Node) error { got = append(got, n.ID); return nil }); err != nil {
		t.Fatal(err)
	}
	want := []string{"plan", "phase-001", "phase-001.task-001", step1, "phase-002"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Walk = %v, want %v", got, want)
	}
}

func TestReadRejectsHandEditedID(t *testing.T) {
	r, dir := newRepo(t)
	p := filepath.Join(dir, "plan", "phases", "phase-001", "tasks", "task-001", "steps", "step-001", "meta.json")
	other := mk(t, "phase-001.task-001.step-002")
	b, _ := Encode(other)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get(step1); err == nil {
		t.Error("a file holding a different id must be rejected")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./plantree/... -count=1`
Expected: build FAIL with `undefined: Repo` / `undefined: Open`.

- [ ] **Step 3: Write the implementation**

Create `plantree/store.go`:

```go
package plantree

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gophermind/gophermind-lib/lockfile"
)

var (
	// ErrNotFound is returned when a node does not exist.
	ErrNotFound = errors.New("plantree: node not found")
	// ErrExists is returned when creating a node that already exists.
	ErrExists = errors.New("plantree: node already exists")
)

// ConflictError reports an Update made against a stale revision.
type ConflictError struct {
	ID       string
	Expected int
	Actual   int
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("plantree: %s changed: expected revision %d, found %d", e.ID, e.Expected, e.Actual)
}

// Repo is the on-disk plan tree. Every mutation takes one exclusive
// cross-process lock and replaces files atomically, so a reader never sees a
// half-written node. Reads take no lock.
type Repo struct {
	dir string
}

// Open returns the repository whose tree lives in <planningDir>/plan.
func Open(planningDir string) *Repo {
	return &Repo{dir: filepath.Join(planningDir, "plan")}
}

func (r *Repo) metaPath(id string) (string, error) {
	rel, err := MetaPath(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(r.dir, filepath.FromSlash(rel)), nil
}

func (r *Repo) lock() (func(), error) {
	state := filepath.Join(r.dir, "_state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		return nil, err
	}
	return lockfile.Acquire(filepath.Join(state, "write.lock"))
}

func (r *Repo) read(id string) (Node, error) {
	p, err := r.metaPath(id)
	if err != nil {
		return Node{}, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return Node{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return Node{}, err
	}
	n, err := Decode(b)
	if err != nil {
		return Node{}, fmt.Errorf("%s: %w", p, err)
	}
	if n.ID != id {
		return Node{}, fmt.Errorf("plantree: %s holds id %q, expected %q", p, n.ID, id)
	}
	return n, nil
}

func (r *Repo) write(n Node) error {
	p, err := r.metaPath(n.ID)
	if err != nil {
		return err
	}
	b, err := Encode(n)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return lockfile.WriteAtomic(p, b, 0o644)
}

// Get returns the node with the given id.
func (r *Repo) Get(id string) (Node, error) { return r.read(id) }

// Init writes the root node. It fails with ErrExists if one is present.
func (r *Repo) Init(root Node) error {
	if root.ID != RootID {
		return fmt.Errorf("plantree: Init needs the root node, got %q", root.ID)
	}
	if err := Validate(root); err != nil {
		return err
	}
	unlock, err := r.lock()
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := r.read(RootID); err == nil {
		return fmt.Errorf("%w: %s", ErrExists, RootID)
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	return r.write(root)
}

// Create adds a new node. Its parent must exist and it must start at
// revision 1.
func (r *Repo) Create(n Node) error {
	if n.ID == RootID {
		return errors.New("plantree: use Init for the root")
	}
	if err := Validate(n); err != nil {
		return err
	}
	if n.NodeRevision != 1 {
		return fmt.Errorf("plantree: new node %s must start at revision 1", n.ID)
	}
	unlock, err := r.lock()
	if err != nil {
		return err
	}
	defer unlock()
	parent, err := ParentID(n.ID)
	if err != nil {
		return err
	}
	if _, err := r.read(parent); err != nil {
		return fmt.Errorf("plantree: parent of %s: %w", n.ID, err)
	}
	if _, err := r.read(n.ID); err == nil {
		return fmt.Errorf("%w: %s", ErrExists, n.ID)
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	return r.write(n)
}

// Update applies mutate to the node under the lock. It fails with a
// *ConflictError if the node's revision is not expectedRevision, so a stale
// caller can never overwrite newer data. id and parent_ref are immutable.
func (r *Repo) Update(id string, expectedRevision int, mutate func(*Node) error) (Node, error) {
	unlock, err := r.lock()
	if err != nil {
		return Node{}, err
	}
	defer unlock()
	cur, err := r.read(id)
	if err != nil {
		return Node{}, err
	}
	if cur.NodeRevision != expectedRevision {
		return Node{}, &ConflictError{ID: id, Expected: expectedRevision, Actual: cur.NodeRevision}
	}
	next := cur
	if err := mutate(&next); err != nil {
		return Node{}, err
	}
	if next.ID != cur.ID {
		return Node{}, fmt.Errorf("plantree: %s: id is immutable", id)
	}
	next.NodeRevision = cur.NodeRevision + 1
	if err := Validate(next); err != nil {
		return Node{}, err
	}
	if err := r.write(next); err != nil {
		return Node{}, err
	}
	return next, nil
}

// Children returns id's direct children in numeric order. A directory is a
// member only if it is correctly named and holds a document.
func (r *Repo) Children(id string) ([]Node, error) {
	rel, ok, err := ContainerDir(id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	entries, err := os.ReadDir(filepath.Join(r.dir, filepath.FromSlash(rel)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Node
	for _, e := range entries {
		if !e.IsDir() || !segmentRE.MatchString(e.Name()) {
			continue
		}
		childID := e.Name()
		if id != RootID {
			childID = id + "." + e.Name()
		}
		n, err := r.read(childID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// Walk visits every node in pre-order, siblings in numeric order.
func (r *Repo) Walk(fn func(Node) error) error {
	root, err := r.read(RootID)
	if err != nil {
		return err
	}
	return r.walk(root, fn)
}

func (r *Repo) walk(n Node, fn func(Node) error) error {
	if err := fn(n); err != nil {
		return err
	}
	kids, err := r.Children(n.ID)
	if err != nil {
		return err
	}
	for _, k := range kids {
		if err := r.walk(k, fn); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Run the tests, including the race detector**

Run: `go test ./plantree/... -count=1 && go test -race ./plantree/... -count=1 && gofmt -l plantree && go vet ./plantree/...`
Expected: `ok` both times, no gofmt output, vet clean.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/plantree/store.go gophermind-lib/plantree/store_test.go gophermind-lib/plantree/testhelp_test.go
git commit -m "feat(plantree): locked, revision-checked, atomic tree repository

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---

### Task 4: Verify (references and cycles)

**Files:**
- Create: `gophermind-lib/plantree/verify.go`
- Test: `gophermind-lib/plantree/verify_test.go`

**Interfaces:**
- Consumes: `(*Repo).Walk`, `Node.DependsOn`.
- Produces: `func (r *Repo) Verify() error` (nil when every `depends_on` id exists in the tree and the dependency graph is acyclic; otherwise an error naming the node or the cycle path).

- [ ] **Step 1: Write the failing test**

Create `plantree/verify_test.go`:

```go
package plantree

import (
	"strings"
	"testing"
)

func addStep(t *testing.T, r *Repo, id string, deps ...string) {
	t.Helper()
	n := mk(t, id)
	n.DependsOn = deps
	if err := r.Create(n); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyCleanTree(t *testing.T) {
	r, _ := newRepo(t)
	addStep(t, r, "phase-001.task-001.step-002", step1)
	if err := r.Verify(); err != nil {
		t.Errorf("Verify on a clean tree: %v", err)
	}
}

func TestVerifyMissingDependency(t *testing.T) {
	r, _ := newRepo(t)
	addStep(t, r, "phase-001.task-001.step-002", "phase-001.task-001.step-009")
	err := r.Verify()
	if err == nil || !strings.Contains(err.Error(), "step-009") {
		t.Errorf("Verify = %v, want an error naming the missing dependency", err)
	}
}

func TestVerifyDetectsCycleAndNamesPath(t *testing.T) {
	r, _ := newRepo(t)
	const a, b, c = "phase-001.task-001.step-002", "phase-001.task-001.step-003", "phase-001.task-001.step-004"
	addStep(t, r, a, c)
	addStep(t, r, b, a)
	addStep(t, r, c, b)
	err := r.Verify()
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("Verify = %v, want a cycle error", err)
	}
	for _, id := range []string{a, b, c} {
		if !strings.Contains(err.Error(), id) {
			t.Errorf("cycle error does not name %s: %v", id, err)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./plantree/... -run Verify -count=1`
Expected: build FAIL with `r.Verify undefined`.

- [ ] **Step 3: Write the implementation**

Create `plantree/verify.go`:

```go
package plantree

import (
	"fmt"
	"strings"
)

// Verify checks the rules that span nodes: every depends_on id exists, and
// the dependency graph has no cycle. Node-local rules are Validate's.
func (r *Repo) Verify() error {
	deps := map[string][]string{}
	var order []string
	err := r.Walk(func(n Node) error {
		deps[n.ID] = n.DependsOn
		order = append(order, n.ID)
		return nil
	})
	if err != nil {
		return err
	}
	for _, id := range order {
		for _, d := range deps[id] {
			if _, ok := deps[d]; !ok {
				return fmt.Errorf("plantree: %s depends on %s, which does not exist", id, d)
			}
		}
	}

	const (
		unseen = iota
		inStack
		done
	)
	state := map[string]int{}
	var stack []string
	var visit func(id string) error
	visit = func(id string) error {
		state[id] = inStack
		stack = append(stack, id)
		for _, d := range deps[id] {
			switch state[d] {
			case inStack:
				start := 0
				for i, s := range stack {
					if s == d {
						start = i
						break
					}
				}
				cycle := append(append([]string{}, stack[start:]...), d)
				return fmt.Errorf("plantree: dependency cycle: %s", strings.Join(cycle, " -> "))
			case unseen:
				if err := visit(d); err != nil {
					return err
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = done
		return nil
	}
	for _, id := range order {
		if state[id] == unseen {
			if err := visit(id); err != nil {
				return err
			}
		}
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./plantree/... -count=1 && gofmt -l plantree && go vet ./plantree/...`
Expected: `ok`, no gofmt output, vet clean.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/plantree/verify.go gophermind-lib/plantree/verify_test.go
git commit -m "feat(plantree): verify dependency references and cycles

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---

### Task 5: Summaries and NextActions (the resume path)

**Files:**
- Create: `gophermind-lib/plantree/summary.go`
- Create: `gophermind-lib/plantree/next.go`
- Test: `gophermind-lib/plantree/next_test.go`

**Interfaces:**
- Consumes: `(*Repo).Walk`, `(*Repo).Children`, `(*Repo).Get`, `(*Repo).Update`, stage and status constants.
- Produces:
  - `type Counts struct{ Skeleton, Inspected, Drafted, AwaitingAnswers, NeedsReconciliation, Approved, Held, Skipped int }`
  - `type Summary struct{ Leaves int; Counts Counts; Status Status }` (`Status` is `StatusReviewed` only when the subtree has leaves and every one is a step with status `reviewed` and stage `approved`; otherwise `StatusUntouched`)
  - `func (r *Repo) Summarize(id string) (Summary, error)`
  - `type ActionKind string` with `ActionDecompose`, `ActionReconcile`, `ActionDraft`, `ActionAnswer`, `ActionApprove`
  - `type Action struct{ Kind ActionKind; NodeID string; Reason string }`
  - `type Actions struct{ Runnable, Blocked []Action }`
  - `func (r *Repo) NextActions() (Actions, error)`

Rules for `NextActions` (derived from disk only, so a restart gets the same answer):
1. A structural node with no children (plan with no phases, phase with no tasks, task with no steps) yields `decompose` (runnable).
2. A step in stage `needs_reconciliation` yields `reconcile` (runnable).
3. A step in stage `skeleton` or `inspected` yields `draft` (runnable).
4. A step in stage `awaiting_answers` yields `answer` (blocked, needs a human).
5. Steps with status `blocked`, `delayed`, `escalated` or `skipped` yield nothing (an explicit hold).
6. Runnable order: all `reconcile`, then all `decompose`, then all `draft`, each in tree order.
7. `approve` (on the plan) is runnable only when there is nothing else runnable or blocked, the plan has leaves, and the plan is not already `reviewed`.

- [ ] **Step 1: Write the failing test**

Create `plantree/next_test.go`:

```go
package plantree

import (
	"fmt"
	"testing"
)

const (
	step2 = "phase-001.task-001.step-002"
	task1 = "phase-001.task-001"
)

// setStep moves a step to a stage (and status), adding work when the stage
// requires it.
func setStep(t *testing.T, r *Repo, id string, stage Stage, status Status, reason string) {
	t.Helper()
	cur, err := r.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Update(id, cur.NodeRevision, func(n *Node) error {
		n.Planning.Stage = stage
		n.Status = status
		n.Reason = reason
		if stage == StageDrafted || stage == StageApproved {
			n.Work = draftedWork()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func kinds(as []Action) string {
	var s []string
	for _, a := range as {
		s = append(s, fmt.Sprintf("%s:%s", a.Kind, a.NodeID))
	}
	return fmt.Sprint(s)
}

func TestNextActionsEmptyPlanNeedsDecomposition(t *testing.T) {
	dir := t.TempDir()
	r := Open(dir)
	if err := r.Init(mk(t, RootID)); err != nil {
		t.Fatal(err)
	}
	got, err := r.NextActions()
	if err != nil {
		t.Fatal(err)
	}
	if kinds(got.Runnable) != "[decompose:plan]" || len(got.Blocked) != 0 {
		t.Errorf("empty plan: runnable=%s blocked=%s", kinds(got.Runnable), kinds(got.Blocked))
	}
}

func TestNextActionsChildlessStructuralNodesNeedDecomposition(t *testing.T) {
	dir := t.TempDir()
	r := Open(dir)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(r.Init(mk(t, RootID)))
	must(r.Create(mk(t, "phase-001")))
	got, _ := r.NextActions()
	if kinds(got.Runnable) != "[decompose:phase-001]" {
		t.Errorf("phase without tasks: %s", kinds(got.Runnable))
	}
	must(r.Create(mk(t, task1)))
	got, _ = r.NextActions()
	if kinds(got.Runnable) != "[decompose:phase-001.task-001]" {
		t.Errorf("task without steps: %s", kinds(got.Runnable))
	}
}

func TestNextActionsDraftReconcileAndPrecedence(t *testing.T) {
	r, _ := newRepo(t)
	addStep(t, r, step2)
	got, _ := r.NextActions()
	if kinds(got.Runnable) != "[draft:"+step1+" draft:"+step2+"]" {
		t.Fatalf("two skeletons: %s", kinds(got.Runnable))
	}

	setStep(t, r, step1, StageDrafted, StatusUntouched, "")
	got, _ = r.NextActions()
	if kinds(got.Runnable) != "[draft:"+step2+"]" {
		t.Errorf("after drafting step-001: %s", kinds(got.Runnable))
	}

	// A changed requirement outranks fresh drafting, even though the step
	// needing a draft comes first in tree order.
	setStep(t, r, step2, StageNeedsReconciliation, StatusUntouched, "")
	setStep(t, r, step1, StageSkeleton, StatusUntouched, "")
	got, _ = r.NextActions()
	if kinds(got.Runnable) != "[reconcile:"+step2+" draft:"+step1+"]" {
		t.Errorf("reconcile must come first: %s", kinds(got.Runnable))
	}
}

func TestNextActionsBlockedQuestionDoesNotStallOtherWork(t *testing.T) {
	r, _ := newRepo(t)
	addStep(t, r, step2)
	setStep(t, r, step1, StageAwaitingAnswers, StatusUntouched, "")
	got, _ := r.NextActions()
	if kinds(got.Runnable) != "[draft:"+step2+"]" || kinds(got.Blocked) != "[answer:"+step1+"]" {
		t.Errorf("runnable=%s blocked=%s", kinds(got.Runnable), kinds(got.Blocked))
	}
}

func TestNextActionsHeldStepsAreIgnored(t *testing.T) {
	r, _ := newRepo(t)
	setStep(t, r, step1, StageSkeleton, StatusDelayed, "waiting for hardware")
	got, _ := r.NextActions()
	if len(got.Runnable) != 0 || len(got.Blocked) != 0 {
		t.Errorf("held step produced actions: %s %s", kinds(got.Runnable), kinds(got.Blocked))
	}
}

func TestNextActionsApproveOnlyWhenNothingElseIsOutstanding(t *testing.T) {
	r, dir := newRepo(t)
	addStep(t, r, step2)
	setStep(t, r, step1, StageDrafted, StatusUntouched, "")
	setStep(t, r, step2, StageAwaitingAnswers, StatusUntouched, "")
	got, _ := r.NextActions()
	for _, a := range append(got.Runnable, got.Blocked...) {
		if a.Kind == ActionApprove {
			t.Fatalf("approve offered while a question is open: %s", kinds(got.Runnable))
		}
	}

	setStep(t, r, step2, StageDrafted, StatusUntouched, "")
	got, _ = Open(dir).NextActions() // a fresh process
	if kinds(got.Runnable) != "[approve:plan]" {
		t.Fatalf("all drafted: %s", kinds(got.Runnable))
	}

	setStep(t, r, step1, StageApproved, StatusReviewed, "")
	setStep(t, r, step2, StageApproved, StatusReviewed, "")
	got, _ = r.NextActions()
	if len(got.Runnable) != 0 || len(got.Blocked) != 0 {
		t.Errorf("an approved plan has nothing left: %s %s", kinds(got.Runnable), kinds(got.Blocked))
	}
}

func TestSummarizeCountsAndDerivedStatus(t *testing.T) {
	r, _ := newRepo(t)
	addStep(t, r, step2)
	addStep(t, r, "phase-001.task-001.step-003")
	setStep(t, r, step1, StageApproved, StatusReviewed, "")
	setStep(t, r, step2, StageAwaitingAnswers, StatusUntouched, "")
	setStep(t, r, "phase-001.task-001.step-003", StageSkeleton, StatusSkipped, "out of scope")

	s, err := r.Summarize(task1)
	if err != nil {
		t.Fatal(err)
	}
	if s.Leaves != 3 || s.Counts.Approved != 1 || s.Counts.AwaitingAnswers != 1 || s.Counts.Skeleton != 1 || s.Counts.Skipped != 1 {
		t.Errorf("counts = %+v", s)
	}
	if s.Status != StatusUntouched {
		t.Errorf("a subtree with unfinished or skipped leaves must not be reviewed, got %q", s.Status)
	}

	setStep(t, r, step2, StageApproved, StatusReviewed, "")
	setStep(t, r, "phase-001.task-001.step-003", StageApproved, StatusReviewed, "")
	s, _ = r.Summarize(RootID)
	if s.Leaves != 3 || s.Status != StatusReviewed {
		t.Errorf("all leaves approved: %+v", s)
	}

	empty := Open(t.TempDir())
	_ = empty.Init(mk(t, RootID))
	if s, _ := empty.Summarize(RootID); s.Leaves != 0 || s.Status != StatusUntouched {
		t.Errorf("an empty plan is not reviewed: %+v", s)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./plantree/... -count=1`
Expected: build FAIL with `r.NextActions undefined` / `r.Summarize undefined`.

- [ ] **Step 3: Write the implementation**

Create `plantree/summary.go`:

```go
package plantree

// Counts tallies the steps in a subtree by planning stage, plus holds.
type Counts struct {
	Skeleton            int
	Inspected           int
	Drafted             int
	AwaitingAnswers     int
	NeedsReconciliation int
	Approved            int
	Held                int // blocked, delayed or escalated
	Skipped             int
}

// Summary is a subtree's derived progress. Structural nodes store no status;
// this is where it comes from.
type Summary struct {
	Leaves int
	Counts Counts
	Status Status
}

// Summarize walks the subtree rooted at id. Status is StatusReviewed only
// when the subtree has at least one step and every step is approved and
// reviewed. A skipped step is not approved, so it keeps the subtree
// untouched rather than passing silently.
func (r *Repo) Summarize(id string) (Summary, error) {
	root, err := r.read(id)
	if err != nil {
		return Summary{}, err
	}
	var s Summary
	reviewed := 0
	err = r.walk(root, func(n Node) error {
		if n.Kind() != KindStep {
			return nil
		}
		s.Leaves++
		switch n.Planning.Stage {
		case StageSkeleton:
			s.Counts.Skeleton++
		case StageInspected:
			s.Counts.Inspected++
		case StageDrafted:
			s.Counts.Drafted++
		case StageAwaitingAnswers:
			s.Counts.AwaitingAnswers++
		case StageNeedsReconciliation:
			s.Counts.NeedsReconciliation++
		case StageApproved:
			s.Counts.Approved++
		}
		switch n.Status {
		case StatusBlocked, StatusDelayed, StatusEscalated:
			s.Counts.Held++
		case StatusSkipped:
			s.Counts.Skipped++
		}
		if n.Status == StatusReviewed && n.Planning.Stage == StageApproved {
			reviewed++
		}
		return nil
	})
	if err != nil {
		return Summary{}, err
	}
	s.Status = StatusUntouched
	if s.Leaves > 0 && reviewed == s.Leaves {
		s.Status = StatusReviewed
	}
	return s, nil
}
```

Create `plantree/next.go`:

```go
package plantree

// ActionKind names the next planning obligation.
type ActionKind string

const (
	// ActionDecompose: a structural node has no children yet.
	ActionDecompose ActionKind = "decompose"
	// ActionReconcile: a requirement changed under a step; redo its spec.
	ActionReconcile ActionKind = "reconcile"
	// ActionDraft: a step has no complete specification yet.
	ActionDraft ActionKind = "draft"
	// ActionAnswer: a step waits on a human answer.
	ActionAnswer ActionKind = "answer"
	// ActionApprove: every step is drafted; the plan awaits approval.
	ActionApprove ActionKind = "approve"
)

// Action is one outstanding planning obligation.
type Action struct {
	Kind   ActionKind
	NodeID string
	Reason string
}

// Actions separates work that can proceed now from work waiting on a human.
type Actions struct {
	Runnable []Action
	Blocked  []Action
}

// NextActions derives what remains to be done from the committed tree alone.
// Nothing is remembered between calls, so a new process asking the same
// question gets the same answer. Runnable order is all reconcile, then all
// decompose, then all draft, each in tree order.
func (r *Repo) NextActions() (Actions, error) {
	var reconcile, decompose, draft []Action
	var out Actions
	err := r.Walk(func(n Node) error {
		if n.Kind() != KindStep {
			kids, err := r.Children(n.ID)
			if err != nil {
				return err
			}
			if len(kids) == 0 {
				decompose = append(decompose, Action{Kind: ActionDecompose, NodeID: n.ID, Reason: "no children yet"})
			}
			return nil
		}
		switch n.Status {
		case StatusBlocked, StatusDelayed, StatusEscalated, StatusSkipped:
			return nil // an explicit hold
		}
		switch n.Planning.Stage {
		case StageNeedsReconciliation:
			reconcile = append(reconcile, Action{Kind: ActionReconcile, NodeID: n.ID, Reason: "a requirement changed"})
		case StageSkeleton, StageInspected:
			draft = append(draft, Action{Kind: ActionDraft, NodeID: n.ID, Reason: "specification not drafted"})
		case StageAwaitingAnswers:
			out.Blocked = append(out.Blocked, Action{Kind: ActionAnswer, NodeID: n.ID, Reason: "waiting on an answer"})
		}
		return nil
	})
	if err != nil {
		return Actions{}, err
	}
	out.Runnable = append(append(append(out.Runnable, reconcile...), decompose...), draft...)
	if len(out.Runnable) == 0 && len(out.Blocked) == 0 {
		s, err := r.Summarize(RootID)
		if err != nil {
			return Actions{}, err
		}
		if s.Leaves > 0 && s.Status != StatusReviewed {
			out.Runnable = append(out.Runnable, Action{Kind: ActionApprove, NodeID: RootID, Reason: "every step is drafted"})
		}
	}
	return out, nil
}
```

- [ ] **Step 4: Run every check**

Run: `go test ./plantree/... -count=1 && go test -race ./plantree/... -count=1 && gofmt -l plantree && go vet ./plantree/... && go build ./...`
Expected: all `ok`, no gofmt output, vet clean, whole module builds.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/plantree/summary.go gophermind-lib/plantree/next.go gophermind-lib/plantree/next_test.go
git commit -m "feat(plantree): derived summaries and resumable next actions

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---

## Self-review (M1)

- **Spec coverage:** design doc principles 2 (tree on disk is the state) and the M1 row are covered by Tasks 1 to 5. Statuses and stages, v4 layout, ids, strict codec, optimistic concurrency, locking and atomic writes are covered. Overview, questions, passes, UI and export are M2 to M6 by design.
- **Placeholder scan:** no TBD or "similar to" steps; every code step has full code.
- **Type consistency:** `Node`, `Work`, `Planning`, `Repo`, `ConflictError`, `Summary`, `Counts`, `Action`, `Actions` and all constants are defined once and used with the same names in later tasks. `step1` is defined in `node_test.go` and reused by later test files in the same package; `newRepo`, `mk`, `draftedWork` live in `testhelp_test.go`.
- **Known limitation, stated:** `lockfile.WriteAtomic` does not fsync the directory. Acceptable for M1; the crash-safety upgrade (v4 rollback slot) is a deferred layer.

## Definition of done (M1)

`go test -race ./plantree/... -count=1` passes, `gofmt -l plantree` is empty, `go vet ./plantree/...` is clean, `go build ./...` succeeds, five commits exist, and a test proves a freshly opened `Repo` returns the same `NextActions` as the one that wrote the tree.
