package ui

import "testing"

// TestShouldSend_CmdEnterSends covers "Cmd+Enter sends" as a decision.
func TestShouldSend_CmdEnterSends(t *testing.T) {
	for _, key := range []rune{'\r', '\n'} {
		ev := KeyEvent{Key: key, Modifiers: ModifierCommand}
		if !ShouldSend(ev) {
			t.Errorf("ShouldSend(%q + Cmd) = false, want true", key)
		}
		if ShouldInsertNewline(ev) {
			t.Errorf("ShouldInsertNewline(%q + Cmd) = true, want false", key)
		}
	}
}

// TestShouldSend_PlainEnterInsertsNewline covers "Enter adds newline".
func TestShouldSend_PlainEnterInsertsNewline(t *testing.T) {
	for _, key := range []rune{'\r', '\n'} {
		ev := KeyEvent{Key: key}
		if ShouldSend(ev) {
			t.Errorf("ShouldSend(%q, no modifier) = true, want false", key)
		}
		if !ShouldInsertNewline(ev) {
			t.Errorf("ShouldInsertNewline(%q, no modifier) = false, want true", key)
		}
	}
}

func TestShouldSend_OtherModifiersDoNotSend(t *testing.T) {
	for _, mod := range []Modifier{ModifierAlt, ModifierControl, ModifierShift, ModifierAlt | ModifierShift} {
		ev := KeyEvent{Key: '\r', Modifiers: mod}
		if ShouldSend(ev) {
			t.Errorf("ShouldSend(Enter + %v) = true, want false (only Cmd should send)", mod)
		}
	}
}

func TestShouldSend_NonEnterKeyNeverTriggers(t *testing.T) {
	ev := KeyEvent{Key: 'a', Modifiers: ModifierCommand}
	if ShouldSend(ev) || ShouldInsertNewline(ev) {
		t.Errorf("a non-Enter key triggered send or newline: ShouldSend=%v ShouldInsertNewline=%v", ShouldSend(ev), ShouldInsertNewline(ev))
	}
}

func TestModifier_Has(t *testing.T) {
	combo := ModifierCommand | ModifierShift
	if !combo.Has(ModifierCommand) || !combo.Has(ModifierShift) {
		t.Error("combo should have both Command and Shift")
	}
	if combo.Has(ModifierAlt) {
		t.Error("combo should not have Alt")
	}
}
