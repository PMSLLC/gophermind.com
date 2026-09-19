// Package export turns an approved plan tree into the legacy planning
// artifacts the existing executor reads: ROADMAP.md, assignments.json,
// SPEC.md, PROJECT.md and the approval marker. The tree is canonical; these
// files are generated from it, replacing the model writing them directly.
//
// It is the one package that imports both plantree/plan and phaseflow. The
// dependency points this way on purpose: plantree knows nothing about
// phaseflow, so the tree can outlive the legacy format.
package export

import (
	"fmt"
	"strings"

	"gophermind/gophermind-lib/plantree"
)

// Bounds on the text one exported task carries. A plan tree can hold far more
// than a legacy task row usefully can, and assignments.json is read whole by
// every execution, so each field is cut rather than copied.
const (
	// taskDescriptionBytes bounds a task's whole description: its objective,
	// its steps and their test commands.
	taskDescriptionBytes = 8000
	// stepLineBytes bounds one step's line inside that description.
	stepLineBytes = 600
	// criterionBytes bounds one acceptance criterion, maxCriteria how many a
	// task keeps. phaseflow only requires at least one.
	criterionBytes = 300
	maxCriteria    = 20
	// planDescriptionBytes bounds a task's one-line description in ROADMAP.md.
	planDescriptionBytes = 200
	// phaseGoalBytes bounds a phase's **Goal** line.
	phaseGoalBytes = 400
	// overviewBytes and factsBytes bound what SPEC.md quotes.
	overviewBytes = 8000
	factsBytes    = 2000
)

// legacyTask is one tree task rendered as a legacy row.
type legacyTask struct {
	ID          string // NN-MM
	Title       string
	Description string
	Criteria    []string
	StepIDs     []string
}

// legacyPhase is one tree phase rendered as a roadmap phase.
type legacyPhase struct {
	Number int // 1-based, in tree order
	Name   string
	Goal   string
	Tasks  []legacyTask
}

// legacyPlan is the whole tree rendered for the legacy format.
type legacyPlan struct {
	Project string
	Phases  []legacyPhase
	Steps   int
}

// read walks the tree and renders it. Ids are assigned by position: phase N
// in tree order, task M within it, giving the "NN-MM" the roadmap parser and
// assignments.json share. Inserted-phase ids (the "2.1" form) are not
// produced: nothing inserts a phase into a tree yet.
func read(repo *plantree.Repo) (legacyPlan, error) {
	root, err := repo.Get(plantree.RootID)
	if err != nil {
		return legacyPlan{}, err
	}
	out := legacyPlan{Project: sanitize(fit(root.Title, planDescriptionBytes))}
	phases, err := repo.Children(plantree.RootID)
	if err != nil {
		return legacyPlan{}, err
	}
	for i, p := range phases {
		lp := legacyPhase{
			Number: i + 1,
			Name:   sanitize(fit(p.Title, planDescriptionBytes)),
			Goal:   sanitize(fit(firstNonEmpty(p.Objective, p.ContextDigest, p.Title), phaseGoalBytes)),
		}
		if lp.Name == "" {
			lp.Name = fmt.Sprintf("Phase %d", lp.Number)
		}
		if lp.Goal == "" {
			lp.Goal = "Deliver " + lp.Name
		}
		tasks, err := repo.Children(p.ID)
		if err != nil {
			return legacyPlan{}, err
		}
		for j, tk := range tasks {
			steps, err := repo.Children(tk.ID)
			if err != nil {
				return legacyPlan{}, err
			}
			lt, err := renderTask(fmt.Sprintf("%02d-%02d", lp.Number, j+1), tk, steps)
			if err != nil {
				return legacyPlan{}, err
			}
			out.Steps += len(steps)
			lp.Tasks = append(lp.Tasks, lt)
		}
		out.Phases = append(out.Phases, lp)
	}
	return out, nil
}

// renderTask folds a task and its steps into one legacy row. A task has no
// description of its own in the tree, so one is synthesized: its objective,
// then each step's work description, then the test commands the steps carry.
// The acceptance criteria are the union of the steps', deduplicated, because
// the legacy verifier checks a task as a whole.
func renderTask(id string, task plantree.Node, steps []plantree.Node) (legacyTask, error) {
	out := legacyTask{ID: id, Title: fit(task.Title, planDescriptionBytes)}
	if out.Title == "" {
		out.Title = "Task " + id
	}
	var b strings.Builder
	if obj := fit(firstNonEmpty(task.Objective, task.ContextDigest), 1000); obj != "" {
		b.WriteString(obj)
		b.WriteString("\n\n")
	}
	b.WriteString("Steps, in order:\n")
	seenCriteria := map[string]bool{}
	seenCommands := map[string]bool{}
	var commands []string
	for _, s := range steps {
		out.StepIDs = append(out.StepIDs, s.ID)
		desc := ""
		if s.Work != nil {
			desc = s.Work.Description
		}
		fmt.Fprintf(&b, "%d. %s: %s\n", len(out.StepIDs), fit(s.Title, 200), fit(firstNonEmpty(desc, s.ContextDigest), stepLineBytes))
		if s.Work == nil {
			continue
		}
		for _, c := range s.Work.AcceptanceCriteria {
			c = fit(c, criterionBytes)
			if c == "" || seenCriteria[c] || len(out.Criteria) >= maxCriteria {
				continue
			}
			seenCriteria[c] = true
			out.Criteria = append(out.Criteria, c)
		}
		if cmd := strings.TrimSpace(strings.Join(s.Work.TestCommand, " ")); cmd != "" && !seenCommands[cmd] {
			seenCommands[cmd] = true
			commands = append(commands, fit(cmd, criterionBytes))
		}
	}
	if len(commands) > 0 {
		// The test commands belong in the description: the executor reads it
		// as the task's instructions, and a command is an instruction, not a
		// check a reviewer performs.
		b.WriteString("\nVerify with: " + strings.Join(commands, " && ") + "\n")
	}
	out.Description = cutBytes(strings.TrimRight(b.String(), "\n"), taskDescriptionBytes)
	if len(out.Criteria) == 0 {
		return legacyTask{}, fmt.Errorf("export: task %s (%s) has no acceptance criteria; every step must be specified before the plan is exported", task.ID, out.Title)
	}
	return out, nil
}

// sanitize makes text safe for the fields phaseflow's validator treats as
// placeholders: it rejects anything in square brackets and any "TBD". A real
// title can contain both innocently, so they are rewritten rather than
// stripped, and only the phase name and goal go through this.
func sanitize(s string) string {
	s = strings.NewReplacer("[", "(", "]", ")", "*", "").Replace(s)
	// "(INSERTED)" makes phaseflow's roadmap parser mark the phase inserted and
	// strip the text, and a "[Inserted]" title has just become one above.
	s = replaceFold(s, "(INSERTED)", "inserted")
	s = replaceFold(s, "TBD", "undecided")
	return strings.TrimSpace(s)
}

// replaceFold replaces every case-insensitive ASCII occurrence of old.
func replaceFold(s, old, repl string) string {
	if !strings.Contains(strings.ToUpper(s), old) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if i+len(old) <= len(s) && strings.EqualFold(s[i:i+len(old)], old) {
			b.WriteString(repl)
			i += len(old)
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// quote renders text as a markdown blockquote, so nothing inside it can be
// read as a roadmap heading, phase line or plan checkbox. The overview and
// the facts come from a model, and ROADMAP.md is parsed by line.
func quote(s string) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight("> "+strings.TrimRight(l, "\r"), " ")
	}
	return strings.Join(lines, "\n")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// fit is one line of at most n bytes, cut on a rune boundary.
func fit(s string, n int) string { return cutBytes(strings.Join(strings.Fields(s), " "), n) }

// cutBytes shortens s to at most max bytes at a rune boundary. It mirrors the
// plan package's own helper, which is private to it.
func cutBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !isRuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
