package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"gophermind/gophermind-lib/runview"
	"gophermind/gophermind-lib/tools"
)

// The tools here change what runs on the server. All four are gated (see
// safety.Gated), so the agent loop asks John before any of them runs.

// writeOutcome turns the helpers' typed errors into one plain sentence the
// model can relay. nil stays nil.
func writeOutcome(err error) string {
	var busy *briefBusyError
	var invalid *briefInvalidError
	var notWaiting *briefNotWaitingError
	switch {
	case errors.As(err, &busy) && errors.Is(err, errResumeFailed):
		return "the answer is saved, and the run will resume when the active run " + busy.activeID + " ends."
	case errors.As(err, &busy):
		return "another run is already active (" + busy.activeID + "); nothing was started."
	case errors.As(err, &invalid):
		msg := invalid.msg
		if probs, ok := invalid.extra["problems"]; ok {
			if b, jerr := json.Marshal(probs); jerr == nil {
				msg += " " + string(b)
			}
		}
		return msg + "; nothing was started."
	case errors.Is(err, errBriefExists): // can't happen: run ids are server-assigned
		return "a run with this brief's id already exists; nothing was started."
	case errors.Is(err, errNoSuchRun):
		return "no such run."
	case errors.Is(err, errNoStoredBrief):
		return "that run has no stored brief to resubmit."
	case errors.Is(err, errAlreadyAnswered):
		return "that question is already answered; nothing was changed."
	case errors.Is(err, errNoSuchQuestion):
		return "no open question with that id; nothing was changed."
	case errors.As(err, &notWaiting):
		return "that run is not waiting for answers (phase " + notWaiting.phase + "); nothing was changed."
	case errors.Is(err, errNotActive):
		return "that run is not active, so there is nothing to stop."
	case errors.Is(err, errCLITooOld):
		return "the installed gophermind CLI is too old for chat answers; reinstall it together with gophermind-server."
	case errors.Is(err, errResumeFailed):
		return "the answer is saved but the run could not be resumed."
	case errors.Is(err, os.ErrNotExist):
		return "that run is not waiting for answers; nothing was changed."
	}
	return "the request failed: " + err.Error()
}

func (c *chatTools) briefCreate() tools.Tool {
	return tools.Tool{
		Name:        "brief_create",
		Description: "Start a run from brief text. Needs John's approval. ask defaults to true, so open questions stop for John to answer. The Approve button is disabled for arguments over about 200000 characters, so a very large brief must be uploaded on the Briefs page instead.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string", "description": "the full brief markdown, front matter included"},
				"ask":  map[string]any{"type": "boolean", "description": "stop for John's answers to open questions (default true)"},
			},
			"required":             []string{"text"},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if err := checkArgKeys("brief_create", raw, "text", "ask"); err != nil {
				return "", err
			}
			var a struct {
				Text string `json:"text"`
				Ask  *bool  `json:"ask"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("brief_create: bad arguments: %w", err)
			}
			if strings.TrimSpace(a.Text) == "" {
				return "", fmt.Errorf("brief_create: text is required")
			}
			if len(a.Text) > maxBriefBytes {
				return "", fmt.Errorf("brief_create: brief is larger than %d bytes", maxBriefBytes)
			}
			ask := a.Ask == nil || *a.Ask
			rec, warnings, err := c.bm.createText([]byte(a.Text), ask)
			if err != nil {
				return c.clean(writeOutcome(err)), nil
			}
			out := "started run " + rec.ID + "."
			if len(warnings) > 0 {
				out += fmt.Sprintf(" The brief has %d lint warning(s), first: %s.", len(warnings), warnings[0].String())
			}
			return c.clean(out), nil
		},
	}
}

func (c *chatTools) briefAnswer() tools.Tool {
	return tools.Tool{
		Name:        "brief_answer",
		Description: "Answer one open question of a waiting run. Needs John's approval. Use only the answer John gave. A question whose answer block is already filled (including a pre-filled gate default) cannot be changed from chat.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":          map[string]any{"type": "string", "description": "the brief run id"},
				"question_id": map[string]any{"type": "string", "description": "the question id, as listed by questions_pending"},
				"text":        map[string]any{"type": "string", "description": "the answer"},
			},
			"required":             []string{"id", "question_id", "text"},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if err := checkArgKeys("brief_answer", raw, "id", "question_id", "text"); err != nil {
				return "", err
			}
			var a struct {
				ID         string `json:"id"`
				QuestionID string `json:"question_id"`
				Text       string `json:"text"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("brief_answer: bad arguments: %w", err)
			}
			if !validBriefID(a.ID) || !runview.ValidRunID(a.ID) {
				return "", fmt.Errorf("brief_answer: %q is not a valid brief id", a.ID)
			}
			if !questionIDRE.MatchString(a.QuestionID) {
				return "", fmt.Errorf("brief_answer: %q is not a valid question id", a.QuestionID)
			}
			text := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(a.Text, "\r\n", "\n"), "\r", "\n"))
			switch {
			case text == "":
				return "", fmt.Errorf("brief_answer: the answer is empty")
			case len(text) > maxAnswerBytes:
				return "", fmt.Errorf("brief_answer: the answer is larger than 8 KiB")
			case strings.ContainsRune(text, 0) || hasFenceLine(text):
				return "", fmt.Errorf("brief_answer: the answer must not hold a line of three backticks")
			}
			remaining, err := c.bm.answerTextOpt(a.ID, a.QuestionID, text, true)
			if err != nil {
				return c.clean(writeOutcome(err)), nil
			}
			if remaining > 0 {
				return fmt.Sprintf("saved the answer; %d question(s) still open on %s.", remaining, a.ID), nil
			}
			return "saved the answer; every question on " + a.ID + " is answered and the run is resuming.", nil
		},
	}
}

func (c *chatTools) briefStop() tools.Tool {
	return tools.Tool{
		Name:        "brief_stop",
		Description: "Stop the active run. Needs John's approval.",
		Schema:      idSchema(),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			id, err := idArg("brief_stop", raw)
			if err != nil {
				return "", err
			}
			if err := c.bm.stop(id); err != nil {
				return c.clean(writeOutcome(err)), nil
			}
			return c.clean("stop requested for run " + id + "; it ends shortly."), nil
		},
	}
}

func (c *chatTools) briefResubmit() tools.Tool {
	return tools.Tool{
		Name:        "brief_resubmit",
		Description: "Start the stored brief of an earlier run again under a new id. Needs John's approval.",
		Schema:      idSchema(),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			id, err := idArg("brief_resubmit", raw)
			if err != nil {
				return "", err
			}
			rec, err := c.bm.resubmitID(id)
			if err != nil {
				return c.clean(writeOutcome(err)), nil
			}
			return c.clean("started run " + rec.ID + " from the brief of " + id + "."), nil
		},
	}
}

// checkArgKeys rejects arguments holding a key the schema does not name, or two
// keys that differ only by case: Go's JSON decoding would match both to one
// field and keep the last, so the approval card and the tool could disagree.
func checkArgKeys(tool string, raw json.RawMessage, allowed ...string) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("%s: bad arguments: %w", tool, err)
	}
	seen := map[string]bool{}
	for k := range m {
		lk := strings.ToLower(k)
		if seen[lk] {
			return fmt.Errorf("%s: the arguments are ambiguous: two keys differ only by letter case; nothing was done", tool)
		}
		seen[lk] = true
		ok := false
		for _, a := range allowed {
			ok = ok || k == a
		}
		if !ok {
			return fmt.Errorf("%s: the arguments are ambiguous: unknown key %q; nothing was done", tool, k)
		}
	}
	return nil
}

// idArg decodes {"id": ...} and checks it is a usable run id.
func idArg(tool string, raw json.RawMessage) (string, error) {
	if err := checkArgKeys(tool, raw, "id"); err != nil {
		return "", err
	}
	var a struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return "", fmt.Errorf("%s: bad arguments: %w", tool, err)
	}
	if !validBriefID(a.ID) || !runview.ValidRunID(a.ID) {
		return "", fmt.Errorf("%s: %q is not a valid brief id", tool, a.ID)
	}
	return a.ID, nil
}
