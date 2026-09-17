package tui

import (
	"strings"
	"testing"

	"gophermind/gophermind-lib/setup"
)

// TestHandleConfigDoneAppliesSpeedModelLive is the deferred follow-up from
// feat/project-execute (#6): m.speedModel was frozen at startup, so changing
// it via /config had no effect until a full restart. handleConfigDone must
// now apply a changed SpeedModel to the live model, the same way it already
// does for Model/ApprovalMode/MaxIter.
func TestHandleConfigDoneAppliesSpeedModelLive(t *testing.T) {
	m := testModel(t)
	m.agent = stubAgent(t)
	m.speedModel = "old-speed"

	m.handleConfigDone(configDoneMsg{result: setup.Result{
		ApprovalMode: m.mode,
		SpeedModel:   "new-speed",
	}})

	if m.speedModel != "new-speed" {
		t.Errorf("speedModel = %q, want new-speed", m.speedModel)
	}
	if !strings.Contains(m.content, "speed model") {
		t.Errorf("changed-values summary omits speed model: %q", m.content)
	}
}

// TestHandleConfigDoneBlankSpeedModelKeepsCurrent mirrors the existing
// Model field's convention: a blank wizard answer means "unchanged", not
// "clear it" -- config.go's own doc comment on Pairs draws this same
// distinction for every optional field except the paths.
func TestHandleConfigDoneBlankSpeedModelKeepsCurrent(t *testing.T) {
	m := testModel(t)
	m.agent = stubAgent(t)
	m.speedModel = "keep-me"

	m.handleConfigDone(configDoneMsg{result: setup.Result{
		ApprovalMode: m.mode,
		SpeedModel:   "",
	}})

	if m.speedModel != "keep-me" {
		t.Errorf("speedModel = %q, want unchanged keep-me", m.speedModel)
	}
	if strings.Contains(m.content, "speed model") {
		t.Errorf("no-op speed model should not be reported as a change: %q", m.content)
	}
}

// TestConfigDefaultsForIncludesLiveSpeedModel: the wizard's prefilled
// defaults must show the live speedModel, not always start blank, or every
// /config run would look like it is clearing a previously set speed model.
func TestConfigDefaultsForIncludesLiveSpeedModel(t *testing.T) {
	m := testModel(t)
	m.agent = stubAgent(t)
	m.speedModel = "prefilled-speed"

	got := configDefaultsFor(m)
	if got.SpeedModel != "prefilled-speed" {
		t.Errorf("defaults.SpeedModel = %q, want prefilled-speed", got.SpeedModel)
	}
}
