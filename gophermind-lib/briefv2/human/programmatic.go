package human

import (
	"context"
	"errors"
)

// Kind says which call a Request is.
type Kind string

const (
	KindAsk      Kind = "ask"
	KindApprove  Kind = "approve"
	KindEscalate Kind = "escalate"
)

type reply struct {
	answers    []Answer
	decision   Decision
	resolution Resolution
	err        error
}

// Request is one pending question for a person, published on
// Programmatic.Requests. Exactly one of Answer, Decide, Resolve, or Fail
// completes it (the matching one for its Kind); a second call does nothing.
type Request struct {
	Kind       Kind
	Questions  []Question
	Plan       PlanSummary
	Escalation Escalation
	reply      chan reply
}

func (r *Request) send(x reply) {
	select {
	case r.reply <- x:
	default:
	}
}

func (r *Request) Answer(as []Answer)   { r.send(reply{answers: as}) }
func (r *Request) Decide(d Decision)    { r.send(reply{decision: d}) }
func (r *Request) Resolve(x Resolution) { r.send(reply{resolution: x}) }
func (r *Request) Fail(err error)       { r.send(reply{err: err}) }

// Programmatic is the gate the run service and the desktop app use: requests
// are published as values and answered by calling methods on them.
type Programmatic struct{ reqs chan *Request }

var _ Gate = (*Programmatic)(nil)

func NewProgrammatic() *Programmatic { return &Programmatic{reqs: make(chan *Request, 16)} }

// Requests delivers each pending request, oldest first.
func (p *Programmatic) Requests() <-chan *Request { return p.reqs }

func (p *Programmatic) roundTrip(ctx context.Context, r *Request) (reply, error) {
	r.reply = make(chan reply, 1)
	select {
	case p.reqs <- r:
	case <-ctx.Done():
		return reply{}, ctx.Err()
	}
	select {
	case x := <-r.reply:
		return x, x.err
	case <-ctx.Done():
		return reply{}, ctx.Err()
	}
}

func (p *Programmatic) Ask(ctx context.Context, qs []Question) ([]Answer, error) {
	x, err := p.roundTrip(ctx, &Request{Kind: KindAsk, Questions: qs})
	if err != nil {
		return nil, err
	}
	if err := validateAnswers(qs, x.answers); err != nil {
		return nil, err
	}
	return x.answers, nil
}

func (p *Programmatic) Approve(ctx context.Context, plan PlanSummary) (Decision, error) {
	x, err := p.roundTrip(ctx, &Request{Kind: KindApprove, Plan: plan})
	if err != nil {
		return Decision{}, err
	}
	if x.decision.By == "" {
		x.decision.By = "programmatic"
	}
	return x.decision, nil
}

func (p *Programmatic) Escalate(ctx context.Context, e Escalation) (Resolution, error) {
	x, err := p.roundTrip(ctx, &Request{Kind: KindEscalate, Escalation: e})
	if err != nil {
		return Resolution{}, err
	}
	switch x.resolution.Action {
	case ActionRetry, ActionSkip, ActionStop:
		return x.resolution, nil
	}
	return Resolution{}, errors.New("human: resolution needs an action of retry, skip, or stop")
}
