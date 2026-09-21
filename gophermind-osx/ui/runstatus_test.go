package ui

import (
	"testing"
	"time"
)

func at(base time.Time, d time.Duration) func() time.Time {
	return func() time.Time { return base.Add(d) }
}

func TestRunStatusIdleBeforeAnythingStarts(t *testing.T) {
	s := NewRunStatus()
	if got := s.Line(); got != "Idle" {
		t.Errorf("Line() = %q, want Idle", got)
	}
}

func TestRunStatusNamesTheStepAndCountsIt(t *testing.T) {
	base := time.Now()
	s := NewRunStatus()
	s.now = at(base, 0)
	s.Begin("breakdown")

	s.now = at(base, 2*time.Second)
	s.StepStarted("read_file")
	line := s.Line()
	if !contains(line, "step 1") {
		t.Errorf("Line() = %q, want it to count the step", line)
	}
	if !contains(line, "read_file") {
		t.Errorf("Line() = %q, want it to name the step", line)
	}

	s.now = at(base, 5*time.Second)
	s.StepFinished()
	s.StepStarted("run_shell")
	if line := s.Line(); !contains(line, "step 2") || !contains(line, "run_shell") {
		t.Errorf("Line() = %q, want step 2 named run_shell", line)
	}
}

func TestRunStatusReportsTheCapSoTheNumbersMeanSomething(t *testing.T) {
	s := NewRunStatus()
	s.Begin("breakdown")
	s.StepStarted("read_file")
	if line := s.Line(); !contains(line, "/12") {
		t.Errorf("Line() = %q, want the per-turn cap shown", line)
	}
}

func TestRunStatusReportsElapsedAndThroughput(t *testing.T) {
	base := time.Now()
	s := NewRunStatus()
	s.now = at(base, 0)
	s.Begin("breakdown")

	s.now = at(base, 10*time.Second)
	s.Usage(400, 200) // prompt, completion
	line := s.Line()
	if !contains(line, "10s") {
		t.Errorf("Line() = %q, want elapsed time", line)
	}
	if !contains(line, "600 tok") {
		t.Errorf("Line() = %q, want total tokens", line)
	}
	// 200 completion tokens over 10s = 20/s. Throughput is quoted on output
	// tokens, which is what a reader watching it appear actually sees.
	if !contains(line, "20.0 tok/s") {
		t.Errorf("Line() = %q, want output throughput", line)
	}
}

func TestRunStatusEndsClean(t *testing.T) {
	base := time.Now()
	s := NewRunStatus()
	s.now = at(base, 0)
	s.Begin("breakdown")
	s.StepStarted("read_file")
	s.now = at(base, 3*time.Second)
	s.Usage(100, 50)
	s.End()

	line := s.Line()
	if !contains(line, "done") {
		t.Errorf("Line() = %q, want a finished state", line)
	}
	// The totals survive the end of the run: the summary is the useful part.
	if !contains(line, "1 step") || !contains(line, "150 tok") {
		t.Errorf("Line() = %q, want the run's totals kept", line)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		(func() bool {
			for i := 0; i+len(needle) <= len(haystack); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		})()
}
