package serve

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"gophermind/gophermind-lib/runview"
)

const (
	maxAnswerBytes     = 8 << 10
	maxAnswerBodyBytes = 16 << 10
	questionsFile      = "QUESTIONS.md"
)

// questionIDRE is what a question id may look like. It is the file gate's own
// safe set, so an id is never a path or a fence.
var questionIDRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// gateQuestion is one question of the file gate's QUESTIONS.md.
type gateQuestion struct {
	ID       string
	Text     string
	Filled   bool // the answer block holds text (an answer or a default)
	Answer   string
	startIdx int // line index of the "```answer <id>" fence
	endIdx   int // line index of the closing fence
}

var (
	gateHeadRE  = regexp.MustCompile(`^## Question \d+ \(id: ([A-Za-z0-9._-]+)\)\s*$`)
	gateFenceRE = regexp.MustCompile("^```answer ([A-Za-z0-9._-]+)\\s*$")
)

// parseGateQuestions reads QUESTIONS.md as the file gate wrote it.
func parseGateQuestions(src string) (lines []string, qs []gateQuestion) {
	lines = strings.Split(src, "\n")
	var cur *gateQuestion
	var text []string
	inText := false
	flush := func() {
		if cur != nil {
			cur.Text = strings.TrimSpace(strings.Join(text, "\n"))
		}
	}
	for i := 0; i < len(lines); i++ {
		ln := lines[i]
		if m := gateHeadRE.FindStringSubmatch(ln); m != nil {
			qs = append(qs, gateQuestion{ID: m[1], startIdx: -1, endIdx: -1})
			cur = &qs[len(qs)-1]
			text, inText = nil, true
			continue
		}
		if cur == nil {
			continue
		}
		if m := gateFenceRE.FindStringSubmatch(ln); m != nil && m[1] == cur.ID {
			flush()
			inText = false
			cur.startIdx = i
			j := i + 1
			for j < len(lines) && strings.TrimRight(lines[j], " \t") != "```" {
				if strings.TrimSpace(lines[j]) != "" {
					cur.Filled = true
				}
				j++
			}
			cur.Answer = strings.TrimSpace(strings.Join(lines[i+1:j], "\n"))
			cur.endIdx = j
			i = j
			continue
		}
		if inText {
			if strings.HasPrefix(ln, "Why it matters:") || strings.HasPrefix(ln, "Options:") || strings.HasPrefix(ln, "Recommended:") {
				flush()
				inText = false
				continue
			}
			text = append(text, ln)
		}
	}
	// cur points into qs, which append may have moved; texts were flushed at
	// each fence, so only a question with no fence is left unset.
	out := qs[:0]
	for _, q := range qs {
		if q.startIdx >= 0 && q.endIdx < len(lines) {
			out = append(out, q)
		}
	}
	return lines, out
}

// readGateQuestions reads the run folder's QUESTIONS.md; nil when absent.
func readGateQuestions(root, id string) []gateQuestion {
	b, err := readSmallFile(filepath.Join(root, ".gophermind", id, questionsFile), 1<<20)
	if err != nil {
		return nil
	}
	_, qs := parseGateQuestions(string(b))
	return qs
}

var errNoSuchQuestion = errors.New("no such question")

// ErrNoSuchQuestion is returned by FillGateAnswer for a question id the run's
// QUESTIONS.md does not hold.
var ErrNoSuchQuestion = errNoSuchQuestion

// FillGateAnswer is the answer write as a command: `gophermind brief
// fill-answer` runs it as the user that owns the run folder, which the web
// server may not be allowed to write.
func FillGateAnswer(root, id, qid, text string) (remaining int, err error) {
	if !runview.ValidRunID(id) || !questionIDRE.MatchString(qid) {
		return 0, errors.New("invalid run or question id")
	}
	return writeGateAnswer(root, id, qid, text)
}

// fillAnswerExit codes of `brief fill-answer`.
const (
	fillExitNoQuestion = 3
	fillExitNoRun      = 4
)

// writeAnswerAs writes the answer through the launcher command, for a run
// folder the server user cannot write: `brief fill-answer <id> <qid> --repo
// <root>` with the text on stdin, answering "remaining N" on stdout.
func (m *briefManager) writeAnswerAs(id, qid, text string) (int, error) {
	cmd := m.cmd([]string{"brief", "fill-answer", id, qid, "--repo", m.root})
	cmd.Stdin = strings.NewReader(text)
	var out bytes.Buffer
	cmd.Stdout = &out
	if cmd.Env == nil {
		cmd.Env = childEnv()
	}
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			switch ee.ExitCode() {
			case fillExitNoQuestion:
				return 0, errNoSuchQuestion
			case fillExitNoRun:
				return 0, os.ErrNotExist
			}
		}
		return 0, err
	}
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(out.String()), "remaining %d", &n); err != nil {
		return 0, errors.New("unexpected fill-answer output")
	}
	return n, nil
}

// writeGateAnswer puts text in the answer block of question qid and reports
// how many blocks are still empty.
func writeGateAnswer(root, id, qid, text string) (remaining int, err error) {
	path := filepath.Join(root, ".gophermind", id, questionsFile)
	b, err := readSmallFile(path, 1<<20)
	if err != nil {
		return 0, err
	}
	lines, qs := parseGateQuestions(string(b))
	idx := -1
	for i, q := range qs {
		if q.ID == qid {
			idx = i
		}
	}
	if idx < 0 {
		return 0, errNoSuchQuestion
	}
	q := qs[idx]
	out := append([]string(nil), lines[:q.startIdx+1]...)
	out = append(out, strings.Split(text, "\n")...)
	out = append(out, lines[q.endIdx:]...)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(out, "\n")), 0o600); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return 0, err
	}
	_, qs = parseGateQuestions(strings.Join(out, "\n"))
	for _, x := range qs {
		if !x.Filled {
			remaining++
		}
	}
	return remaining, nil
}

// answerHandler handles POST /app/briefs/{id}/answer {question_id, answer}:
// it fills the question's block in the run's QUESTIONS.md and, when no block
// is left empty, resumes planning through the launcher.
func (m *briefManager) answerHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !runview.ValidRunID(id) {
		briefError(w, http.StatusNotFound, "no such run", nil)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAnswerBodyBytes))
	if err != nil {
		briefError(w, http.StatusRequestEntityTooLarge, "answer body is too large", nil)
		return
	}
	var req struct {
		QuestionID string `json:"question_id"`
		Answer     string `json:"answer"`
	}
	if json.Unmarshal(body, &req) != nil {
		briefError(w, http.StatusBadRequest, "body must be JSON {question_id, answer}", nil)
		return
	}
	if !questionIDRE.MatchString(req.QuestionID) {
		briefError(w, http.StatusBadRequest, "invalid question id", nil)
		return
	}
	text := strings.ReplaceAll(strings.ReplaceAll(req.Answer, "\r\n", "\n"), "\r", "\n")
	text = strings.TrimSpace(text)
	switch {
	case text == "":
		briefError(w, http.StatusBadRequest, "empty answer", nil)
		return
	case len(text) > maxAnswerBytes:
		briefError(w, http.StatusRequestEntityTooLarge, "answer is larger than 8 KiB", nil)
		return
	case strings.ContainsRune(text, 0) || hasFenceLine(text):
		briefError(w, http.StatusBadRequest, "answer must not hold a line of three backticks", nil)
		return
	}

	m.answerMu.Lock()
	defer m.answerMu.Unlock()
	rec, ok := m.readRecord(id)
	if !ok {
		briefError(w, http.StatusNotFound, "no such run", nil)
		return
	}
	if rec.Phase != phaseWaiting {
		briefError(w, http.StatusConflict, "run is not waiting for answers", map[string]any{"phase": rec.Phase})
		return
	}
	if active := m.activeID(); active != "" {
		briefError(w, http.StatusConflict, "a run is already active", map[string]any{"active_run_id": active})
		return
	}
	remaining, err := writeGateAnswer(m.root, id, req.QuestionID, text)
	if errors.Is(err, os.ErrPermission) {
		// The run folder belongs to the run user: let it write its own file.
		remaining, err = m.writeAnswerAs(id, req.QuestionID, text)
	}
	switch {
	case errors.Is(err, errNoSuchQuestion):
		briefError(w, http.StatusNotFound, "no such open question", nil)
		return
	case errors.Is(err, os.ErrNotExist):
		briefError(w, http.StatusConflict, "run is not waiting for answers", nil)
		return
	case err != nil:
		briefError(w, http.StatusInternalServerError, "cannot save the answer", nil)
		return
	}
	if remaining > 0 {
		briefJSON(w, http.StatusOK, map[string]any{"run_id": id, "saved": true, "resumed": false, "remaining": remaining})
		return
	}
	if err := m.resume(rec); err != nil {
		var busy *briefBusyError
		if errors.As(err, &busy) {
			briefError(w, http.StatusConflict, "a run is already active", map[string]any{"active_run_id": busy.activeID})
			return
		}
		briefError(w, http.StatusInternalServerError, "cannot resume the run", nil)
		return
	}
	briefJSON(w, http.StatusAccepted, map[string]any{"run_id": id, "saved": true, "resumed": true, "remaining": 0})
}

func hasFenceLine(s string) bool {
	for _, ln := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			return true
		}
	}
	return false
}
