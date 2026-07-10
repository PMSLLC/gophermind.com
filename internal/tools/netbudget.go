package tools

import (
	"fmt"
	"sync"
)

// NetBudget bounds network-tool usage across a session: a maximum number of
// requests and a maximum total number of response bytes. A zero limit means
// "no limit" for that dimension, and a nil *NetBudget is entirely unlimited
// (the default). Safe for concurrent use.
type NetBudget struct {
	mu          sync.Mutex
	maxRequests int
	maxBytes    int64
	requests    int
	bytes       int64
}

// NewNetBudget returns a budget with the given caps (0 = unlimited for that
// dimension).
func NewNetBudget(maxRequests int, maxBytes int64) *NetBudget {
	return &NetBudget{maxRequests: maxRequests, maxBytes: maxBytes}
}

// begin accounts for one request, returning an error if the request budget is
// exhausted. A nil budget is always allowed.
func (b *NetBudget) begin() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.maxRequests > 0 && b.requests >= b.maxRequests {
		return fmt.Errorf("network budget exceeded: request limit of %d reached", b.maxRequests)
	}
	b.requests++
	return nil
}

// add accounts for n response bytes, returning an error if the byte budget is
// exceeded. A nil budget is always allowed.
func (b *NetBudget) add(n int) error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.bytes += int64(n)
	if b.maxBytes > 0 && b.bytes > b.maxBytes {
		return fmt.Errorf("network budget exceeded: byte limit of %d reached", b.maxBytes)
	}
	return nil
}
