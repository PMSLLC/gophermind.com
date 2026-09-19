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
