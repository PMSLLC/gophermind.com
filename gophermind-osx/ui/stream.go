package ui

import (
	"context"
	"errors"
	"io"

	"gophermind/gophermind-osx/client"
)

// StreamPump reads a live client.EventStream and applies each event to a
// Transcript, covering "SSE reading in goroutine, UI updates via channel":
// Run is meant to be called in its own goroutine; Transcript's OnChange
// callback (set by the widget layer) is how those updates actually reach
// the UI thread -- see chatview.go, which dispatches it through
// uiQueueMain rather than touching widgets directly from this goroutine.
type StreamPump struct {
	Transcript *Transcript
	// OnApprovalNeeded, if set, is called for an "approval-needed" event.
	// Approval resolution itself (POST /session/{id}/approve) is the
	// caller's responsibility -- this pump only surfaces the event; it has
	// no reference to the client.Client needed to resolve it, since a
	// resolution normally comes from user interaction, not automatically.
	OnApprovalNeeded func(client.Event)
}

// Run reads events from stream until it ends (io.EOF, a "done" event) or
// ctx is done, applying each to Transcript. Blocks; the caller runs it in
// a goroutine. Always closes stream before returning. Returns nil for a
// normal end (EOF or "done"), otherwise the error that ended the stream
// (ctx.Err() on cancellation).
//
// ctx is NOT the context the underlying HTTP request was opened with --
// that one was fixed when the caller opened stream (e.g. via
// client.Client.Stream/RunStream/PipelineEvents) and cancelling THIS ctx
// has no direct effect on an in-flight stream.Next() blocked reading from
// a connection that has simply gone quiet (a live SSE connection with no
// new event yet, not a closed one). So each Next() call runs in its own
// goroutine, raced against ctx.Done(); on cancellation, stream.Close() is
// called explicitly to force that blocked read to fail and unblock the
// goroutine (the standard Go pattern for cancelling blocked I/O), which is
// what actually makes "No UI freezes" true even when nothing has arrived
// on the wire in a while, not just when the caller respects an error
// returned between calls to Next().
func (p *StreamPump) Run(ctx context.Context, stream *client.EventStream) error {
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
			stream.Close() // unblocks the goroutine's in-flight Next()
			<-nextCh       // drain so it doesn't leak
			return ctx.Err()
		case r := <-nextCh:
			if errors.Is(r.err, io.EOF) {
				return nil
			}
			if r.err != nil {
				return r.err
			}
			p.apply(r.ev)
			if r.ev.Type == "done" {
				return nil
			}
		}
	}
}

func (p *StreamPump) apply(ev client.Event) {
	switch ev.Type {
	case "token":
		p.Transcript.AppendToken(ev.Data)
	case "assistant":
		p.Transcript.AddAssistantText(ev.Data)
	case "tool_call":
		if tc, err := ev.ToolCall(); err == nil {
			p.Transcript.AddToolCall(tc.Name, tc.Args)
		}
	case "tool_result":
		if tr, err := ev.ToolResult(); err == nil {
			p.Transcript.AddToolResult(tr.Name, tr.Text)
		}
	case "approval-needed":
		if p.OnApprovalNeeded != nil {
			p.OnApprovalNeeded(ev)
		}
	case "error":
		p.Transcript.AddSystem("error: " + ev.Data)
	case "done", "usage", "model-switched":
		// done: nothing to render (the turn's content already arrived via
		// token/assistant events). usage/model-switched: not shown in the
		// transcript; a future settings/status panel is the natural home
		// for usage, not the message list.
	}
}
