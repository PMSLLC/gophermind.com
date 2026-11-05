package planner

import (
	"context"
	"fmt"
	"testing"
)

func TestRecordRefusalSkipsACancelledContext(t *testing.T) {
	p := &Planner{}
	p.recordRefusal(nil, "testwrite:x", "n", fmt.Errorf("call: %w", context.Canceled))
	p.recordRefusal(nil, "testwrite:x", "n", context.DeadlineExceeded)
	if len(p.refusals) != 0 {
		t.Errorf("refusals = %v, want none", p.refusals)
	}
}
