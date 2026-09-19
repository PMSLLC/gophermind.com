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
