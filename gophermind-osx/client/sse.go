package client

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"gophermind/gophermind-lib/runview"
	"gophermind/gophermind-lib/serve"
)

// Event is one parsed SSE frame: the "event:" line's value and the
// "data:" line(s)' payload (multiple data: lines are joined with "\n",
// per the SSE spec -- writeSSEEvent on the server side, which every
// gophermind-server stream route uses, never emits more than one data:
// line per frame today, but a spec-compliant reader still has to handle
// it). Type is one of the contract's named events (see the package doc
// comment): token, assistant, tool_call, tool_result, usage,
// approval-needed, model-switched, error, done.
type Event struct {
	Type string
	Data string
	// ID is the frame's "id:" value (the pipeline stream sends the line number
	// of the run's event log), or 0 when the frame has none.
	ID int64
}

// ToolCall decodes Data as a serve.ToolCallEvent. Call only when Type ==
// "tool_call"; decoding any other event type's Data this way will fail or
// silently produce a zero-value/partial result, since the shapes differ.
func (e Event) ToolCall() (serve.ToolCallEvent, error) {
	var v serve.ToolCallEvent
	err := json.Unmarshal([]byte(e.Data), &v)
	return v, err
}

// ToolResult decodes Data as a serve.ToolResultEvent (Type == "tool_result").
func (e Event) ToolResult() (serve.ToolResultEvent, error) {
	var v serve.ToolResultEvent
	err := json.Unmarshal([]byte(e.Data), &v)
	return v, err
}

// Usage decodes Data as a serve.UsageEvent (Type == "usage").
func (e Event) Usage() (serve.UsageEvent, error) {
	var v serve.UsageEvent
	err := json.Unmarshal([]byte(e.Data), &v)
	return v, err
}

// ApprovalNeeded decodes Data as a serve.ApprovalNeededEvent (Type ==
// "approval-needed").
func (e Event) ApprovalNeeded() (serve.ApprovalNeededEvent, error) {
	var v serve.ApprovalNeededEvent
	err := json.Unmarshal([]byte(e.Data), &v)
	return v, err
}

// ModelSwitched decodes Data as a serve.ModelSwitchedEvent (Type ==
// "model-switched"). No gophermind-server code path emits this event yet
// (see serve.ModelSwitchedEvent's own doc comment) -- this decoder exists
// so a caller is ready for it regardless.
func (e Event) ModelSwitched() (serve.ModelSwitchedEvent, error) {
	var v serve.ModelSwitchedEvent
	err := json.Unmarshal([]byte(e.Data), &v)
	return v, err
}

// NodeStatus, NodeAttempt, NodeResult, WaveChanged, RunStatus and RunReport
// decode Data for PipelineEvents' stream, whose vocabulary (node-status,
// node-attempt, node-result, wave-changed, run-status, run-report) differs from
// a session or run stream's. The types come from gophermind-lib/runview.
func (e Event) NodeStatus() (runview.NodeStatusData, error) {
	var v runview.NodeStatusData
	err := json.Unmarshal([]byte(e.Data), &v)
	return v, err
}

func (e Event) NodeAttempt() (runview.NodeAttemptData, error) {
	var v runview.NodeAttemptData
	err := json.Unmarshal([]byte(e.Data), &v)
	return v, err
}

func (e Event) NodeResult() (runview.NodeResultData, error) {
	var v runview.NodeResultData
	err := json.Unmarshal([]byte(e.Data), &v)
	return v, err
}

func (e Event) WaveChanged() (runview.WaveChangedData, error) {
	var v runview.WaveChangedData
	err := json.Unmarshal([]byte(e.Data), &v)
	return v, err
}

func (e Event) RunStatus() (runview.RunStatusData, error) {
	var v runview.RunStatusData
	err := json.Unmarshal([]byte(e.Data), &v)
	return v, err
}

// RunReport decodes the run-report event: the same body GET /pipeline/report
// returns.
func (e Event) RunReport() (runview.ReportBody, error) {
	var v runview.ReportBody
	err := json.Unmarshal([]byte(e.Data), &v)
	return v, err
}

// EventStream is a live SSE connection. Callers must call Close when done
// (or cancel the context passed to whichever method opened it); Next
// returns io.EOF when the server ends the stream normally (after a "done"
// event on a session/run stream, or never, for the long-lived pipeline
// events stream).
type EventStream struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
	header  http.Header
	lastID  int64
}

// Header is the response header the stream was opened with.
func (s *EventStream) Header() http.Header { return s.header }

// LastID is the id of the last frame Next returned that carried one. A caller
// that reconnects passes it as since.
func (s *EventStream) LastID() int64 { return s.lastID }

func (s *EventStream) frame(event string, data []string, id int64) Event {
	if id > 0 {
		s.lastID = id
	}
	return Event{Type: event, Data: strings.Join(data, "\n"), ID: id}
}

// Close closes the underlying connection. Safe to call more than once.
func (s *EventStream) Close() error {
	return s.body.Close()
}

// Next reads and returns the next event. Returns io.EOF (wrapped or bare;
// check with errors.Is) when the stream ends without a read error.
func (s *EventStream) Next() (Event, error) {
	var event string
	var data []string
	var id int64
	haveContent := false

	for s.scanner.Scan() {
		line := s.scanner.Text()
		if line == "" {
			if haveContent {
				return s.frame(event, data, id), nil
			}
			continue // a blank line before any field is not a frame boundary
		}
		if v, ok := sseField(line, "event"); ok {
			event = v
			haveContent = true
			continue
		}
		if v, ok := sseField(line, "data"); ok {
			data = append(data, v)
			haveContent = true
			continue
		}
		if v, ok := sseField(line, "id"); ok {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				id = n
			}
			continue
		}
		// Any other field (retry:, or a comment starting with ':') is
		// valid SSE this client doesn't currently use; ignored rather than
		// treated as an error, per the SSE spec's forward-compatibility rule.
	}
	if err := s.scanner.Err(); err != nil {
		return Event{}, err
	}
	// A trailing frame with no terminating blank line (the connection just
	// closed) is still real data -- flush it rather than silently dropping
	// the last event.
	if haveContent {
		return s.frame(event, data, id), nil
	}
	return Event{}, io.EOF
}

// sseField reports whether line is an SSE "field:value" line for field,
// returning value with at most one leading space stripped (the SSE spec's
// convention: "data: x" and "data:x" both carry value "x").
func sseField(line, field string) (string, bool) {
	prefix := field + ":"
	if !strings.HasPrefix(line, prefix) {
		return "", false
	}
	v := line[len(prefix):]
	v = strings.TrimPrefix(v, " ")
	return v, true
}

// openSSE opens an SSE stream at method+path and returns it, or an error
// (including a non-2xx response, whose body is read and reported via
// StatusError rather than left to a confusing Next() parse failure). Not
// retried: see the package doc comment on why.
func (c *Client) openSSE(ctx context.Context, method, path string, body io.Reader) (*EventStream, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	c.authorize(req)

	// c.sse, not c.http: a stream must outlive the request timeout. See
	// Client.sse's field comment.
	resp, err := c.sse.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		return nil, &StatusError{StatusCode: resp.StatusCode, Body: string(b)}
	}
	return &EventStream{body: resp.Body, scanner: bufio.NewScanner(resp.Body), header: resp.Header}, nil
}
