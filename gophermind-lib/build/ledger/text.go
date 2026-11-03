package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gophermind/gophermind-lib/build/runfs"
	"gophermind/gophermind-lib/safety"
)

// TextMessage is one prompt message kept for a verbose run.
type TextMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Text is the prompt and reply of one call, keyed by the call's ledger id.
type Text struct {
	ID       int64         `json:"id"`
	Stage    string        `json:"stage,omitempty"`
	NodeID   string        `json:"node_id,omitempty"`
	Provider string        `json:"provider,omitempty"`
	Model    string        `json:"model,omitempty"`
	Messages []TextMessage `json:"messages"`
	Response string        `json:"response"`
}

// TextRecorder is implemented by a ledger that can keep call text. It is
// optional: a ledger without it keeps only sizes and hashes.
type TextRecorder interface {
	RecordText(ctx context.Context, runID string, t Text) error
}

var _ TextRecorder = (*FS)(nil)

var textScanner = safety.NewSecretScanner()

func (f *FS) llmDir(runID string) (state, dir string, err error) {
	state, err = f.stateDir(runID)
	if err != nil {
		return "", "", err
	}
	return state, filepath.Join(state, "llm"), nil
}

// RecordText writes _state/llm/<id>.json only when the run folder holds the
// _state/verbose marker; otherwise it writes nothing. Secrets and email
// addresses are redacted from every message and from the reply first.
func (f *FS) RecordText(ctx context.Context, runID string, t Text) error {
	if t.ID <= 0 {
		return errors.New("ledger: text needs a positive call id")
	}
	state, dir, err := f.llmDir(runID)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(state, "verbose")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("ledger: %w", err)
	}
	out := t
	out.Messages = make([]TextMessage, len(t.Messages))
	for i, m := range t.Messages {
		out.Messages[i] = TextMessage{Role: m.Role, Content: textScanner.Redact(m.Content)}
	}
	out.Response = textScanner.Redact(t.Response)
	b, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("ledger: %w", err)
	}
	if err := runfs.WriteFileAtomic(filepath.Join(dir, strconv.FormatInt(t.ID, 10)+".json"), b); err != nil {
		return fmt.Errorf("ledger: %w", err)
	}
	return nil
}

// ListText returns the ids that have kept text, ascending.
func (f *FS) ListText(runID string) ([]int64, error) {
	_, dir, err := f.llmDir(runID)
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: %w", err)
	}
	var ids []int64
	for _, e := range ents {
		name, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		if id, err := strconv.ParseInt(name, 10, 64); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

// ReadText returns the text kept for call id. The path is built from the
// formatted integer only.
func (f *FS) ReadText(runID string, id int64) (Text, error) {
	if id <= 0 {
		return Text{}, errors.New("ledger: text id must be a positive integer")
	}
	_, dir, err := f.llmDir(runID)
	if err != nil {
		return Text{}, err
	}
	b, err := os.ReadFile(filepath.Join(dir, strconv.FormatInt(id, 10)+".json"))
	if err != nil {
		return Text{}, fmt.Errorf("ledger: %w", err)
	}
	var t Text
	if err := json.Unmarshal(b, &t); err != nil {
		return Text{}, errors.New("ledger: kept text is not JSON")
	}
	return t, nil
}
