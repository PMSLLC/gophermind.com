package secaudit

import (
	"context"

	"gophermind/internal/llm"
)

// Options tunes an audit run.
type Options struct {
	// Reviewer, when set, verifies each static finding. Nil means static-only.
	Reviewer Reviewer
}

// Audit runs the full hunt over root: the deterministic static scan, then the
// optional model verification, returning a Report. The static scan is always
// run; the verification is additive and never removes findings.
func Audit(ctx context.Context, root string, opts Options) (*Report, error) {
	findings, err := ScanStatic(root)
	if err != nil {
		return nil, err
	}
	if opts.Reviewer != nil {
		findings = Verify(ctx, root, findings, opts.Reviewer)
	}
	return &Report{Target: root, Findings: findings}, nil
}

// ClientReviewer adapts an *llm.Client to the Reviewer interface: a single
// stateless completion per finding, so verification never accumulates context
// across findings (which would blow a small local window).
type ClientReviewer struct {
	Client *llm.Client
}

// Review sends the prompt as a one-shot user message and returns the reply text.
func (c ClientReviewer) Review(ctx context.Context, prompt string) (string, error) {
	msg, _, err := c.Client.Complete(ctx, []llm.Message{{Role: "user", Content: prompt}}, nil)
	if err != nil {
		return "", err
	}
	return msg.Content, nil
}
