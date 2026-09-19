package plan

import (
	"fmt"
	"strings"

	"gophermind/gophermind-lib/plantree"
)

// stepsUnder returns every step that is one of ids or lies below one of them.
func stepsUnder(repo *plantree.Repo, ids []string) ([]plantree.Node, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var out []plantree.Node
	err := repo.Walk(func(n plantree.Node) error {
		if n.Kind() != plantree.KindStep {
			return nil
		}
		for _, id := range ids {
			if n.ID == id || strings.HasPrefix(n.ID, id+".") {
				out = append(out, n)
				return nil
			}
		}
		return nil
	})
	return out, err
}

// holdSteps moves every step under ids that is still waiting for its
// specification (a skeleton or inspected step, not on hold) to the
// awaiting-answers stage, so pass 2 leaves it alone until its questions are
// answered. It returns how many steps it moved. Calling it again changes
// nothing.
func holdSteps(repo *plantree.Repo, ids []string) (int, error) {
	steps, err := stepsUnder(repo, ids)
	if err != nil {
		return 0, err
	}
	held := 0
	for _, s := range steps {
		if !needsSpec(s) {
			continue
		}
		if _, err := repo.Update(s.ID, s.NodeRevision, func(n *plantree.Node) error {
			n.Planning.Stage = plantree.StageAwaitingAnswers
			return nil
		}); err != nil {
			return held, fmt.Errorf("holding %s for an answer: %w", s.ID, err)
		}
		held++
	}
	return held, nil
}

// applyPass1Questions records the questions a skeleton pass asked and holds
// the steps they affect. A question's affects are phase or task titles; they
// are matched, ignoring case and spacing, against the nodes this chunk created
// or reused, and a title that matches none is ignored. It returns how many
// questions were new. Replaying the same pass adds nothing and holds nothing
// new, and a question that is already answered holds nothing.
func applyPass1Questions(repo *plantree.Repo, chunk Chunk, out Pass1Output, touched []string) (int, error) {
	if len(out.Questions) == 0 {
		return 0, nil
	}
	byTitle := map[string][]string{}
	for _, id := range touched {
		n, err := repo.Get(id)
		if err != nil {
			return 0, err
		}
		key := NormalizeTitle(n.Title)
		byTitle[key] = append(byTitle[key], id)
	}
	source := fmt.Sprintf("chunk %d of the brief", chunk.Index+1)
	nqs := make([]NewQuestion, 0, len(out.Questions))
	for _, q := range out.Questions {
		var ids []string
		seen := map[string]bool{}
		for _, title := range q.Affects {
			for _, id := range byTitle[NormalizeTitle(title)] {
				if !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
		}
		nq := newQuestion(q, ids)
		nq.Source = source
		nqs = append(nqs, nq)
	}
	before, err := LoadQuestions(repo)
	if err != nil {
		return 0, err
	}
	known := map[string]bool{}
	for _, q := range before {
		known[q.ID] = true
	}
	asked, err := AddQuestions(repo, nqs)
	if err != nil {
		return 0, err
	}
	added := 0
	var hold []string
	for _, q := range asked {
		if !known[q.ID] {
			added++
		}
		if q.Status == QuestionOpen {
			hold = append(hold, q.Affects...)
		}
	}
	if _, err := holdSteps(repo, hold); err != nil {
		return added, err
	}
	return added, nil
}

// recordPass2Questions records the questions a specification pass asked and
// holds the steps they name: those steps get no specification until the
// questions are answered. It returns how many questions were new. Replaying the
// same pass adds nothing new.
func recordPass2Questions(repo *plantree.Repo, taskID string, out Pass2Output) (int, error) {
	if len(out.Questions) == 0 {
		return 0, nil
	}
	source := "specifying task " + taskID
	nqs := make([]NewQuestion, 0, len(out.Questions))
	for _, q := range out.Questions {
		nq := newQuestion(q, append([]string{}, q.Affects...))
		nq.Source = source
		nqs = append(nqs, nq)
	}
	before, err := LoadQuestions(repo)
	if err != nil {
		return 0, err
	}
	known := map[string]bool{}
	for _, q := range before {
		known[q.ID] = true
	}
	asked, err := AddQuestions(repo, nqs)
	if err != nil {
		return 0, err
	}
	added := 0
	var hold []string
	for _, q := range asked {
		if !known[q.ID] {
			added++
		}
		if q.Status == QuestionOpen {
			hold = append(hold, q.Affects...)
		}
	}
	if _, err := holdSteps(repo, hold); err != nil {
		return added, err
	}
	return added, nil
}

// markAsked records in the in-memory snapshot that the steps named by out's
// questions now wait for an answer, so a later batch of the same task sees them
// so.
func markAsked(steps []plantree.Node, out Pass2Output) {
	asked := map[string]bool{}
	for _, q := range out.Questions {
		for _, id := range q.Affects {
			asked[id] = true
		}
	}
	for i := range steps {
		if asked[steps[i].ID] && needsSpec(steps[i]) {
			steps[i].Planning.Stage = plantree.StageAwaitingAnswers
		}
	}
}

// ReleaseAnswered moves every step that waits for an answer back to the
// inspected stage once no open question still affects it, directly or through
// the task or phase above it, so pass 2 will specify it. It returns how many
// steps it released. A step on hold stays where it is. Calling it again changes
// nothing.
func ReleaseAnswered(repo *plantree.Repo) (int, error) {
	open, err := OpenQuestions(repo)
	if err != nil {
		return 0, err
	}
	blocked := map[string]bool{}
	for _, q := range open {
		for _, id := range q.Affects {
			blocked[id] = true
		}
	}
	var waiting []plantree.Node
	err = repo.Walk(func(n plantree.Node) error {
		if n.Kind() == plantree.KindStep && n.Planning.Stage == plantree.StageAwaitingAnswers && !onHold(n) {
			waiting = append(waiting, n)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	released := 0
	for _, s := range waiting {
		if affectedBy(blocked, s.ID) {
			continue
		}
		if _, err := repo.Update(s.ID, s.NodeRevision, func(n *plantree.Node) error {
			n.Planning.Stage = plantree.StageInspected
			return nil
		}); err != nil {
			return released, fmt.Errorf("releasing %s: %w", s.ID, err)
		}
		released++
	}
	return released, nil
}

// affectedBy reports whether id, or any node above it, is in blocked.
func affectedBy(blocked map[string]bool, id string) bool {
	for id != "" {
		if blocked[id] {
			return true
		}
		parent, err := plantree.ParentID(id)
		if err != nil {
			return false
		}
		id = parent
	}
	return false
}
