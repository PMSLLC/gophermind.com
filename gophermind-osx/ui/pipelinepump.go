package ui

import (
	"context"
	"errors"
	"io"

	"gophermind/gophermind-lib/runview"
	"gophermind/gophermind-osx/client"
)

// PipelinePump reads a live client.EventStream from client.Client.
// PipelineEvents and applies each frame to a PipelineState -- the pipeline
// counterpart to StreamPump (stream.go), same shape: Run blocks and is meant
// to be called in its own goroutine, and always closes stream before
// returning. The pipeline stream has no "done" event and no natural end while
// the server is up; Run returns nil on a clean EOF (the connection closing)
// and the caller (PipelineFollower) reconnects from State.LastSeq().
type PipelinePump struct {
	State *PipelineState
}

// Run reads events from stream until it ends (io.EOF) or ctx is done,
// applying each to State. See StreamPump.Run's doc comment for why each Next()
// call runs in its own goroutine raced against ctx.Done().
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

// apply decodes one frame into State and then advances the stream position, even
// for a frame it could not decode: the line was consumed, and a reconnect must
// not replay it forever.
func (p *PipelinePump) apply(ev client.Event) {
	// Node frames at or below the snapshot's event_seq are already in the nodes
	// (a reconnect replays from an older position than the refetched snapshot).
	// Wave, run-status and run-report frames are derived, share the latest
	// line's id and are absolute sets, so they are always applied.
	replayed := ev.ID <= p.State.SnapSeq()
	switch ev.Type {
	case runview.EventNodeStatus:
		if replayed {
			break
		}
		if e, err := ev.NodeStatus(); err == nil {
			p.State.ApplyNodeStatus(e.ID, e.Status, e.Wave, e.Revision)
		}
	case runview.EventNodeAttempt:
		if replayed {
			break
		}
		if e, err := ev.NodeAttempt(); err == nil {
			p.State.ApplyNodeAttempt(e.ID, e.Attempt)
		}
	case runview.EventNodeResult:
		if replayed {
			break
		}
		if e, err := ev.NodeResult(); err == nil {
			p.State.ApplyNodeResult(e.ID, e.Result)
		}
	case runview.EventWaveChanged:
		if e, err := ev.WaveChanged(); err == nil {
			p.State.ApplyWaveChanged(e.Wave, e.State)
		}
	case runview.EventRunStatus:
		if e, err := ev.RunStatus(); err == nil {
			p.State.ApplyRunStatus(e.Status, e.StopReason)
		}
	case runview.EventRunReport:
		if r, err := ev.RunReport(); err == nil {
			p.State.SetReport(r)
		}
	}
	p.State.SetLastSeq(ev.ID)
}
