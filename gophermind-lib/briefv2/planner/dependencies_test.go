package planner_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
)

const outlineHead = `{"module": "example.com/x",
 "conventions": {"layout": ["internal/x"], "naming": ["verbs"], "errors": "return error"},
 "components": [{"id": "types", "package": "x"}],
 "types": [{"id": "name-error", "package": "x", "file": "internal/x/errors.go", "decl": "type NameError struct{}"}]`

func TestParseOutlineDependencies(t *testing.T) {
	two := outlineHead + `, "dependencies": [
 {"module": "github.com/z/zed", "version": "v1.9.3", "purpose": "zed things"},
 {"module": "github.com/a/alpha", "version": "v0.1.0-rc.1", "purpose": "alpha things"}]}`
	_, deps, err := planner.ParseOutline(two, "gm-2026-09-29-900")
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 2 || deps[0].Module != "github.com/a/alpha" || deps[1].Module != "github.com/z/zed" || deps[1].Version != "v1.9.3" {
		t.Errorf("deps = %+v, want two sorted by module", deps)
	}

	_, deps, err = planner.ParseOutline(outlineHead+`}`, "gm-2026-09-29-900")
	if err != nil || deps == nil || len(deps) != 0 {
		t.Errorf("no dependencies key: deps = %#v, err = %v; want []", deps, err)
	}

	bad := outlineHead + `, "dependencies": [{"module": "fmt", "version": "v1.0.0", "purpose": "x"}]}`
	if _, _, err := planner.ParseOutline(bad, "gm-2026-09-29-900"); err == nil {
		t.Error("an outline with a bad dependency parsed")
	}

	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "contract"})
	var got []planner.Dependency
	if err := json.Unmarshal(g.read("dependencies.json"), &got); err != nil || got == nil || len(got) != 0 {
		t.Errorf("dependencies.json = %q, %v; want []", g.read("dependencies.json"), err)
	}
	if fi, err := os.Stat(filepath.Join(g.runDir, "dependencies.json")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("dependencies.json mode = %v, %v; want 0600", fi, err)
	}
}

func TestDependenciesRejectLatestAndRange(t *testing.T) {
	const canary = "CANARY-mod"
	good := func(m map[string]any) map[string]any {
		out := map[string]any{"module": "github.com/x/y", "version": "v1.2.3", "purpose": "does a thing"}
		for k, v := range m {
			out[k] = v
		}
		return out
	}
	cases := []struct {
		name string
		in   []map[string]any
	}{
		{"latest", []map[string]any{good(map[string]any{"version": "latest"})}},
		{"master", []map[string]any{good(map[string]any{"version": "master"})}},
		{"v1", []map[string]any{good(map[string]any{"version": "v1"})}},
		{"v1.2", []map[string]any{good(map[string]any{"version": "v1.2"})}},
		{"range", []map[string]any{good(map[string]any{"version": ">=v1.0.0"})}},
		{"no v", []map[string]any{good(map[string]any{"version": "1.2.3"})}},
		{"stdlib module", []map[string]any{good(map[string]any{"module": "fmt"})}},
		{"internal module", []map[string]any{good(map[string]any{"module": "internal/x"})}},
		{"empty purpose", []map[string]any{good(map[string]any{"purpose": ""})}},
		{"duplicate", []map[string]any{good(nil), good(nil)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := planner.ParseDependencies(c.in); err == nil {
				t.Fatal("accepted")
			}
		})
		// The same case with a canary in the field that is refused.
		t.Run(c.name+" canary", func(t *testing.T) {
			in := make([]map[string]any, len(c.in))
			for i, m := range c.in {
				in[i] = map[string]any{}
				for k, v := range m {
					s, _ := v.(string)
					switch {
					case c.name == "duplicate":
						in[i][k] = v
					case s != "" && s != "v1.2.3" && s != "github.com/x/y" && s != "does a thing":
						in[i][k] = s + canary
					default:
						in[i][k] = v
					}
				}
			}
			if c.name == "duplicate" {
				in[0]["module"], in[1]["module"] = "github.com/x/"+canary, "github.com/x/"+canary
			}
			if c.name == "empty purpose" {
				in[0]["module"] = "github.com/x/" + canary
			}
			_, err := planner.ParseDependencies(in)
			if err == nil {
				t.Skip("canary variant is valid")
			}
			if strings.Contains(err.Error(), canary) || strings.Contains(err.Error(), "github.com/x") {
				t.Errorf("error quotes the input: %v", err)
			}
		})
	}
}

func TestApprovalHashCoversDependencies(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "approve"})
	if err := planner.VerifyApproval(g.runDir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(g.runDir, "dependencies.json")
	orig := g.read("dependencies.json")
	changed := append(append([]byte{}, orig...), ' ')
	if err := os.WriteFile(path, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := planner.VerifyApproval(g.runDir); err == nil {
		t.Error("VerifyApproval passed after dependencies.json changed by one byte")
	}
	if err := os.WriteFile(path, orig, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := planner.VerifyApproval(g.runDir); err != nil {
		t.Errorf("VerifyApproval after restore: %v", err)
	}
}

func TestRenderPlanListsDependencies(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "contract"})
	// coverage is needed by RenderPlan: plan through approval.
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "approve"})
	md, _, err := planner.RenderPlan(g.runDir)
	if err != nil || !strings.Contains(md, "## Dependencies\n\nnone\n") {
		t.Errorf("empty list: %v\n%s", err, md)
	}
	body := `[{"module": "github.com/x/y", "version": "v1.2.3", "purpose": "purpose"}]`
	if err := os.WriteFile(filepath.Join(g.runDir, "dependencies.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	md, _, err = planner.RenderPlan(g.runDir)
	if err != nil || !strings.Contains(md, "## Dependencies") || !strings.Contains(md, "github.com/x/y@v1.2.3: purpose") {
		t.Errorf("listed: %v\n%s", err, md)
	}
	if strings.Index(md, "## Dependencies") > strings.Index(md, "## Components") {
		t.Error("Dependencies comes after Components")
	}
}
