package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestSessionModeRoundTrip(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())

	if got := readSessionMode("s1"); got != "" {
		t.Fatalf("readSessionMode before write = %q, want empty", got)
	}
	if err := writeSessionMode("s1", "conversational"); err != nil {
		t.Fatalf("writeSessionMode: %v", err)
	}
	if got := readSessionMode("s1"); got != "conversational" {
		t.Fatalf("readSessionMode = %q, want %q", got, "conversational")
	}

	p, err := sessionModePath("s1")
	if err != nil {
		t.Fatalf("sessionModePath: %v", err)
	}
	if !strings.HasSuffix(p, "s1.mode") {
		t.Fatalf("sessionModePath = %q, want suffix s1.mode", p)
	}
}

func TestWriteSessionModeEmptyRemovesSidecar(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())

	if err := writeSessionMode("s2", "reviewer"); err != nil {
		t.Fatalf("writeSessionMode: %v", err)
	}
	p, err := sessionModePath("s2")
	if err != nil {
		t.Fatalf("sessionModePath: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("sidecar not written: %v", err)
	}

	if err := writeSessionMode("s2", ""); err != nil {
		t.Fatalf("writeSessionMode(empty): %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("sidecar still exists after empty write: err=%v", err)
	}
	if got := readSessionMode("s2"); got != "" {
		t.Fatalf("readSessionMode after removal = %q, want empty", got)
	}
}

func TestSystemPromptForModeCoding(t *testing.T) {
	const base = "You are GopherMind, a precise coding agent operating inside a software repository."

	for _, mode := range []string{"", "coding"} {
		if got := systemPromptForMode(mode, base, "/repo"); got != base {
			t.Fatalf("systemPromptForMode(%q) = %q, want basePrompt unchanged", mode, got)
		}
	}
}

func TestSystemPromptForModeConversational(t *testing.T) {
	const base = "You are GopherMind, a precise coding agent operating inside a software repository."

	got := systemPromptForMode("conversational", base, "/repo")
	if strings.Contains(got, "software repository") {
		t.Fatalf("conversational prompt must not read as a repo-bound coding agent, got: %s", got)
	}
	if strings.Contains(got, base) {
		t.Fatalf("conversational prompt must not reuse the coding basePrompt, got: %s", got)
	}
	if !strings.Contains(strings.ToLower(got), "assistant") {
		t.Fatalf("conversational prompt should read as a general assistant, got: %s", got)
	}
}

func TestSystemPromptForModePersona(t *testing.T) {
	root := t.TempDir()
	const base = "BASE"

	// Built-in preset persona.
	got := systemPromptForMode("reviewer", base, root)
	if !strings.HasPrefix(got, base+"\n\n") {
		t.Fatalf("systemPromptForMode(reviewer) = %q, want basePrompt+persona", got)
	}
	if !strings.Contains(got, "code reviewer") {
		t.Fatalf("systemPromptForMode(reviewer) = %q, want reviewer persona text", got)
	}

	// Unknown persona falls back to basePrompt unchanged.
	if got := systemPromptForMode("no-such-persona", base, root); got != base {
		t.Fatalf("systemPromptForMode(unknown) = %q, want basePrompt unchanged", got)
	}
}

func TestModesHandler(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/modes", nil)
	modesHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, id := range []string{"coding", "conversational", "reviewer", "architect", "tester"} {
		if !strings.Contains(body, `"`+id+`"`) {
			t.Fatalf("body = %s, missing mode %q", body, id)
		}
	}
}

func TestSessionConfigHandler(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())

	if err := writeSessionModel("cfg1", "gpt-4o"); err != nil {
		t.Fatalf("writeSessionModel: %v", err)
	}
	if err := writeSessionMode("cfg1", "conversational"); err != nil {
		t.Fatalf("writeSessionMode: %v", err)
	}

	h := sessionConfigHandler()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/session/cfg1/config", nil)
	req.SetPathValue("id", "cfg1")
	h(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"model":"gpt-4o"`) || !strings.Contains(body, `"mode":"conversational"`) {
		t.Fatalf("body = %s, want model+mode", body)
	}
}

func TestSessionConfigHandlerUnset(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())

	h := sessionConfigHandler()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/session/cfg2/config", nil)
	req.SetPathValue("id", "cfg2")
	h(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"model":""`) || !strings.Contains(body, `"mode":""`) {
		t.Fatalf("body = %s, want empty model+mode", body)
	}
}

func TestSessionCreateHandlerWritesModeSidecar(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())

	h := sessionCreateHandler()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/session", strings.NewReader(`{"id":"y","mode":"conversational"}`))
	h(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if got := readSessionMode("y"); got != "conversational" {
		t.Fatalf("readSessionMode(y) = %q, want %q", got, "conversational")
	}
}
