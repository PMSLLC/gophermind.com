package main

import (
	"testing"

	"gophermind/internal/session"
)

func TestChoosePicked(t *testing.T) {
	infos := []session.Info{{ID: "aaa"}, {ID: "bbb"}, {ID: "ccc"}}

	// valid 1-based selection
	id, err := choosePicked(infos, "2")
	if err != nil || id != "bbb" {
		t.Errorf("choice 2 = %q,%v; want bbb", id, err)
	}
	// empty / "0" / "n" means "start fresh" -> empty id, no error
	for _, in := range []string{"", "0", "n", "N"} {
		if id, err := choosePicked(infos, in); err != nil || id != "" {
			t.Errorf("choice %q = %q,%v; want fresh (empty)", in, id, err)
		}
	}
	// out of range
	if _, err := choosePicked(infos, "9"); err == nil {
		t.Error("out-of-range choice should error")
	}
	// non-numeric
	if _, err := choosePicked(infos, "abc"); err == nil {
		t.Error("non-numeric choice should error")
	}
}

func TestChoosePickedEmptyList(t *testing.T) {
	if id, err := choosePicked(nil, "1"); err != nil || id != "" {
		t.Errorf("no sessions should yield fresh: %q,%v", id, err)
	}
}
