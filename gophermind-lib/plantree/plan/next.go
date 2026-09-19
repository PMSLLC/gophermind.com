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
func NextActions(repo *plantree.Repo) (plantree.Actions, error) {
	a, err := repo.NextActions()
	if err != nil {
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
