package jobs

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunConcurrentCompletesAll(t *testing.T) {
	q := New()
	for i := 0; i < 6; i++ {
		q.Add("task")
	}
	q.RunConcurrent(context.Background(), func(context.Context, string) (string, error) {
		return "ok", nil
	}, 3, nil)

	done, failed, pending := q.Counts()
	if done != 6 || failed != 0 || pending != 0 {
		t.Errorf("counts = %d/%d/%d, want 6/0/0", done, failed, pending)
	}
}

func TestRunConcurrentRespectsLimit(t *testing.T) {
	q := New()
	for i := 0; i < 8; i++ {
		q.Add("task")
	}
	var cur, max int32
	q.RunConcurrent(context.Background(), func(context.Context, string) (string, error) {
		n := atomic.AddInt32(&cur, 1)
		for {
			m := atomic.LoadInt32(&max)
			if n <= m || atomic.CompareAndSwapInt32(&max, m, n) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		atomic.AddInt32(&cur, -1)
		return "ok", nil
	}, 3, nil)

	if max > 3 {
		t.Errorf("max concurrency = %d, want <= 3", max)
	}
	if done, _, _ := q.Counts(); done != 8 {
		t.Errorf("done = %d, want 8", done)
	}
}

func TestRunConcurrentRecordsFailures(t *testing.T) {
	q := New()
	q.Add("good")
	q.Add("bad")
	q.RunConcurrent(context.Background(), func(_ context.Context, task string) (string, error) {
		if task == "bad" {
			return "", context.Canceled
		}
		return "ok", nil
	}, 2, nil)
	if _, failed, _ := q.Counts(); failed != 1 {
		t.Errorf("failed = %d, want 1", failed)
	}
}
