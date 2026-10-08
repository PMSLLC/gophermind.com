package human

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// File is the gate for runs nobody is watching. It writes QUESTIONS.md,
// APPROVAL.md, or ESCALATION-<node>.md into a folder, each holding a fenced
// block for the answer, and returns ErrWaiting until the block is filled in.
// The next call (after `resume`) reads and validates the answer.
//
// The answer must not itself contain a line of three backticks.
type File struct{ dir string }

var _ Gate = (*File)(nil)

func NewFile(dir string) *File { return &File{dir: dir} }

var (
	blockRE             = regexp.MustCompile("(?ms)^```([a-z]+) ?([^\\n]*)\\n(.*?)^```[ \\t]*$")
	hashRE              = regexp.MustCompile(`(?m)^Plan hash: (\S+)`)
	understandingHashRE = regexp.MustCompile(`(?m)^Understanding hash: (\S+)`)
	unsafeR             = regexp.MustCompile(`[^A-Za-z0-9._-]`)
)

type block struct{ kind, id, text string }

// parseBlocks returns the fenced blocks of the given kind ("answer",
// "decision", "resolution") in file order.
func parseBlocks(data []byte, kind string) []block {
	var out []block
	for _, m := range blockRE.FindAllStringSubmatch(string(data), -1) {
		if m[1] == kind {
			out = append(out, block{kind: kind, id: strings.TrimSpace(m[2]), text: strings.TrimSpace(m[3])})
		}
	}
	return out
}

func (f *File) write(name, content string) error {
	if err := os.MkdirAll(f.dir, 0o700); err != nil {
		return fmt.Errorf("human: %w", err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(content), 0o600); err != nil {
		return fmt.Errorf("human: %w", err)
	}
	return nil
}

func (f *File) read(name string) ([]byte, bool, error) {
	data, err := os.ReadFile(filepath.Join(f.dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("human: %w", err)
	}
	return data, true, nil
}

// archive moves an answered file aside so the next call with the same file
// name starts a fresh conversation.
func (f *File) archive(name string) {
	answered := strings.TrimSuffix(name, ".md") + ".answered.md"
	os.Rename(filepath.Join(f.dir, name), filepath.Join(f.dir, answered))
}

func (f *File) Ask(ctx context.Context, qs []Question) ([]Answer, error) {
	const name = "QUESTIONS.md"
	data, found, err := f.read(name)
	if err != nil {
		return nil, err
	}
	if !found {
		var b strings.Builder
		b.WriteString("# Questions\n")
		if len(qs) > 0 && qs[0].Round > 0 {
			fmt.Fprintf(&b, "\nRound %d.\n", qs[0].Round)
		}
		b.WriteString("\nFill in each answer block, save the file, then run `gophermind brief resume`.\n")
		b.WriteString("A block that already holds text is the default answer; leave it to accept it.\n")
		b.WriteString("Write `accept` in a block to take the recommended answer shown for that question.\n")
		for i, q := range qs {
			fmt.Fprintf(&b, "\n## Question %d (id: %s)\n\n%s\n", i+1, q.ID, q.Text)
			if q.Why != "" {
				fmt.Fprintf(&b, "\nWhy it matters: %s\n", q.Why)
			}
			if len(q.Options) > 0 {
				fmt.Fprintf(&b, "\nOptions: %s\n", strings.Join(q.Options, " | "))
			}
			if q.Recommended != "" {
				fmt.Fprintf(&b, "\nRecommended: %s\n", q.Recommended)
				if q.RecommendedWhy != "" {
					fmt.Fprintf(&b, "Why: %s\n", q.RecommendedWhy)
				}
			}
			fmt.Fprintf(&b, "\n```answer %s\n%s\n```\n", q.ID, q.Default)
		}
		if err := f.write(name, b.String()); err != nil {
			return nil, err
		}
		return nil, ErrWaiting
	}
	blocks := parseBlocks(data, "answer")
	if len(blocks) != len(qs) {
		return nil, fmt.Errorf("human: %s has %d answer blocks but %d questions are being asked; delete it and run again", name, len(blocks), len(qs))
	}
	answers := make([]Answer, len(qs))
	for i, q := range qs {
		if blocks[i].id != q.ID {
			return nil, fmt.Errorf("human: %s block %d is for %q but question %q is being asked; delete it and run again", name, i+1, blocks[i].id, q.ID)
		}
		if blocks[i].text == "" {
			return nil, ErrWaiting
		}
		text := blocks[i].text
		if q.Recommended != "" && strings.EqualFold(text, "accept") {
			answers[i] = Answer{ID: q.ID, Text: q.Recommended, Accepted: true}
			continue
		}
		answers[i] = Answer{ID: q.ID, Text: text, Assumed: q.Default != "" && text == q.Default}
	}
	if err := validateAnswers(qs, answers); err != nil {
		return nil, err
	}
	f.archive(name)
	return answers, nil
}

func (f *File) Approve(ctx context.Context, plan PlanSummary) (Decision, error) {
	const name = "APPROVAL.md"
	data, found, err := f.read(name)
	if err != nil {
		return Decision{}, err
	}
	if found {
		if m := hashRE.FindSubmatch(data); m != nil && string(m[1]) == plan.Hash {
			blocks := parseBlocks(data, "decision")
			if len(blocks) != 1 {
				return Decision{}, fmt.Errorf("human: %s must contain exactly one decision block", name)
			}
			if blocks[0].text == "" {
				return Decision{}, ErrWaiting
			}
			approved, note, ok := parseDecision(blocks[0].text)
			if !ok {
				return Decision{}, fmt.Errorf("human: %s: write approve, or reject followed by a reason", name)
			}
			return Decision{Approved: approved, By: "file", Note: note}, nil
		}
		// The plan changed since the file was written: an old answer must not approve a new plan.
	}
	body := fmt.Sprintf("# Approval\n\nRead the plan, then write `approve` or `reject: <reason>` in the decision block and run `gophermind brief resume`.\n\n"+
		"Plan hash: %s\n\n---\n\n%s\n\n---\n\n```decision\n\n```\n", plan.Hash, plan.Markdown)
	if err := f.write(name, body); err != nil {
		return Decision{}, err
	}
	return Decision{}, ErrWaiting
}

func (f *File) Confirm(ctx context.Context, u Understanding) (Decision, error) {
	const name = "UNDERSTANDING.md"
	data, found, err := f.read(name)
	if err != nil {
		return Decision{}, err
	}
	if found {
		if m := understandingHashRE.FindSubmatch(data); m != nil && string(m[1]) == u.Hash {
			blocks := parseBlocks(data, "decision")
			if len(blocks) != 1 {
				return Decision{}, fmt.Errorf("human: %s must contain exactly one decision block", name)
			}
			if blocks[0].text == "" {
				return Decision{}, ErrWaiting
			}
			approved, note, ok := parseDecision(blocks[0].text)
			if !ok {
				return Decision{}, fmt.Errorf("human: %s: write approve, or reject followed by a reason", name)
			}
			return Decision{Approved: approved, By: "file", Note: note}, nil
		}
		// The understanding changed since the file was written: an old answer must not confirm a new one.
	}
	body := fmt.Sprintf("# Understanding\n\nRead it, then write `approve` or `reject: <reason>` in the decision block and run `gophermind brief resume`.\n\n"+
		"Understanding hash: %s\n\n---\n\n%s\n\n---\n\n```decision\n\n```\n", u.Hash, u.Markdown)
	if err := f.write(name, body); err != nil {
		return Decision{}, err
	}
	return Decision{}, ErrWaiting
}

func (f *File) Escalate(ctx context.Context, e Escalation) (Resolution, error) {
	name := "ESCALATION-" + unsafeR.ReplaceAllString(e.NodeID, "_") + ".md"
	data, found, err := f.read(name)
	if err != nil {
		return Resolution{}, err
	}
	if !found {
		var b strings.Builder
		fmt.Fprintf(&b, "# Escalation: %s\n\nEvery model failed this leaf: %s\n\n", e.NodeID, e.Reason)
		for _, h := range e.History {
			fmt.Fprintf(&b, "- %s\n", h)
		}
		b.WriteString("\nWrite `retry`, `skip`, or `stop` in the block, optionally followed by a note, then run `gophermind brief resume`.\n")
		b.WriteString("\n```resolution\n\n```\n")
		if err := f.write(name, b.String()); err != nil {
			return Resolution{}, err
		}
		return Resolution{}, ErrWaiting
	}
	blocks := parseBlocks(data, "resolution")
	if len(blocks) != 1 {
		return Resolution{}, fmt.Errorf("human: %s must contain exactly one resolution block", name)
	}
	if blocks[0].text == "" {
		return Resolution{}, ErrWaiting
	}
	action, note, ok := parseAction(blocks[0].text)
	if !ok {
		return Resolution{}, fmt.Errorf("human: %s: write retry, skip, or stop", name)
	}
	f.archive(name)
	return Resolution{Action: action, Note: note, AnsweredBy: AnsweredByHuman}, nil
}
