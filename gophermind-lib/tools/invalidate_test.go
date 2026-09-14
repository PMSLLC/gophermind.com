package tools

import (
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/embed"
)

// rememberBoth seeds a store with one parser fact and one database fact.
func rememberBoth(t *testing.T, memPath string) {
	t.Helper()
	tool := RememberFact(fakeEmbed{}, memPath)
	if _, err := run(t, tool, `{"text":"the parser lives in template.go"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, tool, `{"text":"the database uses sqlite"}`); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidateFactRetiresTheBestMatch(t *testing.T) {
	memPath := filepath.Join(t.TempDir(), "mem.json")
	rememberBoth(t, memPath)

	out, err := run(t, InvalidateFact(fakeEmbed{}, memPath), `{"query":"where the parser lives"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "template.go") {
		t.Errorf("result should name the fact it retired, got %q", out)
	}

	idx, err := embed.LoadIndex(memPath)
	if err != nil {
		t.Fatalf("load memory: %v", err)
	}
	for _, v := range idx.Vectors {
		switch v.Text {
		case "the parser lives in template.go":
			if v.ValidUntil == "" {
				t.Error("the matched fact should have been retired")
			}
		case "the database uses sqlite":
			if v.ValidUntil != "" {
				t.Error("an unrelated fact should stay live")
			}
		}
	}
	if len(idx.Vectors) != 2 {
		t.Errorf("retired facts should stay on disk, got %d vectors", len(idx.Vectors))
	}
}

func TestInvalidateFactRetiresOnlyOne(t *testing.T) {
	memPath := filepath.Join(t.TempDir(), "mem.json")
	tool := RememberFact(fakeEmbed{}, memPath)
	// Two live facts that are similar to the query but not to each other
	// enough to have superseded one another at write time.
	if _, err := run(t, tool, `{"text":"the parser is fast"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, tool, `{"text":"the network is slow"}`); err != nil {
		t.Fatal(err)
	}

	if _, err := run(t, InvalidateFact(fakeEmbed{}, memPath), `{"query":"the parser"}`); err != nil {
		t.Fatal(err)
	}

	idx, _ := embed.LoadIndex(memPath)
	retired := 0
	for _, v := range idx.Vectors {
		if v.ValidUntil != "" {
			retired++
		}
	}
	if retired != 1 {
		t.Errorf("retired %d facts, want exactly 1", retired)
	}
}

func TestInvalidateFactNoMatchLeavesStoreUnchanged(t *testing.T) {
	memPath := filepath.Join(t.TempDir(), "mem.json")
	rememberBoth(t, memPath)

	out, err := run(t, InvalidateFact(fakeEmbed{}, memPath), `{"query":"the network topology"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(out), "no") {
		t.Errorf("result should say nothing matched, got %q", out)
	}

	idx, _ := embed.LoadIndex(memPath)
	for _, v := range idx.Vectors {
		if v.ValidUntil != "" {
			t.Errorf("a below-threshold query retired %q", v.Text)
		}
	}
}

func TestInvalidateFactSkipsAlreadyRetiredFacts(t *testing.T) {
	memPath := filepath.Join(t.TempDir(), "mem.json")
	rememberBoth(t, memPath)
	tool := InvalidateFact(fakeEmbed{}, memPath)

	if _, err := run(t, tool, `{"query":"where the parser lives"}`); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, tool, `{"query":"where the parser lives"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(out), "no") {
		t.Errorf("a second invalidation should find nothing live, got %q", out)
	}
}

func TestInvalidateFactNilProvider(t *testing.T) {
	if _, err := run(t, InvalidateFact(nil, filepath.Join(t.TempDir(), "m.json")), `{"query":"x"}`); err == nil {
		t.Error("nil provider should error with configuration guidance")
	}
}

func TestInvalidateFactEmptyQuery(t *testing.T) {
	if _, err := run(t, InvalidateFact(fakeEmbed{}, filepath.Join(t.TempDir(), "m.json")), `{"query":"  "}`); err == nil {
		t.Error("empty query should error")
	}
}
