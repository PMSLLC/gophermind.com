package executor

import (
	"reflect"
	"testing"

	"gophermind/gophermind-lib/build/planner"
)

func attributeFixture() *runCtx {
	a, b := &Leaf{ID: "fn-a"}, &Leaf{ID: "fn-b"}
	return &runCtx{
		plan: &Plan{Leaves: []*Leaf{b, a}, byID: map[string]*Leaf{"fn-a": a, "fn-b": b}},
		accept: acceptPlan{
			acceptance:  []planBullet{{req: planner.Requirement{ID: "A5"}}},
			constraints: []planBullet{{req: planner.Requirement{ID: "C2"}}},
		},
	}
}

// The coverage step mapped only the feature requirements to leaves. A failing
// acceptance bullet that tests the program as a whole (slugify --max 6) had no
// leaf, so no repair was tried although the program was wrong.
func TestAnAcceptanceBulletNobodyOwnsFallsBackToEveryLeaf(t *testing.T) {
	rc := attributeFixture()
	if got := rc.attributeFor("A5", failedBullet{}); !reflect.DeepEqual(got, []string{"fn-a", "fn-b"}) {
		t.Errorf("A5 = %v, want every leaf, sorted", got)
	}
	if got := rc.attributeFor("A5", failedBullet{Nodes: []string{"fn-b", "gone"}}); !reflect.DeepEqual(got, []string{"fn-b"}) {
		t.Errorf("a mapped bullet = %v, want only its own leaf", got)
	}
}

// A constraint check no leaf owns (go 1.22 against go.mod written by the
// harness) cannot be repaired by changing code, so it does not fall back.
func TestAConstraintNobodyOwnsStaysUnowned(t *testing.T) {
	rc := attributeFixture()
	if got := rc.attributeFor("C2", failedBullet{}); len(got) != 0 {
		t.Errorf("C2 = %v, want none", got)
	}
}
