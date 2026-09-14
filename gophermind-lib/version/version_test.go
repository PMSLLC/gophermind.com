package version

import (
	"strings"
	"testing"
)

func TestStringIncludesVersionCommitDate(t *testing.T) {
	oldV, oldC, oldD := Version, Commit, Date
	t.Cleanup(func() { Version, Commit, Date = oldV, oldC, oldD })

	Version, Commit, Date = "1.2.3", "abc1234", "2026-07-09"
	got := String()
	for _, want := range []string{"gophermind", "1.2.3", "abc1234", "2026-07-09"} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q, want it to contain %q", got, want)
		}
	}
}
