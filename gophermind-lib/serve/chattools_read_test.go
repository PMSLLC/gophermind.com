package serve

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/tools"
)

func chatToolsEnv(t *testing.T) (*chatTools, string) {
	t.Helper()
	dir := t.TempDir()
	m, err := newBriefManager(Options{BriefsDir: dir}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &chatTools{bm: m}, dir
}

func writeRunRecord(t *testing.T, dir string, rec briefRecord, log string) {
	t.Helper()
	d := filepath.Join(dir, rec.ID)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(rec)
	if err := os.WriteFile(filepath.Join(d, "run.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if log != "" {
		if err := os.WriteFile(filepath.Join(d, "run.log"), []byte(log), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func findTool(t *testing.T, c *chatTools, name string) tools.Tool {
	t.Helper()
	for _, tl := range c.all() {
		if tl.Name == name {
			return tl
		}
	}
	t.Fatalf("no tool %s", name)
	return tools.Tool{}
}

func TestBriefsListNewestFirst(t *testing.T) {
	c, dir := chatToolsEnv(t)
	writeRunRecord(t, dir, briefRecord{ID: "gm-2026-10-23-001", Title: "Older", Phase: phaseDone, StartedAt: "2026-10-23T10:00:00Z"}, "")
	writeRunRecord(t, dir, briefRecord{ID: "gm-2026-10-24-001", Title: "Newer", Phase: phaseRunning, StartedAt: "2026-10-24T10:00:00Z"}, "")
	out, err := findTool(t, c, "briefs_list").Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	ni, oi := strings.Index(out, "gm-2026-10-24-001"), strings.Index(out, "gm-2026-10-23-001")
	if ni < 0 || oi < 0 || ni > oi {
		t.Errorf("order wrong:\n%s", out)
	}
	if !strings.Contains(out, "Newer") || !strings.Contains(out, "running") || !strings.Contains(out, "2026-10-24T10:00:00Z") {
		t.Errorf("missing fields:\n%s", out)
	}
}

func TestBriefsListEmpty(t *testing.T) {
	c, _ := chatToolsEnv(t)
	out, err := findTool(t, c, "briefs_list").Run(context.Background(), nil)
	if err != nil || out != "no briefs" {
		t.Errorf("got %q, %v", out, err)
	}
}

func TestBriefStateRejectsBadID(t *testing.T) {
	c, _ := chatToolsEnv(t)
	for _, id := range []string{"../x", "", "a/b"} {
		raw, _ := json.Marshal(map[string]string{"id": id})
		if _, err := findTool(t, c, "brief_state").Run(context.Background(), raw); err == nil {
			t.Errorf("id %q accepted", id)
		}
	}
}

func TestBriefStateCapsLog(t *testing.T) {
	c, dir := chatToolsEnv(t)
	id := "gm-2026-10-24-002"
	log := strings.Repeat("line of log output that is fairly long to fill space quickly....\n", 3200)
	if len(log) < 200<<10 {
		t.Fatalf("log only %d", len(log))
	}
	writeRunRecord(t, dir, briefRecord{ID: id, Title: "Big", Phase: phaseRunning, StartedAt: "2026-10-24T10:00:00Z"}, log+"LAST-LINE\n")
	raw, _ := json.Marshal(map[string]string{"id": id})
	out, err := findTool(t, c, "brief_state").Run(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > 8<<10 {
		t.Errorf("output %d bytes, over cap", len(out))
	}
	if !strings.Contains(out, "[tail of ") || !strings.Contains(out, "full text at") || !strings.Contains(out, "LAST-LINE") {
		t.Errorf("no note or tail:\n%s", out[len(out)-300:])
	}
}

func TestOutputRedactsSecrets(t *testing.T) {
	c, dir := chatToolsEnv(t)
	id := "gm-2026-10-24-003"
	writeRunRecord(t, dir, briefRecord{ID: id, Phase: phaseRunning, StartedAt: "2026-10-24T10:00:00Z"},
		"calling api with Authorization: Bearer abcdef1234567890abcdef1234567890\nowner jane.doe@example.com\n")
	raw, _ := json.Marshal(map[string]string{"id": id})
	out, err := findTool(t, c, "brief_state").Run(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "abcdef1234567890abcdef1234567890") {
		t.Errorf("token leaked:\n%s", out)
	}
}
