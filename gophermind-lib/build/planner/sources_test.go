package planner

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/brief"
)

func slugifyRun(t *testing.T) *run {
	t.Helper()
	raw, err := os.ReadFile("testdata/slugify/brief.md")
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.ReplaceAll(string(raw), "REPO_DIR", t.TempDir()))
	b, err := brief.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return &run{src: raw, brief: b, dir: t.TempDir()}
}

func TestComponentBriefLevel0IsTheWholeBrief(t *testing.T) {
	r := slugifyRun(t)
	if got := componentBrief(r, "cli-logic", 0); got != string(r.src) {
		t.Errorf("level 0 is not the whole brief")
	}
}

func TestComponentBriefAtShrinkLevelsKeepsEveryFeatureWhole(t *testing.T) {
	r := slugifyRun(t)
	for _, level := range []int{1, 2, 3} {
		s := componentBrief(r, "cli-logic", level)
		for _, want := range []string{"### Make a slug", "### Report errors",
			"`--max 6` on `Hello, World!` gives `hello`",
			"prints `usage: slugify [--max N] [FILE]`",
			"## Architecture", "## Constraints", "## Acceptance", "## Out of scope"} {
			if !strings.Contains(s, want) {
				t.Errorf("level %d lacks %q", level, want)
			}
		}
		for _, bad := range []string{"## Overview", "spec_version:", "(cut)"} {
			if strings.Contains(s, bad) {
				t.Errorf("level %d contains %q", level, bad)
			}
		}
	}
	if componentBrief(r, "cli-logic", 3) != componentBrief(r, "cli-logic", 1) {
		t.Error("level 3 differs from level 1")
	}
}

func TestComponentBriefLevel1HasNoOverviewOrFrontmatter(t *testing.T) {
	r := slugifyRun(t)
	s := componentBrief(r, "cli-logic", 1)
	if strings.Contains(s, "## Overview") || strings.Contains(s, "spec_version:") {
		t.Error("level 1 carries the overview or the frontmatter")
	}
}

func TestComponentSourcesListsEveryFeature(t *testing.T) {
	r := slugifyRun(t)
	want := []string{"feature-make-a-slug", "feature-report-errors", "section-architecture", "section-data", "section-constraints", "section-out-of-scope", "section-acceptance"}
	if got := componentSources(r, "slug-logic"); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBriefSourcesIdsAndSections(t *testing.T) {
	r := slugifyRun(t)
	var body string
	for _, f := range r.brief.Features {
		if f.Name == "Report errors" {
			body = f.Body
		}
	}
	want := sourceRef{ID: "feature-report-errors", Section: "Features / Report errors", Bytes: len(body)}
	for _, s := range briefSources(r.brief) {
		if s == want {
			return
		}
	}
	t.Errorf("no entry %+v in %+v", want, briefSources(r.brief))
}

func TestTooLongErrNamesWholeSources(t *testing.T) {
	if !strings.Contains(tooLongErr("contract:x", 1, 2, 3).Error(), "the brief sources were kept whole") {
		t.Error("message does not name whole sources")
	}
}

func TestWriteSourcesRecordsEveryComponent(t *testing.T) {
	r := slugifyRun(t)
	doc := map[string]any{"components": []any{map[string]any{"id": "slug-logic"}, map[string]any{"id": "cli-logic"}}}
	if err := writeSources(r, doc); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(r.path(stateSources))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Sources    []sourceRef         `json:"sources"`
		Components map[string][]string `json:"components"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Sources, briefSources(r.brief)) {
		t.Errorf("sources = %+v", got.Sources)
	}
	if len(got.Components) != 2 {
		t.Fatalf("components = %v", got.Components)
	}
	for _, id := range []string{"slug-logic", "cli-logic"} {
		if !reflect.DeepEqual(got.Components[id], componentSources(r, id)) {
			t.Errorf("components[%s] = %v", id, got.Components[id])
		}
	}
}
