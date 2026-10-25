package serve

import (
	"net/http"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/brief"
)

func TestBriefSpecDownload(t *testing.T) {
	e := newBriefEnv(t)
	w := e.get("/app/brief-spec.md")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /app/brief-spec.md = %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Errorf("Content-Type %q, want text/markdown", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); cd != `attachment; filename="gophermind-brief-spec.md"` {
		t.Errorf("Content-Disposition %q", cd)
	}
	if !strings.HasPrefix(w.Body.String(), "# GopherMind brief specification") {
		t.Errorf("body starts %q", w.Body.String()[:40])
	}
	// A request without a session is not served the file.
	if w := do(e.h, "GET", "/app/brief-spec.md", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("without a session = %d, want 401", w.Code)
	}
}

func TestUploadPageLinksTheBriefSpec(t *testing.T) {
	e := newBriefEnv(t)
	w := e.get("/app/briefs/new")
	if !strings.Contains(w.Body.String(), `href="/app/brief-spec.md"`) {
		t.Error("the upload page does not link /app/brief-spec.md")
	}
}

// The example in the specification is the one thing a reader copies, so it
// must pass the same parser the upload uses.
func TestBriefSpecExampleParses(t *testing.T) {
	e := newBriefEnv(t)
	body := e.get("/app/brief-spec.md").Body.String()
	const open = "```markdown\n"
	i := strings.Index(body, open)
	if i < 0 {
		t.Fatal("no markdown example in the specification")
	}
	rest := body[i+len(open):]
	j := strings.Index(rest, "\n```\n")
	if j < 0 {
		t.Fatal("the example is not closed")
	}
	b, err := brief.Parse([]byte(rest[:j] + "\n"))
	if err != nil {
		t.Fatalf("the example brief is refused: %v", err)
	}
	if len(b.Features) < 2 {
		t.Errorf("example has %d features, want 2", len(b.Features))
	}
}
