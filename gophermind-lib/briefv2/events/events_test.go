package events_test

import (
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/ledger"
)

func TestCollectorKeepsOrderAndStampsTime(t *testing.T) {
	c := events.NewCollector()
	c.Emit(events.Event{Kind: events.KindStageStarted, Stage: "clarify"})
	fixed := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	c.Emit(events.Event{Kind: events.KindWarning, Message: "w", At: fixed})
	c.Emit(events.Event{Kind: events.KindCall, Call: &ledger.Call{ID: 7}})

	got := c.Events()
	if len(got) != 3 || got[0].Stage != "clarify" || got[1].Message != "w" || got[2].Call.ID != 7 {
		t.Fatalf("events = %+v", got)
	}
	if got[0].At.IsZero() {
		t.Error("an event without a time should be stamped")
	}
	if !got[1].At.Equal(fixed) {
		t.Errorf("an event with a time must keep it, got %v", got[1].At)
	}
	if n := len(c.OfKind(events.KindWarning)); n != 1 {
		t.Errorf("OfKind(warning) = %d, want 1", n)
	}
}

func TestCollectorReturnsACopy(t *testing.T) {
	c := events.NewCollector()
	c.Emit(events.Event{Kind: events.KindWarning, Message: "a"})
	first := c.Events()
	first[0].Message = "changed"
	if c.Events()[0].Message != "a" {
		t.Error("mutating the returned slice changed the collector")
	}
}

func TestCollectorIsSafeForConcurrentUse(t *testing.T) {
	c := events.NewCollector()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Emit(events.Event{Kind: events.KindWarning})
			_ = c.Events()
		}()
	}
	wg.Wait()
	if n := len(c.Events()); n != 50 {
		t.Errorf("collected %d events, want 50", n)
	}
}

func TestNopDiscards(t *testing.T) {
	events.Nop.Emit(events.Event{Kind: events.KindWarning}) // must not panic
}
