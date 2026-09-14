package jobs

import (
	"context"
	"sync"
)

// RunConcurrent runs all pending jobs concurrently, bounded to at most
// `concurrency` at once, updating each job's status as it progresses. This is
// the fleet/overseer execution mode: many supervised sessions in flight, sharing
// the same runner (and thus the same approval policy). ctx cancellation stops
// scheduling further jobs; in-flight jobs observe ctx via the runner. onUpdate,
// when non-nil, is called (possibly from multiple goroutines) after each status
// change.
func (q *Queue) RunConcurrent(ctx context.Context, run Runner, concurrency int, onUpdate func(*Job)) {
	if concurrency < 1 {
		concurrency = 1
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex

	notify := func(j *Job) {
		if onUpdate != nil {
			onUpdate(j)
		}
	}

	for _, j := range q.jobs {
		if j.Status != Pending {
			continue
		}
		if ctx.Err() != nil {
			break // cancelled: leave the rest pending
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(j *Job) {
			defer wg.Done()
			defer func() { <-sem }()

			mu.Lock()
			j.Status = Running
			mu.Unlock()
			notify(j)

			result, err := run(ctx, j.Task)

			mu.Lock()
			if err != nil {
				j.Status = Failed
				j.Err = err.Error()
			} else {
				j.Status = Done
				j.Result = result
			}
			mu.Unlock()
			notify(j)
		}(j)
	}
	wg.Wait()
}
