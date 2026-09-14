// Package jobs implements a simple sequential task queue with per-job status
// tracking, so a batch of prompts can be enqueued and run with a visible
// progress/status trail.
package jobs

import "context"

// Status is a job's lifecycle state.
type Status string

const (
	Pending Status = "pending"
	Running Status = "running"
	Done    Status = "done"
	Failed  Status = "failed"
)

// Job is one queued task and its outcome.
type Job struct {
	ID     int
	Task   string
	Status Status
	Result string
	Err    string
}

// Queue holds an ordered set of jobs.
type Queue struct {
	jobs   []*Job
	nextID int
}

// New returns an empty queue.
func New() *Queue { return &Queue{} }

// Add enqueues a task and returns its job.
func (q *Queue) Add(task string) *Job {
	q.nextID++
	j := &Job{ID: q.nextID, Task: task, Status: Pending}
	q.jobs = append(q.jobs, j)
	return j
}

// Jobs returns the jobs in insertion order.
func (q *Queue) Jobs() []*Job { return q.jobs }

// Runner executes one task and returns its result or an error.
type Runner func(ctx context.Context, task string) (string, error)

// Run executes each pending job sequentially, updating status as it goes. It
// stops early if ctx is cancelled (remaining jobs stay pending). onUpdate, when
// non-nil, is called after every status change so callers can render progress.
func (q *Queue) Run(ctx context.Context, run Runner, onUpdate func(*Job)) {
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
			return // cancelled: leave the rest pending
		}
		j.Status = Running
		notify(j)

		result, err := run(ctx, j.Task)
		if err != nil {
			j.Status = Failed
			j.Err = err.Error()
		} else {
			j.Status = Done
			j.Result = result
		}
		notify(j)
	}
}

// Pending returns how many jobs are still pending.
func (q *Queue) Pending() int {
	_, _, p := q.Counts()
	return p
}

// Counts returns the number of done, failed, and pending jobs.
func (q *Queue) Counts() (done, failed, pending int) {
	for _, j := range q.jobs {
		switch j.Status {
		case Done:
			done++
		case Failed:
			failed++
		case Pending, Running:
			pending++
		}
	}
	return done, failed, pending
}
