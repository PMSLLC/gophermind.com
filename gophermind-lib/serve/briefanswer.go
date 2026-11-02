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
	ID     string
	Text   string
	Filled bool // the answer block holds text (an answer or a default)
	Answer string
	// Options and Recommended come from the "Options:" and "Recommended:"
	// lines the file gate writes; empty when the question has none.
	Options     []string
	Recommended string
	startIdx    int // line index of the "```answer <id>" fence
	endIdx      int // line index of the closing fence
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
		if t, ok := strings.CutPrefix(ln, "Options:"); ok && cur.startIdx < 0 {
			for _, o := range strings.Split(t, "|") {
				if o = strings.TrimSpace(o); o != "" {
					cur.Options = append(cur.Options, o)
				}
			}
		} else if t, ok := strings.CutPrefix(ln, "Recommended:"); ok && cur.startIdx < 0 {
			cur.Recommended = strings.TrimSpace(t)
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

// errAlreadyAnswered is a refused write: the question already has an answer.
var errAlreadyAnswered = errors.New("question is already answered")

// ErrNoSuchQuestion is returned by FillGateAnswer for a question id the run's
// QUESTIONS.md does not hold.
var ErrNoSuchQuestion = errNoSuchQuestion

// FillGateAnswer is the answer write as a command: `gophermind brief
// fill-answer` runs it as the user that owns the run folder, which the web
// server may not be allowed to write.
func FillGateAnswer(root, id, qid, text string) (remaining int, err error) {
	return FillGateAnswerOpt(root, id, qid, text, false)
}

// errCLITooOld is a launcher that rejected --no-overwrite: an installed
// gophermind older than this server.
var errCLITooOld = errors.New("the installed gophermind CLI does not know --no-overwrite")

// ErrAlreadyAnswered is returned by FillGateAnswerOpt with noOverwrite for a
// question whose answer block is already filled.
var ErrAlreadyAnswered = errAlreadyAnswered

// FillGateAnswerOpt is FillGateAnswer; noOverwrite refuses a question that is
// already answered, writing nothing.
func FillGateAnswerOpt(root, id, qid, text string, noOverwrite bool) (remaining int, err error) {
	if !runview.ValidRunID(id) || !questionIDRE.MatchString(qid) {
		return 0, errors.New("invalid run or question id")
	}
	return writeGateAnswer(root, id, qid, text, noOverwrite)
}

// fillAnswerExit codes of `brief fill-answer`.
const (
	fillExitNoQuestion = 3
	fillExitNoRun      = 4
	fillExitAnswered   = 5
)

// writeAnswerAs writes the answer through the launcher command, for a run
// folder the server user cannot write: `brief fill-answer <id> <qid> --repo
// <root>` with the text on stdin, answering "remaining N" on stdout.
func (m *briefManager) writeAnswerAs(id, qid, text string, noOverwrite bool) (int, error) {
	args := []string{"brief", "fill-answer", id, qid, "--repo", m.repoFor(id)}
	if noOverwrite {
		args = append(args, "--no-overwrite")
	}
	cmd := m.cmd(args)
	cmd.Stdin = strings.NewReader(text)
	var out bytes.Buffer
	cmd.Stdout = &out
	var errOut bytes.Buffer
	if cmd.Stderr == nil {
		cmd.Stderr = &errOut
	}
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
			case fillExitAnswered:
				return 0, errAlreadyAnswered
			case 1:
				if noOverwrite && strings.Contains(errOut.String(), "usage:") {
					return 0, errCLITooOld
				}
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
func writeGateAnswer(root, id, qid, text string, noOverwrite bool) (remaining int, err error) {
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
	if noOverwrite && q.Filled {
		return 0, errAlreadyAnswered
	}
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
	id = m.resolveID(id)
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

	remaining, err := m.answerText(id, req.QuestionID, text)
	var notWaiting *briefNotWaitingError
	var busy *briefBusyError
	switch {
	case errors.Is(err, errNoSuchRun):
		briefError(w, http.StatusNotFound, "no such run", nil)
	case errors.As(err, &notWaiting):
		briefError(w, http.StatusConflict, "run is not waiting for answers", map[string]any{"phase": notWaiting.phase})
	case errors.Is(err, errNoSuchQuestion):
		briefError(w, http.StatusNotFound, "no such open question", nil)
	case errors.Is(err, errResumeFailed) && errors.As(err, &busy):
		// Another run holds the slot: the answers are saved, and the run
		// resumes by itself when that run ends (resumeAnswered).
		briefJSON(w, http.StatusOK, map[string]any{"run_id": id, "saved": true, "resumed": false, "remaining": 0,
			"queued": true, "active_run_id": busy.activeID})
	case errors.Is(err, errResumeFailed):
		briefError(w, http.StatusInternalServerError, "cannot resume the run", nil)
	case errors.Is(err, os.ErrNotExist):
		briefError(w, http.StatusConflict, "run is not waiting for answers", nil)
	case err != nil:
		briefError(w, http.StatusInternalServerError, "cannot save the answer", nil)
	case remaining > 0:
		briefJSON(w, http.StatusOK, map[string]any{"run_id": id, "saved": true, "resumed": false, "remaining": remaining})
	default:
		briefJSON(w, http.StatusAccepted, map[string]any{"run_id": id, "saved": true, "resumed": true, "remaining": 0})
	}
}

// briefNotWaitingError is an answer sent to a run that is not waiting.
type briefNotWaitingError struct{ phase string }

func (e *briefNotWaitingError) Error() string { return "run is not waiting for answers" }

// errResumeFailed wraps the error from resuming a run after its last answer.
// The answer itself was saved. A *briefBusyError inside means the run is
// queued behind the active one.
var errResumeFailed = errors.New("cannot resume the run")

type resumeError struct{ err error }

func (e *resumeError) Error() string   { return errResumeFailed.Error() + ": " + e.err.Error() }
func (e *resumeError) Unwrap() []error { return []error{errResumeFailed, e.err} }

// answerText fills the question's block in the run's QUESTIONS.md and, when
// no block is left empty, resumes planning. text must already be cleaned (see
// answerHandler). Errors: errNoSuchRun, *briefNotWaitingError,
// errNoSuchQuestion, os.ErrNotExist, or a resume failure (errResumeFailed,
// with a *briefBusyError inside when the run is queued; remaining is then 0).
func (m *briefManager) answerText(id, qid, text string) (remaining int, err error) {
	return m.answerTextOpt(id, qid, text, false)
}

// answerTextOpt is answerText; noOverwrite refuses (errAlreadyAnswered, nothing
// written) a question whose answer block is already filled. The check runs under
// answerMu on the data being written, for the direct write and the run-user one.
func (m *briefManager) answerTextOpt(id, qid, text string, noOverwrite bool) (remaining int, err error) {
	m.answerMu.Lock()
	defer m.answerMu.Unlock()
	rec, ok := m.readRecord(id)
	if !ok {
		return 0, errNoSuchRun
	}
	if rec.Phase != phaseWaiting {
		return 0, &briefNotWaitingError{rec.Phase}
	}
	remaining, err = writeGateAnswer(m.repoFor(id), id, qid, text, noOverwrite)
	if errors.Is(err, os.ErrPermission) {
		// The run folder belongs to the run user: let it write its own file.
		remaining, err = m.writeAnswerAs(id, qid, text, noOverwrite)
	}
	if err != nil {
		return 0, err
	}
	m.log.Info("brief answer saved", "run_id", id, "question_id", qid, "remaining", remaining)
	if remaining > 0 {
		return remaining, nil
	}
	if err := m.resume(rec); err != nil {
		return 0, &resumeError{err}
	}
	return 0, nil
}

// resumeAnswered resumes the oldest run that waits with every question
// answered, when the slot is free. It reports the run it resumed, "" when none.
func (m *briefManager) resumeAnswered() string {
	m.answerMu.Lock()
	defer m.answerMu.Unlock()
	ents, err := os.ReadDir(m.dir)
	if err != nil {
		return ""
	}
	for _, e := range ents {
		id := e.Name()
		if !e.IsDir() || !runview.ValidRunID(id) {
			continue
		}
		rec, ok := m.readRecord(id)
		if !ok || rec.Phase != phaseWaiting {
			continue
		}
		qs := readGateQuestions(m.repoFor(id), id)
		answered := len(qs) > 0
		for _, q := range qs {
			answered = answered && q.Filled
		}
		if !answered {
			continue
		}
		if m.resume(rec) != nil {
			return ""
		}
		return id
	}
	return ""
}

func hasFenceLine(s string) bool {
	for _, ln := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			return true
		}
	}
	return false
}
