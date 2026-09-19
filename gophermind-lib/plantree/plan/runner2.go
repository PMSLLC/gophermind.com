package plan

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"gophermind/gophermind-lib/llm"
	"gophermind/gophermind-lib/plantree"
)

// Options2 tunes RunPass2. Zero values pick the defaults.
type Options2 struct {
	ProjectName string // default: the root node's title
	// StepsPerPass bounds how many steps one model call specifies (default 6),
	// so the reply stays small however large a task is.
	StepsPerPass int
	// BriefBytes bounds the brief excerpts shown with a task (default 4000).
	// With the defaults one pass is at most 27,000 bytes (a test pins it):
	// BriefBytes + OverviewCapBytes + FactsCapBytes + 3000 (step list) + about
	// 2,000 (decisions) + about 8,000 (phase, task and the steps being
	// specified) + about 3,500 (instructions). Pass2Prompt cuts each of those
	// inputs itself; a BriefBytes above the default raises the total by the
	// difference. At 3 to 4 bytes per token that must leave room for the reply
	// inside the model's window.
	BriefBytes int
	// Facts is text describing the repository (language, how to build and test,
	// layout) shown to every pass, cut to FactsCapBytes. If empty, RunPass2
	// reads the facts stored with WriteFacts, if any.
	Facts string
	// Reconcile also re-specifies the steps a changed answer flagged (stage
	// needs_reconciliation), showing each its previous specification and the
	// decision that changed. It is off by default so an ordinary resume never
	// silently redoes work that has already been paid for.
	Reconcile bool
	// Progress, if set, is called after each batch of steps has been written,
	// with the batches done and the batches this call will run in all. It runs
	// on the calling goroutine. A nil Progress changes nothing.
	Progress func(done, total int)
}

// Result2 summarizes one RunPass2 call.
type Result2 struct {
	Tasks  int // tasks this call ran passes for, whether or not every step got a specification
	Steps  int // steps specified by this call
	Passes int // model passes made by this call
	// Released counts steps that were waiting for an answer and were released
	// because their questions are now answered. They become eligible for
	// specification in this call, which may still ask about them again.
	Released int
	// Unspecified counts steps this call put in a pass batch that ended the
	// pass neither specified nor waiting for an answer: the model re-asked an
	// already answered question (a duplicate that holds nothing) or left a step
	// out. Such a step is selected again by the next RunPass2, so a caller
	// that sees Unspecified > 0 with no new Steps or Questions is making no
	// progress and should stop rather than loop.
	Unspecified int
	// Questions counts new questions asked by this call. The steps they name
	// wait for an answer and are not specified.
	Questions int
	// EmptyTasks counts tasks that have no steps at all; pass 2 cannot specify
	// them, and NextActions keeps offering decompose for them.
	EmptyTasks int
	// Reconciled counts steps that were waiting to be re-planned (stage
	// needs_reconciliation) and were specified again by this call. It is
	// always zero unless Options2.Reconcile is set. Such a step is counted in
	// Steps as well.
	Reconciled int
}

// taskWork is one task with the steps still waiting for a specification.
type taskWork struct {
	phase   plantree.Node
	task    plantree.Node
	steps   []plantree.Node // every step of the task
	pending []plantree.Node // steps that need a first specification
	redo    []plantree.Node // steps to specify again (needs_reconciliation)
}

// onHold reports whether a step is parked in a status that stops work on it.
func onHold(s plantree.Node) bool {
	switch s.Status {
	case plantree.StatusBlocked, plantree.StatusDelayed, plantree.StatusEscalated,
		plantree.StatusFailed, plantree.StatusNeedsRevision, plantree.StatusSkipped:
		return true
	}
	return false
}

// markSpecified records in the in-memory snapshot that the steps in out were
// written, so a later batch of the same task sees them as specified.
func markSpecified(steps []plantree.Node, out Pass2Output) {
	done := map[string]bool{}
	for _, s := range out.Steps {
		done[s.ID] = true
	}
	for i := range steps {
		if done[steps[i].ID] {
			steps[i].Planning.Stage = plantree.StageDrafted
		}
	}
}

// needsSpec reports whether a step is waiting for its specification: still a
// skeleton or only inspected, and not on hold.
func needsSpec(s plantree.Node) bool {
	if onHold(s) {
		return false
	}
	return s.Planning.Stage == plantree.StageSkeleton || s.Planning.Stage == plantree.StageInspected
}

// needsRespec reports whether a step carries a specification that a changed
// answer invalidated, so a reconciling pass must write it again. Holding and
// releasing ignore such a step: it already has a specification, so it is not
// waiting for one, and it blocks approval on its own (NextActions offers
// reconcile for it).
func needsRespec(s plantree.Node) bool {
	return !onHold(s) && s.Planning.Stage == plantree.StageNeedsReconciliation
}

// pendingWork lists, in tree order, every task that has a step needing a
// specification. It is derived from the tree alone, so a resumed run finds
// exactly what is left. With reconcile set it also collects the steps a
// changed answer flagged for re-planning.
func pendingWork(repo *plantree.Repo, reconcile bool) ([]taskWork, error) {
	var out []taskWork
	var phase plantree.Node
	err := repo.Walk(func(n plantree.Node) error {
		switch n.Kind() {
		case plantree.KindPhase:
			phase = n
		case plantree.KindTask:
			steps, err := repo.Children(n.ID)
			if err != nil {
				return err
			}
			var pending, redo []plantree.Node
			for _, s := range steps {
				switch {
				case needsSpec(s):
					pending = append(pending, s)
				case reconcile && needsRespec(s):
					redo = append(redo, s)
				}
			}
			if len(pending)+len(redo) > 0 {
				out = append(out, taskWork{phase: phase, task: n, steps: steps, pending: pending, redo: redo})
			}
		}
		return nil
	})
	return out, err
}

func idsOf(nodes []plantree.Node) []string {
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	return ids
}

// RunPass2 writes the work specification of every step that lacks one: one
// fresh-context pass per batch of steps of one task, in tree order. It keeps no
// cursor: what is left is derived from the tree, so calling it again after any
// error continues with the steps still waiting. It does not rewrite
// overview.md; only pass 1 does.
func RunPass2(ctx context.Context, repo *plantree.Repo, c Completer, opt Options2) (Result2, error) {
	if err := opt.Validate(); err != nil {
		return Result2{}, err
	}
	unlock, err := AcquireRun(repo)
	if err != nil {
		return Result2{}, err
	}
	defer unlock()
	if opt.StepsPerPass < 1 {
		opt.StepsPerPass = defaultStepsPerPass
	}
	if opt.BriefBytes < 1 {
		opt.BriefBytes = defaultBriefBytes
	}
	root, err := repo.Get(plantree.RootID)
	if err != nil {
		return Result2{}, err
	}
	if opt.ProjectName == "" {
		opt.ProjectName = root.Title
	}
	released, err := ReleaseAnswered(repo)
	if err != nil {
		return Result2{Released: released}, err
	}
	if _, err := HoldOpen(repo); err != nil {
		return Result2{Released: released}, err
	}
	facts := opt.Facts
	if strings.TrimSpace(facts) == "" {
		if facts, err = ReadFacts(repo); err != nil {
			return Result2{Released: released}, err
		}
	}
	overview, err := ReadOverview(repo.Dir())
	if err != nil {
		return Result2{}, err
	}
	overview = FitOverview(overview, OverviewCapBytes)
	prov, err := loadProvenance(repo)
	if err != nil {
		return Result2{}, err
	}
	chunks, err := BriefChunks(repo)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Result2{}, err
	}
	work, err := pendingWork(repo, opt.Reconcile)
	if err != nil {
		return Result2{}, err
	}
	allQuestions, err := LoadQuestions(repo)
	if err != nil {
		return Result2{}, err
	}

	empty, err := EmptyTasks(repo)
	if err != nil {
		return Result2{}, err
	}
	res := Result2{EmptyTasks: len(empty), Released: released}
	totalBatches, doneBatches := 0, 0
	for _, w := range work {
		totalBatches += len(batchesOf(w.pending, opt.StepsPerPass)) + len(batchesOf(w.redo, reconcileBatch(opt.StepsPerPass)))
	}
	for _, w := range work {
		ids := append([]string{w.task.ID}, idsOf(w.steps)...)
		excerpts := Excerpts(chunks, prov.chunksFor(ids), opt.BriefBytes)
		decisions := decisionsFor(allQuestions, append([]string{w.phase.ID}, ids...))
		// Batches are never mixed: a batch of steps being re-planned is
		// smaller and carries each step's previous specification, so keeping
		// the two apart is what keeps the prompt inside its budget.
		batches := append(batchesOf(w.pending, opt.StepsPerPass), batchesOf(w.redo, reconcileBatch(opt.StepsPerPass))...)
		for _, batch := range batches {
			if err := ctx.Err(); err != nil {
				return res, err
			}
			batchIDs := idsOf(batch)
			var live []plantree.Node
			for _, s := range w.steps {
				if !onHold(s) && s.Planning.Stage != plantree.StageAwaitingAnswers {
					live = append(live, s)
				}
			}
			siblingIDs := idsOf(live)
			prompt := Pass2Prompt(Pass2Input{
				Project: opt.ProjectName, Overview: overview, Facts: facts, Decisions: decisions, Excerpts: excerpts, ExcerptsCap: opt.BriefBytes,
				Phase: w.phase, Task: w.task, Siblings: w.steps, Batch: batch,
			})
			out, err := askJSON(ctx, c, prompt, func(reply string) (Pass2Output, error) {
				return ParsePass2(reply, batchIDs, siblingIDs)
			})
			res.Passes++
			if err != nil {
				return res, taskError(w.task.ID, err)
			}
			asked, err := recordPass2Questions(repo, w.task.ID, out)
			res.Questions += asked
			if err != nil {
				return res, taskError(w.task.ID, err)
			}
			// A batch is all pending or all being re-planned, so the redo set
			// is that of this batch alone.
			redo := map[string]bool{}
			for _, s := range batch {
				if needsRespec(s) {
					redo[s.ID] = true
				}
			}
			if err := applySpecs(repo, out, redo); err != nil {
				return res, taskError(w.task.ID, err)
			}
			markSpecified(w.steps, out)
			markAsked(w.steps, out)
			res.Steps += len(out.Steps)
			for _, s := range out.Steps {
				if redo[s.ID] {
					res.Reconciled++
				}
			}
			for _, s := range batch {
				cur, err := repo.Get(s.ID)
				if err != nil {
					return res, taskError(w.task.ID, err)
				}
				if needsSpec(cur) || (opt.Reconcile && needsRespec(cur)) {
					res.Unspecified++
				}
			}
			doneBatches++
			if opt.Progress != nil {
				opt.Progress(doneBatches, totalBatches)
			}
		}
		res.Tasks++
	}
	return res, nil
}

// reconcileBatch is the batch size for steps being re-planned. It is smaller
// than an ordinary batch because each such step also carries its previous
// specification and the reason it is being redone, and it is never larger
// than the ordinary batch the caller chose, so sizes shrunk for a small
// context window (see SizesFor) shrink the re-planning batch with them.
func reconcileBatch(stepsPerPass int) int {
	if stepsPerPass > 0 && stepsPerPass < reconcileStepsPerPass {
		return stepsPerPass
	}
	return reconcileStepsPerPass
}

// batchesOf cuts steps into consecutive batches of at most size, in order.
func batchesOf(steps []plantree.Node, size int) [][]plantree.Node {
	if size < 1 {
		size = 1
	}
	var out [][]plantree.Node
	for start := 0; start < len(steps); start += size {
		end := start + size
		if end > len(steps) {
			end = len(steps)
		}
		out = append(out, steps[start:end])
	}
	return out
}

func taskError(taskID string, err error) error {
	hint := ""
	if _, ok := llm.ContextLimitFromError(err); ok {
		hint = " (the server's context window is smaller than one pass needs: lower Options2.BriefBytes or StepsPerPass)"
	}
	return fmt.Errorf("plan: task %s: %w%s", taskID, err, hint)
}

// applySpecs writes each step's specification and moves it to the drafted
// stage. Update rejects a step whose specification is incomplete, so nothing
// half-specified reaches the tree. A step in redo is being specified again
// after a decision changed: its resume note is replaced with a summary of the
// specification this one overwrites, so what it used to say is not simply
// lost.
func applySpecs(repo *plantree.Repo, out Pass2Output, redo map[string]bool) error {
	for _, s := range out.Steps {
		cur, err := repo.Get(s.ID)
		if err != nil {
			return err
		}
		spec := s
		note := ""
		if redo[s.ID] {
			note = respecNote(cur.Work)
		}
		if _, err := repo.Update(s.ID, cur.NodeRevision, func(n *plantree.Node) error {
			n.Work = &plantree.Work{
				Description:        spec.Description,
				TargetPaths:        spec.TargetPaths,
				AcceptanceCriteria: spec.AcceptanceCriteria,
				TestCommand:        spec.TestCommand,
			}
			n.DependsOn = spec.DependsOn
			n.Planning.Stage = plantree.StageDrafted
			if note != "" {
				n.ResumeNote = note
			}
			return nil
		}); err != nil {
			return fmt.Errorf("writing the specification of %s: %w", s.ID, err)
		}
	}
	return nil
}

// respecNote summarizes the specification a re-planned step is losing. It
// replaces the note the changed answer left, which the prompt for this pass
// has already shown the model, and only the most recent replacement is kept,
// so the note cannot grow.
func respecNote(old *plantree.Work) string {
	if old == nil || strings.TrimSpace(old.Description) == "" {
		return "re-planned after the owner changed a decision"
	}
	return cutBytes("re-planned; the previous specification was: "+oneLine(old.Description), reconcileNoteBytes)
}

// EmptyTasks returns, in tree order, the ids of every task that has no steps.
func EmptyTasks(repo *plantree.Repo) ([]string, error) {
	var out []string
	err := repo.Walk(func(n plantree.Node) error {
		if n.Kind() != plantree.KindTask {
			return nil
		}
		steps, err := repo.Children(n.ID)
		if err != nil {
			return err
		}
		if len(steps) == 0 {
			out = append(out, n.ID)
		}
		return nil
	})
	return out, err
}
