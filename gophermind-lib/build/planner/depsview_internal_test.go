package planner

import "testing"

func depsView(sigs ...any) map[string]any {
	return map[string]any{"id": "fn-run", "context": map[string]any{"dependency_signatures": sigs, "constraints": []any{"c"}}}
}

const linkDecl = "// Link is one URL.\ntype Link struct {\n\tURL string\n}"
const parseFlagsSig = "// Parses flags.\nfunc parseFlags(args []string) (bool, string, error)"
const allSig = "// All checks links.\nfunc All(links []Link) []Result"

func TestWithoutUnexportedDepsDropsHelpersForAnExportedTarget(t *testing.T) {
	in := depsView(linkDecl, parseFlagsSig, allSig)
	out := withoutUnexportedDeps(in, "func Run(args []string) int", "cli")
	got := out["context"].(map[string]any)["dependency_signatures"].([]any)
	if len(got) != 2 || got[0] != linkDecl || got[1] != allSig {
		t.Errorf("dependency_signatures = %v", got)
	}
	if n := len(in["context"].(map[string]any)["dependency_signatures"].([]any)); n != 3 {
		t.Errorf("input was mutated: %d entries", n)
	}
	if out["context"].(map[string]any)["constraints"] == nil {
		t.Error("constraints were dropped")
	}
}

func TestWithoutUnexportedDepsKeepsThemForAnUnexportedTargetAndMain(t *testing.T) {
	for _, c := range []struct{ sig, dir string }{{"func run(args []string) int", "cli"}, {"func Run(args []string) int", "cmd/linkcheck"}} {
		out := withoutUnexportedDeps(depsView(linkDecl, parseFlagsSig), c.sig, c.dir)
		if got := out["context"].(map[string]any)["dependency_signatures"].([]any); len(got) != 2 {
			t.Errorf("%s in %s: dependency_signatures = %v, want both kept", c.sig, c.dir, got)
		}
	}
}

func TestWithoutUnexportedDepsKeepsWhatItCannotParse(t *testing.T) {
	out := withoutUnexportedDeps(depsView("not go at all"), "func Run() int", "cli")
	if got := out["context"].(map[string]any)["dependency_signatures"].([]any); len(got) != 1 {
		t.Errorf("dependency_signatures = %v", got)
	}
}
