package update

import (
	"errors"
	"strings"
	"testing"
)

func TestCheckNotifiesWhenNewer(t *testing.T) {
	notice, has := Check("1.0.0", func() (string, error) { return "v1.2.0", nil })
	if !has {
		t.Fatal("expected an available update")
	}
	if !strings.Contains(notice, "1.2.0") || !strings.Contains(notice, "1.0.0") {
		t.Errorf("notice should mention both versions: %q", notice)
	}
}

func TestCheckSilentWhenCurrent(t *testing.T) {
	if notice, has := Check("1.2.0", func() (string, error) { return "1.2.0", nil }); has || notice != "" {
		t.Errorf("no update expected, got %q", notice)
	}
}

func TestCheckSilentWhenAhead(t *testing.T) {
	if _, has := Check("2.0.0", func() (string, error) { return "1.9.9", nil }); has {
		t.Error("local newer than latest should not notify")
	}
}

func TestCheckSkipsDevBuild(t *testing.T) {
	called := false
	_, has := Check("dev", func() (string, error) { called = true; return "1.0.0", nil })
	if has {
		t.Error("dev build should never report an update")
	}
	if called {
		t.Error("dev build should not even fetch")
	}
}

func TestCheckSwallowsFetchError(t *testing.T) {
	if notice, has := Check("1.0.0", func() (string, error) { return "", errors.New("network down") }); has || notice != "" {
		t.Errorf("fetch error must be silent, got %q", notice)
	}
}

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.2.0", "1.1.9", 1},
		{"2.0.0", "1.9.9", 1},
		{"1.0", "1.0.0", 0}, // missing patch treated as 0
		{"1.0.0", "1.0", 0},
		{"v1.0.0", "1.0.0", 0}, // leading v ignored
	}
	for _, c := range cases {
		if got := compareSemver(c.a, c.b); got != c.want {
			t.Errorf("compareSemver(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
