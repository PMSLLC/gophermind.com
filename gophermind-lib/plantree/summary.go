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
