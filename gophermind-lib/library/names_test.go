package library

import (
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/build/planner"
)

func TestNextName(t *testing.T) {
	tests := []struct {
		parent   string
		children []string
		want     string
	}{
		{"", nil, "v1"},
		{"v1", nil, "v1.1"},
		{"v1.1", nil, "v1.2"},
		{"v1.1", []string{"v1.2"}, "v1.1.1"},
		{"v1.1", []string{"v1.2", "v1.1.1"}, "v1.1b.1"},
		{"v1.1", []string{"v1.2", "v1.1.1", "v1.1b.1"}, "v1.1c.1"},
		{"v1.1.1", nil, "v1.1.2"},
		{"v1", []string{"v1.1"}, "v1b.1"},
		{"v1.1b.2", nil, "v1.1b.3"},
	}
	for _, tc := range tests {
		all := append([]string{tc.parent}, tc.children...)
		got, err := NextName(tc.parent, all, tc.children)
		if err != nil || got != tc.want {
			t.Errorf("NextName(%q, %v) = %q, %v; want %q", tc.parent, tc.children, got, err, tc.want)
		}
	}
}

func TestNextNameContinuationTakenFallsBack(t *testing.T) {
	// v1.2 exists elsewhere in the family, so v1.1 with no children cannot continue to v1.2.
	got, err := NextName("v1.1", []string{"v1.1", "v1.2"}, nil)
	if err != nil || got != "v1.1.1" {
		t.Errorf("got %q, %v; want v1.1.1", got, err)
	}
}

func TestNextNameExhausted(t *testing.T) {
	children := []string{"v1.2", "v1.1.1"}
	all := append([]string{"v1.1"}, children...)
	for c := 'b'; c <= 'z'; c++ {
		n := "v1.1" + string(c) + ".1"
		children = append(children, n)
		all = append(all, n)
	}
	if got, err := NextName("v1.1", all, children); err == nil {
		t.Errorf("expected error, got %q", got)
	}
}

func TestSlug(t *testing.T) {
	ok := []struct{ in, want string }{
		{"csvstat", "csvstat"},
		{"AI Venture Studio", "ai-venture-studio"},
		{"../x", "x"},
		{"  --Hello,   World!!  ", "hello-world"},
		{"café bar", "caf-bar"},
	}
	for _, tc := range ok {
		got, err := Slug(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("Slug(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	for _, in := range []string{"", "///", "éè", "   "} {
		if got, err := Slug(in); err == nil {
			t.Errorf("Slug(%q) = %q, want error", in, got)
		}
	}
	long := strings.Repeat("a", 59) + " bbbbbbbbbb"
	got, err := Slug(long)
	if err != nil || len(got) > 60 || strings.HasSuffix(got, "-") {
		t.Errorf("long slug = %q, %v", got, err)
	}
	got, err = Slug(strings.Repeat("a", 300))
	if err != nil || len(got) != 60 {
		t.Errorf("300 chars = %q (%d), %v", got, len(got), err)
	}
}

func TestRunName(t *testing.T) {
	day := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	if got := RunName("csvstat", "v1.1.2", day, 4); got != "csvstat-v1.1.2-20261029-r004" {
		t.Errorf("got %q", got)
	}
	if got := RunName("csvstat", "v1.1.2", day, 1000); got != "csvstat-v1.1.2-20261029-r1000" {
		t.Errorf("got %q", got)
	}
	for _, n := range []int{1, 4, 999, 1000} {
		id := RunName("csvstat", "v1.1b.2", day, n)
		if !planner.ValidRunID(id) {
			t.Errorf("%q not a valid run id", id)
		}
	}
}
