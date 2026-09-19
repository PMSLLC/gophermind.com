package plan

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func joined(cs []Chunk) string {
	var b strings.Builder
	for _, c := range cs {
		b.WriteString(c.Text)
	}
	return b.String()
}

const sampleBrief = "Intro paragraph before any heading.\n\n" +
	"# Alpha\nalpha line one\nalpha line two\n\n" +
	"## Beta\nbeta body\n\n" +
	"# Gamma\ngamma body that is a bit longer than the others\n"

func TestSplitBriefIsLosslessAtEveryBudget(t *testing.T) {
	for _, max := range []int{1, 7, 20, 40, 80, 200, 5000} {
		cs := SplitBrief(sampleBrief, max)
		if got := joined(cs); got != sampleBrief {
			t.Errorf("max=%d: chunks do not reproduce the brief:\n%q", max, got)
		}
		for i, c := range cs {
			if c.Index != i {
				t.Errorf("max=%d: chunk %d has Index %d", max, i, c.Index)
			}
			if len(c.Text) > max && max >= 4 {
				t.Errorf("max=%d: chunk %d is %d bytes", max, i, len(c.Text))
			}
			if !utf8.ValidString(c.Text) {
				t.Errorf("max=%d: chunk %d is not valid UTF-8", max, i)
			}
		}
	}
}

func TestSplitBriefPacksSmallSectionsAndBreaksAtHeadings(t *testing.T) {
	one := SplitBrief(sampleBrief, 5000)
	if len(one) != 1 || one[0].Title != "" {
		t.Fatalf("a brief that fits is one chunk with the first section's title, got %d chunks", len(one))
	}
	cs := SplitBrief(sampleBrief, 70)
	if len(cs) < 2 {
		t.Fatalf("expected several chunks, got %d", len(cs))
	}
	for i, c := range cs[1:] {
		if !strings.HasPrefix(c.Text, "#") && !strings.HasPrefix(c.Text, "gamma") && !strings.HasPrefix(c.Text, "alpha") && !strings.HasPrefix(c.Text, "beta") {
			t.Errorf("chunk %d starts mid-sentence: %q", i+1, c.Text)
		}
	}
	titles := map[string]bool{}
	for _, c := range cs {
		titles[c.Title] = true
	}
	for _, want := range []string{"Alpha", "Gamma"} {
		if !titles[want] {
			t.Errorf("no chunk titled %q; titles = %v", want, titles)
		}
	}
}

func TestSplitBriefSplitsAnOversizedSectionAtParagraphs(t *testing.T) {
	para := strings.Repeat("word ", 20) + "\n\n" // 102 bytes
	brief := "# Big\n" + strings.Repeat(para, 6)
	cs := SplitBrief(brief, 250)
	if joined(cs) != brief {
		t.Fatal("not lossless")
	}
	for i, c := range cs {
		if len(c.Text) > 250 {
			t.Errorf("chunk %d is %d bytes", i, len(c.Text))
		}
		if c.Title != "Big" {
			t.Errorf("chunk %d title = %q, want Big", i, c.Title)
		}
		if i < len(cs)-1 && !strings.HasSuffix(c.Text, "\n\n") {
			t.Errorf("chunk %d does not end at a paragraph break: %q", i, c.Text[len(c.Text)-10:])
		}
	}
}

func TestSplitBriefNeverSplitsARune(t *testing.T) {
	brief := strings.Repeat("é", 100) // one 200-byte line, no newlines
	cs := SplitBrief(brief, 31)
	if joined(cs) != brief {
		t.Fatal("not lossless")
	}
	for i, c := range cs {
		if !utf8.ValidString(c.Text) {
			t.Errorf("chunk %d splits a rune", i)
		}
	}
}

func TestSplitBriefEmpty(t *testing.T) {
	for _, in := range []string{"", "   \n\n  "} {
		if cs := SplitBrief(in, 100); cs != nil {
			t.Errorf("SplitBrief(%q) = %v, want nil", in, cs)
		}
	}
}

func TestHeadingTitle(t *testing.T) {
	good := map[string]string{"# A\n": "A", "###### deep\r\n": "deep", "## spaced   title  \n": "spaced   title", "# \n": ""}
	for line, want := range good {
		if got, ok := headingTitle(line); !ok || got != want {
			t.Errorf("headingTitle(%q) = %q, %v; want %q", line, got, ok, want)
		}
	}
	for _, line := range []string{"#nospace\n", "####### seven\n", "text\n", "#\n"} {
		if title, ok := headingTitle(line); ok {
			t.Errorf("headingTitle(%q) accepted a non-heading as %q", line, title)
		}
	}
}
