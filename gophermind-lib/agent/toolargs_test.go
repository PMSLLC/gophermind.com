package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

// decodeCommand runs raw through normalizeToolArgs and pulls out the "command"
// field, which is what run_shell would receive.
func decodeCommand(t *testing.T, raw string) string {
	t.Helper()
	args, err := normalizeToolArgs("run_shell", raw)
	if err != nil {
		t.Fatalf("normalizeToolArgs(%q): %v", raw, err)
	}
	var a struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		t.Fatalf("repaired args %s do not decode: %v", args, err)
	}
	return a.Command
}

// TestUnderEscapedBackslashesAreRepaired is the reported failure: a model
// quoting a regex into a shell command emits `\[` where JSON requires `\\[`.
// The backslash the model wrote must survive into the command verbatim.
func TestUnderEscapedBackslashesAreRepaired(t *testing.T) {
	raw := `{"command":"grep -rn 'preg_replace.*\[^0-9\].*\$phone' --include='*.php' ."}`
	got := decodeCommand(t, raw)
	want := `grep -rn 'preg_replace.*\[^0-9\].*\$phone' --include='*.php' .`
	if got != want {
		t.Errorf("command = %q, want %q", got, want)
	}
}

// TestValidEscapesAreUntouched: a correctly-escaped payload must round-trip
// byte-for-byte, so the repair can never corrupt a well-formed call.
func TestValidEscapesAreUntouched(t *testing.T) {
	cases := []struct{ raw, want string }{
		{`{"command":"grep '\\[0-9\\]' ."}`, `grep '\[0-9\]' .`}, // already correct
		{`{"command":"echo \"hi\""}`, `echo "hi"`},               // escaped quotes
		{`{"command":"printf 'a\nb'"}`, "printf 'a\nb'"},         // real \n escape
		{`{"command":"echo é"}`, "echo é"},                       // unicode escape
		{`{"command":"ls /a/b"}`, "ls /a/b"},                     // nothing to do
	}
	for _, c := range cases {
		if got := decodeCommand(t, c.raw); got != c.want {
			t.Errorf("raw %s -> %q, want %q", c.raw, got, c.want)
		}
	}
}

// TestRawControlCharactersAreEscaped covers the other way models break a
// quoted command: a literal newline inside the JSON string.
func TestRawControlCharactersAreEscaped(t *testing.T) {
	raw := "{\"command\":\"grep foo\n  bar\"}"
	if got, want := decodeCommand(t, raw), "grep foo\n  bar"; got != want {
		t.Errorf("command = %q, want %q", got, want)
	}
}

// TestEmptyArgsBecomeEmptyObject keeps no-argument tools callable: a model that
// sends nothing at all must not be an error.
func TestEmptyArgsBecomeEmptyObject(t *testing.T) {
	for _, raw := range []string{"", "   ", "\n"} {
		args, err := normalizeToolArgs("git_info", raw)
		if err != nil {
			t.Fatalf("normalizeToolArgs(%q): %v", raw, err)
		}
		if string(args) != "{}" {
			t.Errorf("normalizeToolArgs(%q) = %s, want {}", raw, args)
		}
	}
}

// TestTruncatedArgsAreNotSilentlyCompleted is the safety boundary: closing an
// unterminated string would hand run_shell a command the model never finished
// writing. It must fail, and the error must tell the model what to do instead.
func TestTruncatedArgsAreNotSilentlyCompleted(t *testing.T) {
	raw := `{"command":"grep -rn 'preg_replace.*\\[^0-9\\]' --include='*.php' . | grep -v vendor`
	args, err := normalizeToolArgs("run_shell", raw)
	if err == nil {
		t.Fatalf("truncated arguments were accepted as %s", args)
	}
	if !strings.Contains(err.Error(), "search") {
		t.Errorf("error does not point the model at a working alternative: %v", err)
	}
	if !strings.Contains(err.Error(), "run_shell") {
		t.Errorf("error does not name the tool that failed: %v", err)
	}
}

// TestRepairIsDiscardedWhenItDoesNotHelp: reescapeJSONStrings must never be
// applied blind — a document that is still invalid after rewriting is reported
// as an error, not passed to the tool.
func TestRepairIsDiscardedWhenItDoesNotHelp(t *testing.T) {
	if _, err := normalizeToolArgs("run_shell", `{"command": }`); err == nil {
		t.Error("structurally broken arguments were accepted")
	}
}

// TestMultiByteRunesSurviveRepair guards the byte-walking loop against
// splitting a UTF-8 sequence while fixing an escape elsewhere in the string.
func TestMultiByteRunesSurviveRepair(t *testing.T) {
	raw := `{"command":"grep 'café.*\[0-9\]' ."}`
	if got, want := decodeCommand(t, raw), `grep 'café.*\[0-9\]' .`; got != want {
		t.Errorf("command = %q, want %q", got, want)
	}
}
