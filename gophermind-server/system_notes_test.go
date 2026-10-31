package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gophermind/gophermind-lib/serve"
)

const notesBase = "base prompt line one\n\nsecond paragraph"

func TestWithSystemNotesAppendsWhenNoBlock(t *testing.T) {
	got := withSystemNotes(notesBase, true, false, true)
	if !strings.HasPrefix(got, notesBase+"\n\n"+notesOpen) || !strings.HasSuffix(got, notesClose) {
		t.Fatalf("block not appended after the base prompt:\n%s", got)
	}
	if strings.Contains(got, "burrow_lookup") {
		t.Errorf("burrow sentence present without burrow")
	}
}

func TestWithSystemNotesReplacesStaleBlock(t *testing.T) {
	stale := notesBase + "\n\n" + notesOpen + "\nold stale text\n" + notesClose
	got := withSystemNotes(stale, true, false, true)
	if strings.Contains(got, "old stale text") {
		t.Fatalf("stale text kept:\n%s", got)
	}
	if strings.Count(got, notesOpen) != 1 || strings.Count(got, notesClose) != 1 {
		t.Fatalf("expected exactly one block:\n%s", got)
	}
	if !strings.HasPrefix(got, notesBase+"\n\n"+notesOpen) {
		t.Errorf("rest of the prompt changed")
	}
	// Text after the block is preserved byte for byte.
	withTail := stale + "\n\nlater tail"
	if g := withSystemNotes(withTail, true, false, true); !strings.HasSuffix(g, notesClose+"\n\nlater tail") {
		t.Errorf("tail after block changed:\n%s", g)
	}
}

func TestWithSystemNotesIdempotent(t *testing.T) {
	for _, burrow := range []bool{false, true} {
		once := withSystemNotes(notesBase, true, burrow, true)
		if twice := withSystemNotes(once, true, burrow, true); twice != once {
			t.Errorf("burrow=%v: second run changed the prompt", burrow)
		}
	}
}

func TestWithSystemNotesRemovesBlockWhenToolsAbsent(t *testing.T) {
	with := withSystemNotes(notesBase, true, true, true)
	if got := withSystemNotes(with, false, false, true); got != notesBase {
		t.Errorf("block not removed cleanly: %q", got)
	}
	if got := withSystemNotes(notesBase, false, false, true); got != notesBase {
		t.Errorf("prompt without block changed: %q", got)
	}
}

func TestWithSystemNotesBurrowOnlyWhenOn(t *testing.T) {
	if got := withSystemNotes(notesBase, true, true, true); !strings.Contains(got, "matches exact contracts only") {
		t.Errorf("burrow on but sentence missing")
	}
	if got := withSystemNotes(notesBase, true, false, true); strings.Contains(got, "matches exact contracts only") {
		t.Errorf("burrow off but sentence present")
	}
	// Turning burrow off later removes the sentence from an existing block.
	on := withSystemNotes(notesBase, true, true, true)
	if got := withSystemNotes(on, true, false, true); strings.Contains(got, "matches exact contracts only") {
		t.Errorf("burrow sentence survived a refresh without burrow")
	}
}

func TestSystemNotesOrientationText(t *testing.T) {
	got := withSystemNotes("", true, false, true)
	for _, want := range []string{"briefs_list", "harness_state", "questions_pending", "runs_digest", "empty scratch workspace", "Never list files", "1. Call briefs_list", "4. Call runs_digest"} {
		if !strings.Contains(got, want) {
			t.Errorf("notes lack %q", want)
		}
	}
	if strings.ContainsAny(got, "—–") {
		t.Errorf("notes contain a dash character")
	}
}

// serverTurn runs one turn against a server built from cfg, in the
// session id, and returns the model request body.
func serverTurn(t *testing.T, cfg serverConfig, llmURL string, bodies func() []string) string {
	t.Helper()
	root := t.TempDir()
	cfg.Token, cfg.LLMEndpoint, cfg.Root = "t", llmURL, root
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d, err := buildDeps(cfg, root, logger)
	if err != nil {
		t.Fatalf("buildDeps: %v", err)
	}
	mux, err := serve.NewMux(d, briefOptions(cfg, serve.Options{Token: "t"}))
	if err != nil {
		t.Fatalf("serve.NewMux: %v", err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	before := len(bodies())
	resp, err := http.DefaultClient.Do(authedReq(t, http.MethodPost, srv.URL+"/session", "t", `{"id":"on-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	resp, err = http.DefaultClient.Do(authedReq(t, http.MethodPost, srv.URL+"/session/on-1/stream", "t", "where is everything at right now?"))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	got := bodies()
	if len(got) <= before {
		t.Fatal("no model request recorded")
	}
	return got[len(got)-1]
}

func TestExistingSessionGetsNotesOnNextTurn(t *testing.T) {
	llm, bodies := recordingLLM(t, "ok")
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())

	// Turn 1 with no brief tools: the saved session has an old prompt, no block.
	first := serverTurn(t, serverConfig{}, llm.URL, bodies)
	if strings.Contains(first, "gophermind-system-notes") {
		t.Fatal("first turn without tools already has the block")
	}

	// Turn 2 with the tools on: the older session gains the block.
	second := serverTurn(t, serverConfig{BriefsDir: t.TempDir()}, llm.URL, bodies)
	for _, want := range []string{"[gophermind-system-notes]", "briefs_list", "harness_state", "questions_pending", "empty scratch workspace"} {
		if !strings.Contains(second, want) {
			t.Errorf("second turn request lacks %q", want)
		}
	}

	// Turn 3: no duplicate.
	third := serverTurn(t, serverConfig{BriefsDir: t.TempDir()}, llm.URL, bodies)
	if n := strings.Count(third, "[gophermind-system-notes]"); n != 1 {
		t.Errorf("third turn has %d blocks, want 1", n)
	}

	// Turn 4 without tools: block removed.
	fourth := serverTurn(t, serverConfig{}, llm.URL, bodies)
	if strings.Contains(fourth, "gophermind-system-notes") {
		t.Errorf("block kept after tools went away")
	}
}

func TestNewSessionWithoutBriefsDirHasNoBlock(t *testing.T) {
	if req := firstTurnRequest(t, ""); strings.Contains(req, "gophermind-system-notes") {
		t.Errorf("session without BriefsDir has the block")
	}
}

func TestWithSystemNotesStripsLegacyGuidance(t *testing.T) {
	old := notesBase + "\n\n" + legacyBriefToolsGuidance + "\n\n" + legacyBurrowToolGuidance
	want := withSystemNotes(notesBase, true, true, true)
	if got := withSystemNotes(old, true, true, true); got != want {
		t.Errorf("legacy text not migrated:\n%s", got)
	}
	if got := withSystemNotes(old, false, false, true); got != notesBase {
		t.Errorf("legacy text kept when tools absent: %q", got)
	}
	if got := withSystemNotes(legacyBriefToolsGuidance+"\n\n"+notesBase, true, false, true); strings.Contains(got, "You can look at briefs") || !strings.Contains(got, notesBase+"\n\n"+notesOpen) {
		t.Errorf("leading legacy text mishandled:\n%s", got)
	}
	// A burrow sentence inside the block is the current one and stays.
	cur := withSystemNotes(notesBase, true, true, true)
	if withSystemNotes(cur, true, true, true) != cur {
		t.Errorf("block burrow sentence disturbed")
	}
	if got := withSystemNotes(notesBase, true, false, true); got != withSystemNotes(notesBase, true, false, true) || strings.Contains(got, "You can look at briefs") {
		t.Errorf("untouched prompt changed")
	}
}

func TestWithSystemNotesWorkingFolderVariants(t *testing.T) {
	scratch := withSystemNotes(notesBase, true, false, true)
	project := withSystemNotes(notesBase, true, false, false)
	if !strings.Contains(scratch, "empty scratch workspace") || strings.Contains(project, "scratch") {
		t.Errorf("variants wrong:\n%s\n%s", scratch, project)
	}
	if !strings.Contains(project, "project John pointed this chat at") || !strings.Contains(project, "brief tools first") {
		t.Errorf("project variant lacks its sentence")
	}
	if got := withSystemNotes(scratch, true, false, false); got != project {
		t.Errorf("switch scratch to project did not update")
	}
	if got := withSystemNotes(project, true, false, true); got != scratch {
		t.Errorf("switch project to scratch did not update")
	}
}

func TestWithSystemNotesStrayMarkerInPersona(t *testing.T) {
	persona := "persona says " + notesOpen + " is a marker\n\nmore persona"
	got := withSystemNotes(persona, true, false, true)
	if !strings.HasPrefix(got, persona+"\n\n"+notesOpen) {
		t.Fatalf("persona changed or block misplaced:\n%s", got)
	}
	again := withSystemNotes(got, true, false, false)
	if !strings.HasPrefix(again, persona+"\n\n"+notesOpen) || strings.Count(again, notesClose) != 1 {
		t.Errorf("refresh spanned user text:\n%s", again)
	}
}

func TestWithSystemNotesRemovalAtStart(t *testing.T) {
	only := withSystemNotes("", true, false, true)
	if got := withSystemNotes(only, false, false, true); got != "" {
		t.Errorf("leftover after removal: %q", got)
	}
	atStart := only + "\n\nrest of prompt"
	if got := withSystemNotes(atStart, false, false, true); got != "rest of prompt" {
		t.Errorf("leading newlines left: %q", got)
	}
}

func TestPersonaPromptKeptByteIdentical(t *testing.T) {
	persona := "You are a pirate.\n\nSpeak in rhyme."
	once := withSystemNotes(persona, true, false, true)
	if !strings.HasPrefix(once, persona+"\n\n"+notesOpen) || strings.Count(once, notesOpen) != 1 {
		t.Fatalf("persona or block wrong:\n%s", once)
	}
	twice := withSystemNotes(once, true, false, true)
	if twice != once {
		t.Errorf("second pass changed the prompt")
	}
}
