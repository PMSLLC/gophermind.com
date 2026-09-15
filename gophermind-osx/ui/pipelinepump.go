package ui

import (
	"context"
	"errors"
	"io"

	"gophermind/gophermind-lib/phaseflow"
	"gophermind/gophermind-osx/client"
)

// PipelinePump reads a live client.EventStream from client.Client.
// PipelineEvents and applies each frame to a PipelineState -- the pipeline
// counterpart to StreamPump (stream.go), same shape: Run blocks and is
// meant to be called in its own goroutine, and always closes stream
// before returning. Unlike a chat/run stream, gophermind-server's pipeline
// events stream has no "done" event and no natural end while the server is
// up; Run still returns nil on a clean EOF (the connection closing) for
// symmetry with StreamPump, and the caller (the cgo widget layer) decides
// whether to reconnect.
type PipelinePump struct {
	State *PipelineState
}

// Run reads events from stream until it ends (io.EOF) or ctx is done,
// applying each to State. See StreamPump.Run's doc comment for why each
// Next() call runs in its own goroutine raced against ctx.Done() -- the
// same blocked-I/O cancellation problem applies here.
func (p *PipelinePump) Run(ctx context.Context, stream *client.EventStream) error {
	defer stream.Close()

	type result struct {
		ev  client.Event
		err error
	}

	for {
		nextCh := make(chan result, 1)
		go func() {
			ev, err := stream.Next()
			nextCh <- result{ev, err}
		}()

		select {
		case <-ctx.Done():
			stream.Close()
			<-nextCh
			return ctx.Err()
		case r := <-nextCh:
			if errors.Is(r.err, io.EOF) {
				return nil
			}
			if r.err != nil {
				return r.err
			}
			p.apply(r.ev)
		}
	}
}

func (p *PipelinePump) apply(ev client.Event) {
	switch ev.Type {
	case "task-status":
		if e, err := ev.TaskStatus(); err == nil {
			p.State.ApplyTaskStatus(e.ID, e.Status, e.Wave)
		}
	case "task-attempt":
		if e, err := ev.TaskAttempt(); err == nil {
			// StartedAt isn't part of TaskAttemptEvent's wire shape (see
			// gophermind-lib/serve/events.go); zero value is fine here,
			// the attempt history's own Duration/Verdict/Reason are what
			// 04-06's acceptance criteria actually display.
			p.State.ApplyTaskAttempt(e.TaskID, phaseflow.Attempt{
				Model:    e.Model,
				Duration: e.Duration,
				Verdict:  e.Verdict,
				Reason:   e.Reason,
			})
		}
	case "wave-changed":
		if e, err := ev.WaveChanged(); err == nil {
			p.State.ApplyWaveChanged(e.Wave, e.State)
		}
	case "run-report":
		if r, err := ev.RunReport(); err == nil {
			p.State.SetReport(r)
		}
	}
}
