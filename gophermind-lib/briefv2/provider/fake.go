package provider

import (
	"context"
	"sync"
)

// Fake is a scripted provider for tests. fn receives the 1-based call number
// and the request and returns the reply or a typed error.
type Fake struct {
	name   string
	models []ModelInfo
	fn     func(call int, req Request) (Response, error)

	mu   sync.Mutex
	n    int
	reqs []Request
}

var _ Provider = (*Fake)(nil)

// NewFake builds a Fake. A nil fn answers every call with an empty reply that
// names the requested model.
func NewFake(name string, models []ModelInfo, fn func(call int, req Request) (Response, error)) *Fake {
	return &Fake{name: name, models: models, fn: fn}
}

func (f *Fake) Name() string        { return f.name }
func (f *Fake) Models() []ModelInfo { return f.models }

func (f *Fake) Complete(ctx context.Context, req Request) (Response, error) {
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	f.mu.Lock()
	f.n++
	n := f.n
	f.reqs = append(f.reqs, req)
	f.mu.Unlock()
	if f.fn == nil {
		return Response{Model: req.Model}, nil
	}
	return f.fn(n, req)
}

// Calls is how many times Complete reached the script.
func (f *Fake) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.n
}

// Requests returns a copy of every request received, oldest first.
func (f *Fake) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.reqs...)
}
