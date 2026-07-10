package prompt

import (
	"strings"
	"testing"
)

func TestBuilderAccounting(t *testing.T) {
	b, err := NewBuilder()
	if err != nil {
		t.Fatal(err)
	}
	costs, total := b.Accounting()
	if len(costs) == 0 {
		t.Fatal("expected per-section costs")
	}
	sum := 0
	for _, c := range costs {
		if c.Tokens <= 0 {
			t.Errorf("section %q has non-positive tokens: %d", c.Name, c.Tokens)
		}
		sum += c.Tokens
	}
	if sum != total {
		t.Errorf("section tokens (%d) do not sum to total (%d)", sum, total)
	}
	// The default template has a role section.
	var hasRole bool
	for _, c := range costs {
		if c.Name == "role" {
			hasRole = true
		}
	}
	if !hasRole {
		t.Errorf("expected a role section in accounting: %+v", costs)
	}
}

func TestAccountingDropsEmptySections(t *testing.T) {
	// project_context resolves to empty with no context injected, so it must not
	// appear in the accounting.
	b, err := NewBuilder()
	if err != nil {
		t.Fatal(err)
	}
	costs, _ := b.Accounting()
	for _, c := range costs {
		if c.Name == "project_context" {
			t.Errorf("empty project_context should be dropped: %+v", costs)
		}
	}
}

func TestRenderAccounting(t *testing.T) {
	b, err := NewBuilder()
	if err != nil {
		t.Fatal(err)
	}
	out := b.RenderAccounting()
	if !strings.Contains(out, "role") || !strings.Contains(out, "total") {
		t.Errorf("rendered accounting missing role/total:\n%s", out)
	}
}
