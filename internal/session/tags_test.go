package session

import (
	"testing"
	"time"
)

func TestSetAndGetTags(t *testing.T) {
	dir := t.TempDir()
	if err := setTagsIn(dir, "sess1", []string{"important", "refactor"}); err != nil {
		t.Fatal(err)
	}
	got := tagsIn(dir, "sess1")
	if len(got) != 2 || got[0] != "important" || got[1] != "refactor" {
		t.Errorf("unexpected tags: %v", got)
	}
	if len(tagsIn(dir, "other")) != 0 {
		t.Error("untagged session should have no tags")
	}
}

func TestAddTagsDeduplicates(t *testing.T) {
	dir := t.TempDir()
	if err := addTagsIn(dir, "s", []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if err := addTagsIn(dir, "s", []string{"b", "c"}); err != nil {
		t.Fatal(err)
	}
	got := tagsIn(dir, "s")
	if len(got) != 3 {
		t.Errorf("expected deduped [a b c], got %v", got)
	}
}

func TestSetTagsRejectsBadID(t *testing.T) {
	if err := setTagsIn(t.TempDir(), "../evil", []string{"x"}); err == nil {
		t.Error("bad id should be rejected")
	}
}

func TestFilterInfos(t *testing.T) {
	now := time.Now()
	infos := []Info{
		{ID: "a", ModTime: now},
		{ID: "b", ModTime: now.Add(-48 * time.Hour)},
		{ID: "c", ModTime: now},
	}
	tagsOf := func(id string) []string {
		if id == "a" || id == "c" {
			return []string{"keep"}
		}
		return nil
	}

	// Tag filter.
	got := FilterInfos(infos, Filter{Tag: "keep"}, tagsOf)
	if len(got) != 2 {
		t.Errorf("tag filter: expected 2, got %d", len(got))
	}

	// Date filter: only sessions newer than 24h ago.
	got = FilterInfos(infos, Filter{Since: now.Add(-24 * time.Hour)}, tagsOf)
	for _, i := range got {
		if i.ID == "b" {
			t.Error("b is older than the Since bound and should be excluded")
		}
	}
	if len(got) != 2 {
		t.Errorf("date filter: expected 2, got %d", len(got))
	}

	// Combined tag + date.
	got = FilterInfos(infos, Filter{Tag: "keep", Since: now.Add(-24 * time.Hour)}, tagsOf)
	if len(got) != 2 {
		t.Errorf("combined filter: expected 2, got %d", len(got))
	}
}
