package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"gophermind/internal/tools"
)

// ReplayEvent records a single event for deterministic replay.
type ReplayEvent struct {
	Type  string        `json:"type"`
	Name  string        `json:"name,omitempty"`
	Text  string        `json:"text,omitempty"`
	Usage UsageSnapshot `json:"usage,omitempty"`
}

// ReplayRecorder captures events for deterministic replay.
type ReplayRecorder struct {
	events []ReplayEvent
}

// NewReplayRecorder creates a new replay recorder.
func NewReplayRecorder() *ReplayRecorder {
	return &ReplayRecorder{}
}

// Record captures an event.
func (r *ReplayRecorder) Record(e Event) {
	r.events = append(r.events, ReplayEvent{
		Type:  e.Type,
		Name:  e.Name,
		Text:  e.Text,
		Usage: e.Usage,
	})
}

// Events returns the recorded events.
func (r *ReplayRecorder) Events() []ReplayEvent {
	return r.events
}

// ReplayWriter writes recorded events to a file for replay.
func (r *ReplayRecorder) Write(w io.Writer) error {
	enc := json.NewEncoder(w)
	for _, e := range r.events {
		if err := enc.Encode(e); err != nil {
			return fmt.Errorf("encode event: %w", err)
		}
	}
	return nil
}

// ReplayReader reads recorded events from a file.
func ReplayReader(r io.Reader) ([]ReplayEvent, error) {
	var events []ReplayEvent
	dec := json.NewDecoder(r)
	for {
		var e ReplayEvent
		err := dec.Decode(&e)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode event: %w", err)
		}
		events = append(events, e)
	}
	return events, nil
}

// ReplayTool returns a tool that writes the current replay events to a file.
func ReplayTool(rec *ReplayRecorder) tools.Tool {
	return tools.Tool{
		Name:        "_gophermind_replay",
		Description: "Write recorded replay events to a file for deterministic replay.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "File path to write replay events."},
			},
			"required": []string{"path"},
		},
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var args struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			f, err := os.Create(args.Path)
			if err != nil {
				return "", fmt.Errorf("create file: %w", err)
			}
			defer f.Close()
			if err := rec.Write(f); err != nil {
				return "", fmt.Errorf("write replay: %w", err)
			}
			return fmt.Sprintf("Wrote %d replay events to %s.", len(rec.Events()), args.Path), nil
		},
	}
}
