package plan

import (
	"fmt"
	"strconv"

	"gophermind/gophermind-lib/plantree"
)

// Created counts the nodes a merge added to the tree.
type Created struct {
	Phases int
	Tasks  int
	Steps  int
}

func (c *Created) add(o Created) {
	c.Phases += o.Phases
	c.Tasks += o.Tasks
	c.Steps += o.Steps
}

// Merge applies a skeleton pass to the tree. A proposed node whose title
// (ignoring case and spacing) matches an existing sibling is reused unchanged,
// so replaying the same pass after a crash adds nothing twice. New nodes start
// as untouched skeletons. The output must come from ParsePass1: Merge does not
// re-validate it.
func Merge(repo *plantree.Repo, out Pass1Output) (Created, error) {
	created, _, err := mergeTracked(repo, out)
	return created, err
}

// mergeTracked is Merge that also returns the ids of every node the pass
// created or reused, in order and without repeats. RunPass1 records them so
// pass 2 can show a task the part of the brief that produced it.
func mergeTracked(repo *plantree.Repo, out Pass1Output) (Created, []string, error) {
	var created Created
	var touched []string
	seen := map[string]bool{}
	note := func(id string) {
		if !seen[id] {
			seen[id] = true
			touched = append(touched, id)
		}
	}
	for _, p := range out.Phases {
		id, made, err := ensureChild(repo, plantree.RootID, p.Title, p.Digest, p.Objective)
		if err != nil {
			return created, touched, err
		}
		note(id)
		if made {
			created.Phases++
		}
		for _, t := range p.Tasks {
			tid, made, err := ensureChild(repo, id, t.Title, t.Digest, t.Objective)
			if err != nil {
				return created, touched, err
			}
			note(tid)
			if made {
				created.Tasks++
			}
			for _, s := range t.Steps {
				sid, made, err := ensureChild(repo, tid, s.Title, s.Digest, "")
				if err != nil {
					return created, touched, err
				}
				note(sid)
				if made {
					created.Steps++
				}
			}
		}
	}
	return created, touched, nil
}

// ensureChild returns the id of parent's child with the given title, creating
// it if there is none. made reports whether it was created.
func ensureChild(repo *plantree.Repo, parent, title, digest, objective string) (id string, made bool, err error) {
	kids, err := repo.Children(parent)
	if err != nil {
		return "", false, err
	}
	want := NormalizeTitle(title)
	highest := 0
	for _, k := range kids {
		if NormalizeTitle(k.Title) == want {
			return k.ID, false, nil
		}
		if n := segmentNumber(k.ID); n > highest {
			highest = n
		}
	}
	id, err = plantree.ChildID(parent, highest+1)
	if err != nil {
		return "", false, fmt.Errorf("adding %q under %s: %w", title, parent, err)
	}
	node, err := newSkeleton(id, title, digest, objective)
	if err != nil {
		return "", false, err
	}
	if err := repo.Create(node); err != nil {
		return "", false, err
	}
	return id, true, nil
}

// segmentNumber returns the trailing three-digit number of an id.
func segmentNumber(id string) int {
	if len(id) < 3 {
		return 0
	}
	n, _ := strconv.Atoi(id[len(id)-3:])
	return n
}

func newSkeleton(id, title, digest, objective string) (plantree.Node, error) {
	ref, err := plantree.ParentRef(id)
	if err != nil {
		return plantree.Node{}, err
	}
	n := plantree.Node{
		SchemaVersion: plantree.SchemaVersion,
		ID:            id,
		Title:         oneLine(title),
		NodeRevision:  1,
		ContextDigest: oneLine(digest),
		ParentRef:     &ref,
		DependsOn:     []string{},
		Planning:      plantree.Planning{Stage: plantree.StageSkeleton},
		Objective:     objective,
	}
	if n.Kind() == plantree.KindStep {
		n.Status = plantree.StatusUntouched
	}
	return n, nil
}
