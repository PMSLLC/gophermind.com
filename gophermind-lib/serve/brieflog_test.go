//go:build !windows

package serve

import (
	"net/http"
	"strings"
	"sync"
	"testing"
)

// lockedBuf is a log sink the run goroutine and the test can share.
type lockedBuf struct {
	mu sync.Mutex
	sb strings.Builder
}

func (b *lockedBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sb.Write(p)
}

func (b *lockedBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sb.String()
}

func TestBriefEventsAreLogged(t *testing.T) {
	e := newBriefEnv(t)
	id := "gm-2026-10-23-081"
	e.startAsking(id)
	secret := "hunter2-the-answer"
	w := e.post("/app/briefs/"+id+"/answer", answerBody("clarify-q1", secret), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("answer 1 = %d %s", w.Code, w.Body)
	}
	if w := e.post("/app/briefs/"+id+"/answer", answerBody("clarify-q2", secret), nil); w.Code != http.StatusAccepted {
		t.Fatalf("answer 2 = %d %s", w.Code, w.Body)
	}
	e.waitPhase(id, phaseDone, phaseFailed)
	e.waitNotActive(id)
	log := e.logs.String()
	for _, want := range []string{
		"brief upload accepted", "run_id=" + id, "ask=true",
		"brief process started", "step=plan", "step=resume", "step=run",
		"brief process ended", "exit_code=3", "exit_code=0", "duration_ms=",
		"brief answer saved", "question_id=clarify-q1", "question_id=clarify-q2",
		"brief resume started",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, secret) {
		t.Error("an answer's text reached the log")
	}
}

func TestBriefStopIsLogged(t *testing.T) {
	e := newBriefEnv(t)
	id := "gm-2026-10-23-082"
	e.script = func(args []string) string { return "sleep 30" }
	if w := e.post("/app/briefs", e.briefText(id), nil); w.Code != http.StatusAccepted {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}
	e.waitPhase(id, phasePlanning)
	e.stopAndDrain(id)
	e.waitPhase(id, phaseStopped)
	log := e.logs.String()
	if !strings.Contains(log, "brief stop requested") || !strings.Contains(log, "run_id="+id) {
		t.Errorf("log lacks the stop line:\n%s", log)
	}
}
