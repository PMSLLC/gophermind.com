package serve

import (
	"regexp"
	"strings"
	"testing"
)

// TestBriefPageAcceptsBothRunIDShapes reads the embedded run page, pulls the
// JavaScript regex literal that extracts the run id from the path, and checks
// it with Go's regexp (the literal is RE2 compatible). A new library run id must
// pass, or every new run's page shows "Bad run id.".
func TestBriefPageAcceptsBothRunIDShapes(t *testing.T) {
	b, err := pagesAssets.ReadFile("assets/brief.html")
	if err != nil {
		t.Fatal(err)
	}
	lit := regexp.MustCompile(`const m = /(.+)/\.exec\(window\.location\.pathname\)`).FindSubmatch(b)
	if lit == nil {
		t.Fatal("run id regex literal not found in brief.html")
	}
	src := strings.ReplaceAll(string(lit[1]), `\/`, `/`)
	re, err := regexp.Compile(src)
	if err != nil {
		t.Fatalf("page regex %q is not RE2: %v", src, err)
	}
	for _, id := range []string{"csvstat-v1-20261101-r001", "csvstat-v1.1b.2-20261101-r1000", "gm-2026-10-30-001"} {
		m := re.FindStringSubmatch("/app/briefs/" + id)
		if m == nil || m[1] != id {
			t.Errorf("page regex rejects %q (match %v)", id, m)
		}
		if re.FindStringSubmatch("/app/briefs/"+id+"/") == nil {
			t.Errorf("page regex rejects %q with trailing slash", id)
		}
	}
	for _, id := range []string{"../x", "a/b", "CSVSTAT-v1-20261101-r001", "Gm-2026-10-30-001", ""} {
		if m := re.FindStringSubmatch("/app/briefs/" + id); m != nil {
			t.Errorf("page regex accepts %q", id)
		}
	}
}
