package plan

import (
	"fmt"

	"gophermind/gophermind-lib/plantree"
)

// NextActions is repo.NextActions made aware of the questions store. A plan
// with an open question is never complete: the question may name no step, or
// only steps that are already specified, and then the tree alone cannot see
// it. So when any question is open the result has no approve action and has at
// least one blocked answer action (on the plan root, unless a held step
// already carries one). Callers that ask "what is left to do" should use this
// instead of repo.NextActions.
//
// Each reconcile action also says which decision changed, taken from the
// step's resume note, so the owner reads why the step must be planned again
// rather than only that it must. A reconcile action is runnable and keeps
// approval away until RunPass2 with Options2.Reconcile has redone the step.
func NextActions(repo *plantree.Repo) (plantree.Actions, error) {
	a, err := repo.NextActions()
	if err != nil {
		return plantree.Actions{}, err
	}
	if err := explainReconcile(repo, a.Runnable); err != nil {
		return plantree.Actions{}, err
	}
	open, err := OpenQuestions(repo)
	if err != nil {
		return plantree.Actions{}, err
	}
	if len(open) == 0 {
		return a, nil
	}
	kept := make([]plantree.Action, 0, len(a.Runnable))
	for _, x := range a.Runnable {
		if x.Kind != plantree.ActionApprove {
			kept = append(kept, x)
		}
	}
	a.Runnable = kept
	for _, x := range a.Blocked {
		if x.Kind == plantree.ActionAnswer {
			return a, nil
		}
	}
	a.Blocked = append(a.Blocked, plantree.Action{
		Kind:   plantree.ActionAnswer,
		NodeID: plantree.RootID,
		Reason: fmt.Sprintf("%d open question(s) wait for an answer, first %s", len(open), open[0].ID),
	})
	return a, nil
}

// explainReconcile replaces each reconcile action's generic reason with the
// step's resume note, which names the question whose answer changed. A step
// with no note keeps the generic reason.
func explainReconcile(repo *plantree.Repo, actions []plantree.Action) error {
	for i, x := range actions {
		if x.Kind != plantree.ActionReconcile {
			continue
		}
		n, err := repo.Get(x.NodeID)
		if err != nil {
			return err
		}
		if note := oneLine(n.ResumeNote); note != "" {
			actions[i].Reason = cutBytes(note, reconcileNoteBytes)
		}
	}
	return nil
}
