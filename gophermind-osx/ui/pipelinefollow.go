package ui

import (
	"context"
	"errors"
	"time"

	"gophermind/gophermind-osx/client"
)

// OpenEventsFunc opens the pipeline event stream of a run from an event id,
// normally client.PipelineEvents behind the live connection.
type OpenEventsFunc func(ctx context.Context, run string, since int64) (*client.EventStream, error)

// Messages the follower leaves in State when it gives up on a stream.
const (
	// StreamLineTooLongMessage is shown when one event line is over the 8 MiB
	// the client reads; reconnecting would meet the same line again.
	StreamLineTooLongMessage = "The server sent a pipeline event larger than this app can read. Update the app."
	// ResponseTooLargeMessage is shown when a response is over the client's cap.
	ResponseTooLargeMessage = "The server sent a pipeline response larger than this app can read."
)

// PipelineFollower keeps a PipelinePump running: it opens the stream at
// State.LastSeq(), pumps it until it drops, waits with a backoff and opens it
// again at the new LastSeq, so a dropped connection loses and repeats nothing.
// A failure a retry cannot fix is not retried: a schema mismatch, a response
// over the client's size cap (client.ErrResponseTooLarge) and an event line over
// 8 MiB (client.ErrSSELineTooLong) each set State's message and return the
// error. Every other error reconnects with backoff. Run returns ctx's error
// when ctx ends. The follower logs nothing, so no token can reach a log.
type PipelineFollower struct {
	State *PipelineState
	Open  OpenEventsFunc
	// OnOpen, when set, is called on every successful (re)connect, after the
	// stream is open and before it is read. The server derives wave, run-status
	// and run-report events from files and never replays them, so a run that
	// moved or ended while the app was away is only visible in a fresh fetch:
	// the app uses this hook to refetch the state, and the report when the run
	// status is terminal. The hook runs on the follower's goroutine; the stream
	// is read after it returns. SetState keeps the stream position forward-only,
	// so a refetch cannot rewind it.
	OnOpen func(ctx context.Context)
	// Wait is the delay before reconnect attempt n (0-based). Nil means 1s,
	// doubling to 15s. Progress (a new event id) resets n.
	Wait func(n int) time.Duration
}

func (f *PipelineFollower) wait(n int) time.Duration {
	if f.Wait != nil {
		return f.Wait(n)
	}
	if n > 4 {
		return 15 * time.Second
	}
	d := time.Second << uint(n)
	if d > 15*time.Second {
		d = 15 * time.Second
	}
	return d
}

// fatal reports whether err can never succeed on a retry, and sets the panel
// message for it.
func (f *PipelineFollower) fatal(err error) bool {
	var mm *client.SchemaMismatchError
	switch {
	case errors.As(err, &mm):
		f.State.SetMessage(client.SchemaMismatchMessage)
	case errors.Is(err, client.ErrSSELineTooLong):
		f.State.SetMessage(StreamLineTooLongMessage)
	case errors.Is(err, client.ErrResponseTooLarge):
		f.State.SetMessage(ResponseTooLargeMessage)
	default:
		return false
	}
	return true
}

// Run follows until ctx ends or a failure that a retry cannot fix.
func (f *PipelineFollower) Run(ctx context.Context) error {
	pump := &PipelinePump{State: f.State}
	for n := 0; ; n++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		run := f.State.RunID()
		stream, err := f.Open(ctx, run, f.State.LastSeq())
		if err != nil {
			if f.fatal(err) {
				return err
			}
		} else {
			if f.OnOpen != nil {
				f.OnOpen(ctx)
			}
			if f.State.RunID() != run {
				// The refetch moved to another run: this stream is the old run's.
				// Drop it unread and reconnect at once for the new run.
				stream.Close()
				n = -1
				continue
			}
			before := f.State.LastSeq()
			perr := pump.Run(ctx, stream)
			if perr != nil && ctx.Err() == nil && f.fatal(perr) {
				return perr
			}
			if f.State.LastSeq() > before {
				n = 0
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(f.wait(n)):
		}
	}
}
