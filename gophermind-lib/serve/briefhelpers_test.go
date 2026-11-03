package serve

import (
	"errors"
	"testing"
	"time"
)

func TestAnswerTextUnknownRun(t *testing.T) {
	m := &briefManager{}
	m.dir = t.TempDir()
	if _, err := m.answerText("gm-2026-01-01-001", "q1", "yes"); !errors.Is(err, errNoSuchRun) {
		t.Fatalf("err = %v, want errNoSuchRun", err)
	}
}

func TestResubmitIDUnknown(t *testing.T) {
	m := &briefManager{}
	m.dir = t.TempDir()
	if _, err := m.resubmitID("bad id"); !errors.Is(err, errNoSuchRun) {
		t.Fatalf("err = %v, want errNoSuchRun", err)
	}
}

func TestCreateTextInvalid(t *testing.T) {
	m := &briefManager{now: time.Now}
	m.dir = t.TempDir()
	_, _, err := m.createText([]byte("not a brief"), false, false)
	var inv *briefInvalidError
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want *briefInvalidError", err)
	}
}
