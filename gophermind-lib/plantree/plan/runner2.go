package plan

import (
	"context"
	"errors"
	"fmt"
	"os"

	"gophermind/gophermind-lib/llm"
	"gophermind/gophermind-lib/plantree"
)

// Options2 tunes RunPass2. Zero values pick the defaults.
type Options2 struct {
	ProjectName string // default: the root node's title
	// StepsPerPass bounds how many steps one model call specifies (default 6),
	// so the reply stays small however large a task is.
	StepsPerPass int
	// BriefBytes bounds the brief excerpts shown with a task (default 6000).
	// With the defaults one pass is at most about 25,000 bytes: BriefBytes +
	// OverviewCapBytes + 4000 (step list) + about 8,000 (phase, task and the
	// steps being specified) + 2,500 (instructions). At 3 to 4 bytes per token
	// that must leave room for the reply inside the model's window.
	BriefBytes int
}

// Result2 summarizes one RunPass2 call.
type Result2 struct {
	Tasks  int // tasks whose pending steps were all specified by this call
	Steps  int // steps specified by this call
	Passes int // model passes made by this call
	// EmptyTasks counts tasks that have no steps at all; pass 2 cannot specify
	// them, and NextActions keeps offering decompose for them.
	EmptyTasks int
}

// taskWork is one task with the steps still waiting for a specification.
type taskWork struct {
	phase   plantree.Node
	task    plantree.Node
	steps   []plantree.Node // every step of the task
	pending []plantree.Node // steps that need a specification
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

// pendingWork lists, in tree order, every task that has a step needing a
// specification. It is derived from the tree alone, so a resumed run finds
// exactly what is left.
func pendingWork(repo *plantree.Repo) ([]taskWork, error) {
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
			var pending []plantree.Node
			for _, s := range steps {
				if needsSpec(s) {
					pending = append(pending, s)
				}
			}
			if len(pending) > 0 {
				out = append(out, taskWork{phase: phase, task: n, steps: steps, pending: pending})
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
	work, err := pendingWork(repo)
	if err != nil {
		return Result2{}, err
	}

	empty, err := EmptyTasks(repo)
	if err != nil {
		return Result2{}, err
	}
	res := Result2{EmptyTasks: len(empty)}
	for _, w := range work {
		ids := append([]string{w.task.ID}, idsOf(w.steps)...)
		excerpts := Excerpts(chunks, prov.chunksFor(ids), opt.BriefBytes)
		for start := 0; start < len(w.pending); start += opt.StepsPerPass {
			if err := ctx.Err(); err != nil {
				return res, err
			}
			end := start + opt.StepsPerPass
			if end > len(w.pending) {
				end = len(w.pending)
			}
			batch := w.pending[start:end]
			batchIDs := idsOf(batch)
			var live []plantree.Node
			for _, s := range w.steps {
				if !onHold(s) {
					live = append(live, s)
				}
			}
			siblingIDs := idsOf(live)
			prompt := Pass2Prompt(opt.ProjectName, overview, w.phase, w.task, w.steps, batch, excerpts)
			out, err := askJSON(ctx, c, prompt, func(reply string) (Pass2Output, error) {
				return ParsePass2(reply, batchIDs, siblingIDs)
			})
			res.Passes++
			if err != nil {
				return res, taskError(w.task.ID, err)
			}
			if err := applySpecs(repo, out); err != nil {
				return res, taskError(w.task.ID, err)
			}
			markSpecified(w.steps, out)
			res.Steps += len(batch)
		}
		res.Tasks++
	}
	return res, nil
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
// half-specified reaches the tree.
func applySpecs(repo *plantree.Repo, out Pass2Output) error {
	for _, s := range out.Steps {
		cur, err := repo.Get(s.ID)
		if err != nil {
			return err
		}
		spec := s
		if _, err := repo.Update(s.ID, cur.NodeRevision, func(n *plantree.Node) error {
			n.Work = &plantree.Work{
				Description:        spec.Description,
				TargetPaths:        spec.TargetPaths,
				AcceptanceCriteria: spec.AcceptanceCriteria,
				TestCommand:        spec.TestCommand,
			}
			n.DependsOn = spec.DependsOn
			n.Planning.Stage = plantree.StageDrafted
			return nil
		}); err != nil {
			return fmt.Errorf("writing the specification of %s: %w", s.ID, err)
		}
	}
	return nil
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
