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
	// ActionHeld: a step is on hold, skipped, failed or needs revision;
	// Reason says why.
	ActionHeld ActionKind = "held"
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
// decompose, then all draft, each in tree order. An empty result means the
// plan is complete.
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
		case StatusBlocked, StatusDelayed, StatusEscalated, StatusFailed, StatusNeedsRevision, StatusSkipped:
			out.Blocked = append(out.Blocked, Action{Kind: ActionHeld, NodeID: n.ID, Reason: string(n.Status) + ": " + n.Reason})
			return nil
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
