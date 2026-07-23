package projectctx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestGatherEmptyWhenNothingPresent: a fresh repo must not produce a digest
// full of headings with no content.
func TestGatherEmptyWhenNothingPresent(t *testing.T) {
	if got := Gather(t.TempDir()); got != "" {
		t.Errorf("Gather = %q, want empty", got)
	}
}

// TestGatherIncludesEachSource covers the three trees plus PROJECT.md.
func TestGatherIncludesEachSource(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".planning/SPEC.md", "SPEC-MARKER goals and scope")
	writeFile(t, root, ".remember/now.md", "NOW-MARKER current work")
	writeFile(t, root, ".superpowers/sdd/progress.md", "LEDGER-MARKER task 1 done")
	writeFile(t, root, "PROJECT.md", "CONVENTIONS-MARKER build with make")

	got := Gather(root)
	for _, want := range []string{"SPEC-MARKER", "NOW-MARKER", "LEDGER-MARKER", "CONVENTIONS-MARKER"} {
		if !strings.Contains(got, want) {
			t.Errorf("digest missing %q:\n%s", want, got)
		}
	}
}

// TestGatherSkipsEmptyFiles avoids emitting a heading for a zero-byte file.
func TestGatherSkipsEmptyFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".remember/now.md", "   \n\n")
	if got := Gather(root); got != "" {
		t.Errorf("Gather = %q, want empty for a blank file", got)
	}
}

// TestGatherRespectsTotalCap is the constraint that matters on a small local
// context window.
func TestGatherRespectsTotalCap(t *testing.T) {
	root := t.TempDir()
	big := strings.Repeat("x", 50_000)
	writeFile(t, root, ".planning/SPEC.md", big)
	writeFile(t, root, ".planning/ROADMAP.md", big)
	writeFile(t, root, ".remember/now.md", big)
	writeFile(t, root, ".remember/recent.md", big)
	writeFile(t, root, ".superpowers/sdd/progress.md", big)

	got := Gather(root)
	if len([]rune(got)) > totalMax+500 { // +headings overhead
		t.Errorf("digest is %d runes, want <= ~%d", len([]rune(got)), totalMax)
	}
}

// TestGatherTruncationIsMarked: a silently truncated spec would read as the
// whole spec.
func TestGatherTruncationIsMarked(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".planning/SPEC.md", strings.Repeat("y", perSourceMax*2))

	got := Gather(root)
	if !strings.Contains(got, "truncated") {
		t.Errorf("truncation not marked:\n%s", got[:200])
	}
}

// TestLedgerKeepsTheTail: the ledger is append-only, so the recent end is the
// useful part.
func TestLedgerKeepsTheTail(t *testing.T) {
	root := t.TempDir()
	body := "OLDEST-MARKER\n" + strings.Repeat("z", perSourceMax*2) + "\nNEWEST-MARKER"
	writeFile(t, root, ".superpowers/sdd/progress.md", body)

	got := Gather(root)
	if !strings.Contains(got, "NEWEST-MARKER") {
		t.Error("ledger tail was dropped; the most recent entries are the useful ones")
	}
	if strings.Contains(got, "OLDEST-MARKER") {
		t.Error("ledger head was kept; it should have been trimmed from the front")
	}
}

// TestPlanIsOrderedFirst: an existing plan is the most directly reusable
// source, so it leads the digest. Per-source caps mean it does not starve the
// other sources -- both appear, in priority order.
func TestPlanIsOrderedFirst(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".planning/SPEC.md", "SPEC-MARKER "+strings.Repeat("a", totalMax))
	writeFile(t, root, ".remember/now.md", "MEMORY-MARKER")

	got := Gather(root)
	if !strings.Contains(got, "SPEC-MARKER") {
		t.Fatal("the spec should always make it into the digest")
	}
	if !strings.Contains(got, "MEMORY-MARKER") {
		t.Fatal("memory should still fit alongside a capped spec")
	}
	if strings.Index(got, "SPEC-MARKER") > strings.Index(got, "MEMORY-MARKER") {
		t.Error("memory was ordered ahead of the existing plan")
	}
}
