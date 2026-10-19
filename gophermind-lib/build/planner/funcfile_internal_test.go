package planner

import (
	"reflect"
	"testing"

	"gophermind/gophermind-lib/build/contract"
)

func ffContract(types []contract.Type, fns []contract.Function) *contract.Contracts {
	return &contract.Contracts{Module: "csvstat", Types: types, Functions: fns}
}

func ffFn(id, file, sig string) contract.Function {
	return contract.Function{ID: id, Package: "csvstat/internal/stats", File: file, Signature: sig}
}

func fileOf(c *contract.Contracts) map[string]string {
	m := map[string]string{}
	for _, f := range c.Functions {
		m[f.ID] = f.File
	}
	return m
}

const stats = "internal/stats/stats.go"

func TestUniqueFunctionFilesSharedWithTypeAndEachOther(t *testing.T) {
	c := ffContract(
		[]contract.Type{{ID: "ty-column-state", Package: "csvstat/internal/stats", File: stats, Decl: "type columnState struct{}"}},
		[]contract.Function{
			ffFn("fn-finalize-column-state", stats, "func finalizeColumnState(s *columnState) ColumnSummary"),
			ffFn("fn-update", stats, "func (s *columnState) Update(v string)"),
			ffFn("fn-solo", "internal/stats/solo.go", "func Solo()"),
		})
	changed := UniqueFunctionFiles(c)
	want := map[string]string{
		"fn-finalize-column-state": "internal/stats/finalize_column_state.go",
		"fn-update":                "internal/stats/column_state_update.go",
		"fn-solo":                  "internal/stats/solo.go",
	}
	if got := fileOf(c); !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %v", got)
	}
	if c.Types[0].File != stats {
		t.Fatalf("type file moved: %s", c.Types[0].File)
	}
	if !reflect.DeepEqual(changed, map[string]string{"fn-finalize-column-state": stats, "fn-update": stats}) {
		t.Fatalf("changed = %v", changed)
	}
	if again := UniqueFunctionFiles(c); len(again) != 0 {
		t.Fatalf("not idempotent: %v", again)
	}
}

func TestUniqueFunctionFilesLeavesUniqueFileAlone(t *testing.T) {
	c := ffContract(nil, []contract.Function{
		ffFn("fn-a", "internal/stats/a.go", "func A()"),
		ffFn("fn-b", "internal/stats/b.go", "func B()"),
	})
	if ch := UniqueFunctionFiles(c); len(ch) != 0 {
		t.Fatalf("changed %v", ch)
	}
}

func TestUniqueFunctionFilesFunctionsOnlyShareVacatesOldFile(t *testing.T) {
	c := ffContract(nil, []contract.Function{
		ffFn("fn-1", stats, "func Stats()"),
		ffFn("fn-2", stats, "func Other()"),
	})
	UniqueFunctionFiles(c)
	if got := fileOf(c); got["fn-1"] != "internal/stats/stats.go" || got["fn-2"] != "internal/stats/other.go" {
		// both get their own file; the vacated shared name is free for Stats
		t.Fatalf("files = %v", got)
	}
}

func TestUniqueFunctionFilesCollisionsGetSuffix(t *testing.T) {
	c := ffContract(
		[]contract.Type{{ID: "ty", Package: "csvstat/internal/stats", File: stats, Decl: "type T int"}},
		[]contract.Function{
			ffFn("fn-1", stats, "func Parse()"),
			ffFn("fn-2", stats, "func parse()"),
			ffFn("fn-3", stats, "func Parse2()"),
			ffFn("fn-4", "internal/stats/parse_3.go", "func Keep()"),
		})
	UniqueFunctionFiles(c)
	got := fileOf(c)
	want := map[string]string{
		"fn-1": "internal/stats/parse.go",
		"fn-2": "internal/stats/parse_2.go",
		"fn-3": "internal/stats/parse2.go",
		"fn-4": "internal/stats/parse_3.go",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %v", got)
	}
}

func TestUniqueFunctionFilesNeverTestOrStubNames(t *testing.T) {
	c := ffContract(
		[]contract.Type{{ID: "ty", Package: "csvstat/internal/stats", File: stats, Decl: "type T int"}},
		[]contract.Function{
			ffFn("fn-1", stats, "func Foo_test()"),
			ffFn("fn-2", stats, "func ZzGmThing()"),
		})
	UniqueFunctionFiles(c)
	for _, f := range c.Functions {
		if len(f.File) < 3 || f.File[len(f.File)-8:] == "_test.go" {
			t.Fatalf("test-like file %s", f.File)
		}
		base := f.File[len("internal/stats/"):]
		if len(base) >= 5 && base[:5] == "zz_gm" {
			t.Fatalf("stub-like file %s", f.File)
		}
	}
}

func TestUniqueFunctionFilesDifferentPackagesKeepTheirOwn(t *testing.T) {
	c := ffContract(
		[]contract.Type{{ID: "ty", Package: "csvstat/a", File: "a/types.go", Decl: "type T int"}},
		[]contract.Function{
			{ID: "fn-1", Package: "csvstat/b", File: "b/types.go", Signature: "func One()"},
		})
	if ch := UniqueFunctionFiles(c); len(ch) != 0 {
		t.Fatalf("changed %v", ch)
	}
}

func TestUniqueFunctionFilesDoc(t *testing.T) {
	doc := map[string]any{
		"module": "csvstat",
		"types":  []any{map[string]any{"id": "ty", "package": "csvstat/internal/stats", "file": stats}},
		"functions": []any{
			map[string]any{"id": "fn-a", "package": "csvstat/internal/stats", "file": stats, "signature": "func A()"},
			map[string]any{"id": "fn-b", "package": "csvstat/internal/stats", "file": stats, "signature": "func (r *Rec) B()"},
		},
	}
	ids := uniqueFunctionFilesDoc(doc)
	if !reflect.DeepEqual(ids, []string{"fn-a", "fn-b"}) {
		t.Fatalf("ids = %v", ids)
	}
	fns := objects(doc["functions"])
	if fns[0]["file"] != "internal/stats/a.go" || fns[1]["file"] != "internal/stats/rec_b.go" {
		t.Fatalf("files = %v %v", fns[0]["file"], fns[1]["file"])
	}
}

func TestSnakeName(t *testing.T) {
	for in, want := range map[string]string{
		"finalizeColumnState": "finalize_column_state", "ParseCSV": "parse_csv", "ParseCSVRow": "parse_csv_row",
		"A": "a", "http2Get": "http2_get", "already_snake": "already_snake",
	} {
		if got := snakeName(in); got != want {
			t.Errorf("snakeName(%q) = %q, want %q", in, got, want)
		}
	}
}
