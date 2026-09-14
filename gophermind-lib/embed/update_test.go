package embed

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateIndexReembedsOnlyChanged(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha alpha"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("beta beta"), 0o644)

	idx, err := BuildIndex(context.Background(), fakeProvider{}, dir, []string{".txt"})
	if err != nil {
		t.Fatal(err)
	}
	// Capture b's original vector to prove it is untouched.
	var bBefore []float32
	for _, v := range idx.Vectors {
		if strings.HasPrefix(v.ID, "b.txt") {
			bBefore = v.Values
		}
	}

	// Change a.txt, then incrementally update only a.txt.
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("gamma gamma gamma"), 0o644)
	updated, err := UpdateIndex(context.Background(), fakeProvider{}, dir, []string{".txt"}, idx, []string{"a.txt"})
	if err != nil {
		t.Fatal(err)
	}

	var aText string
	var bAfter []float32
	for _, v := range updated.Vectors {
		if strings.HasPrefix(v.ID, "a.txt") {
			aText = v.Text
		}
		if strings.HasPrefix(v.ID, "b.txt") {
			bAfter = v.Values
		}
	}
	if !strings.Contains(aText, "gamma") {
		t.Errorf("a.txt should have been re-embedded with new content, got %q", aText)
	}
	if len(bBefore) != len(bAfter) || bBefore[0] != bAfter[0] {
		t.Errorf("b.txt vector should be unchanged: %v vs %v", bBefore, bAfter)
	}
}

func TestUpdateIndexRemovesDeletedFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha"), 0o644)
	idx, _ := BuildIndex(context.Background(), fakeProvider{}, dir, []string{".txt"})
	// a.txt is "changed" but now deleted -> its vectors should be dropped.
	os.Remove(filepath.Join(dir, "a.txt"))
	updated, err := UpdateIndex(context.Background(), fakeProvider{}, dir, []string{".txt"}, idx, []string{"a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range updated.Vectors {
		if strings.HasPrefix(v.ID, "a.txt") {
			t.Errorf("deleted file's vectors should be removed: %s", v.ID)
		}
	}
}
