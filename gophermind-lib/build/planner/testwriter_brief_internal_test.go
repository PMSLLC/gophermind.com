package planner

import (
	"strings"
	"testing"
)

func TestTestwriterPromptCarriesBriefSection(t *testing.T) {
	brief := "### Make a slug\n\n- a `max` of 0 or less means no limit."
	got, err := renderTestwriterBrief(`{"contract":{"signature":"func Make(s string, max int) string"}}`,
		"example.com/slugify", "internal/slug/make.go", "TestFnMake", "internal/slug/fn_make_test.go", nil, brief)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "<brief_section>") || !strings.Contains(got, "0 or less means no limit") {
		t.Fatal("the prompt lacks the brief section")
	}
	if !strings.Contains(got, "the brief wins") {
		t.Error("the prompt does not say the brief wins over the node")
	}
	plain, err := renderTestwriterWith(`{"contract":{"signature":"func Make(s string, max int) string"}}`,
		"example.com/slugify", "internal/slug/make.go", "TestFnMake", "internal/slug/fn_make_test.go", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain, "<brief_section>") {
		t.Error("an empty brief section is still rendered")
	}
}
