package stream

import (
	"context"
	"strings"
	"testing"
)

func TestParseControl(t *testing.T) {
	act, val, ok := parseControl([]byte(`{"type":"control","action":"set-model","model":"fast-1"}`))
	if !ok || act != "set-model" || val != "fast-1" {
		t.Errorf("set-model parse = %q,%q,%v", act, val, ok)
	}
	act, _, ok = parseControl([]byte(`{"type":"control","action":"interrupt"}`))
	if !ok || act != "interrupt" {
		t.Errorf("interrupt parse = %q,%v", act, ok)
	}
	// a normal user message is not a control message
	if _, _, ok := parseControl([]byte(`{"type":"user","message":{"role":"user","content":"hi"}}`)); ok {
		t.Error("user message misdetected as control")
	}
}

func TestRunHandlesControlLines(t *testing.T) {
	var b strings.Builder
	enc := NewEncoder(&b, "s")
	sess := &fakeSession{enc: enc, answer: "ok"}

	var controls []string
	in := `{"type":"control","action":"set-model","model":"fast-1"}` + "\n" +
		`{"type":"user","message":{"role":"user","content":"do it"}}` + "\n" +
		`{"type":"control","action":"interrupt"}` + "\n"

	err := Run(context.Background(), enc, sess, Options{
		InputFormat: "stream-json",
		In:          strings.NewReader(in),
		OnControl: func(action, value string) error {
			controls = append(controls, action+":"+value)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The user turn ran; control lines did not.
	if len(sess.inputs) != 1 || sess.inputs[0] != "do it" {
		t.Errorf("user inputs = %v, want [do it]", sess.inputs)
	}
	if len(controls) != 2 || controls[0] != "set-model:fast-1" || controls[1] != "interrupt:" {
		t.Errorf("controls = %v", controls)
	}
}
