package human

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Terminal asks on out and reads answers from in (stderr and stdin in the
// real command).
type Terminal struct {
	in  *bufio.Reader
	out io.Writer
}

var _ Gate = (*Terminal)(nil)

func NewTerminal(in io.Reader, out io.Writer) *Terminal {
	return &Terminal{in: bufio.NewReader(in), out: out}
}

// readLine returns the next line without its newline. A last line with no
// newline still counts; io.EOF is returned only when nothing was read.
func (t *Terminal) readLine(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s, err := t.in.ReadString('\n')
	if err == io.EOF && s != "" {
		err = nil
	}
	return strings.TrimRight(s, "\r\n"), err
}

func (t *Terminal) Ask(ctx context.Context, qs []Question) ([]Answer, error) {
	out := make([]Answer, 0, len(qs))
	for i, q := range qs {
		fmt.Fprintf(t.out, "\nQuestion %d of %d: %s\n", i+1, len(qs), q.Text)
		for j, o := range q.Options {
			fmt.Fprintf(t.out, "  %d) %s\n", j+1, o)
		}
		if q.Default != "" {
			fmt.Fprintf(t.out, "  (press Enter for: %s)\n", q.Default)
		}
		for {
			fmt.Fprint(t.out, "> ")
			line, err := t.readLine(ctx)
			line = strings.TrimSpace(line)
			if err != nil && line == "" {
				if errors.Is(err, io.EOF) && q.Default != "" {
					out = append(out, Answer{ID: q.ID, Text: q.Default, Assumed: true})
					break
				}
				if errors.Is(err, io.EOF) {
					return nil, fmt.Errorf("human: input ended before question %q was answered", q.ID)
				}
				return nil, err
			}
			if line == "" {
				if q.Default != "" {
					out = append(out, Answer{ID: q.ID, Text: q.Default, Assumed: true})
					break
				}
				fmt.Fprintln(t.out, "An answer is required.")
				continue
			}
			if n, convErr := strconv.Atoi(line); convErr == nil && n >= 1 && n <= len(q.Options) {
				line = q.Options[n-1]
			}
			out = append(out, Answer{ID: q.ID, Text: line})
			break
		}
	}
	return out, validateAnswers(qs, out)
}

func (t *Terminal) Approve(ctx context.Context, plan PlanSummary) (Decision, error) {
	fmt.Fprintf(t.out, "\n%s\n\nPlan hash: %s\n", plan.Markdown, plan.Hash)
	for {
		fmt.Fprint(t.out, "Approve this plan? [y/N] ")
		line, err := t.readLine(ctx)
		if err != nil && strings.TrimSpace(line) == "" {
			if errors.Is(err, io.EOF) {
				return Decision{}, errors.New("human: input ended before the plan was approved or rejected")
			}
			return Decision{}, err
		}
		if strings.TrimSpace(line) == "" {
			return Decision{Approved: false, By: "terminal"}, nil
		}
		if approved, note, ok := parseDecision(line); ok {
			return Decision{Approved: approved, By: "terminal", Note: note}, nil
		}
		fmt.Fprintln(t.out, "Answer y or n, optionally followed by a note.")
	}
}

func (t *Terminal) Escalate(ctx context.Context, e Escalation) (Resolution, error) {
	fmt.Fprintf(t.out, "\nEvery model failed %s: %s\n", e.NodeID, e.Reason)
	for _, h := range e.History {
		fmt.Fprintf(t.out, "  - %s\n", h)
	}
	for {
		fmt.Fprint(t.out, "retry, skip, or stop (add a note after it if you like): ")
		line, err := t.readLine(ctx)
		if err != nil && strings.TrimSpace(line) == "" {
			if errors.Is(err, io.EOF) {
				return Resolution{}, errors.New("human: input ended before the escalation was resolved")
			}
			return Resolution{}, err
		}
		if action, note, ok := parseAction(line); ok {
			return Resolution{Action: action, Note: note}, nil
		}
		fmt.Fprintln(t.out, "Type retry, skip, or stop.")
	}
}
