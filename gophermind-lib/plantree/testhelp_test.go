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
