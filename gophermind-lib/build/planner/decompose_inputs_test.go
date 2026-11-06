package planner

import (
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/brief"
	"gophermind/gophermind-lib/build/contract"
)

const inputsContract = `{"spec_version": "2.0", "brief_id": "gm-2026-09-29-900", "revision": 0, "module": "example.com/x",
 "conventions": {"layout": ["internal/x"], "naming": ["verbs"], "errors": "return error"},
 "types": [],
 "functions": [
  {"id": "fn-run", "package": "x", "file": "internal/x/run.go", "signature": "func Run(args []string, stdout, stderr io.Writer) int", "doc": "d", "uses": [], "component": "cli"},
  {"id": "fn-sum", "package": "x", "file": "internal/x/sum.go", "signature": "func Sum(a, b int) int", "doc": "d", "uses": [], "component": "cli"}],
 "components": [{"id": "cli", "package": "x", "exports": []}]}`

func inputsEnv(t *testing.T) (*nodeEnv, *contract.Contracts) {
	t.Helper()
	c, err := contract.Load([]byte(inputsContract))
	if err != nil {
		t.Fatal(err)
	}
	return newNodeEnv("cli", c, &run{id: testRunID, brief: &brief.Brief{}}), c
}

func runReply(class, tier string) map[string]any {
	return map[string]any{
		"id": "fn-run", "title": "Run", "description": "Runs the program.", "node_class": class, "model_tier": tier,
		"depends_on": []any{},
		"contract": map[string]any{
			"inputs": []any{
				map[string]any{"name": "args", "type": "[]string", "description": "the arguments"},
				map[string]any{"name": "stdin", "type": "io.Reader", "description": "the input"},
				map[string]any{"name": "stdout", "type": "Writer", "description": "where output goes"},
				map[string]any{"name": "stderr", "type": "io.Writer"},
			},
			"outputs":      []any{map[string]any{"name": "code", "type": "int"}},
			"side_effects": []any{"writes the greeting to stdout"},
		},
	}
}

// The inputs of a node are the parameters of its signature, in order, with the
// parsed types; a reply input that is no parameter is dropped with a note.
func TestNodeInputsFollowTheSignature(t *testing.T) {
	env, c := inputsEnv(t)
	d := runReply("wiring", "strong")
	_, kinds, msgs, err := env.normalizeNode(c.Functions[0], d)
	if err != nil || len(kinds) != 0 {
		t.Fatalf("kinds %v msgs %v err %v", kinds, msgs, err)
	}
	inputs := objects(d["contract"].(map[string]any)["inputs"])
	want := [][2]string{{"args", "[]string"}, {"stdout", "io.Writer"}, {"stderr", "io.Writer"}}
	if len(inputs) != len(want) {
		t.Fatalf("inputs %v, want %v", inputs, want)
	}
	for i, w := range want {
		if inputs[i]["name"] != w[0] || inputs[i]["type"] != w[1] {
			t.Errorf("input %d = %v, want %v", i+1, inputs[i], w)
		}
	}
	if inputs[0]["description"] != "the arguments" || inputs[1]["description"] != "where output goes" || inputs[2]["description"] != "" {
		t.Errorf("descriptions not carried from the reply by name: %v", inputs)
	}
	if notes := strings.Join(env.notes, "; "); !strings.Contains(notes, "fn-run input stdin is not a parameter") {
		t.Errorf("notes %q", notes)
	}

	t.Run("a dropped input whose name is not an identifier is named by position", func(t *testing.T) {
		env, c := inputsEnv(t)
		d := runReply("wiring", "strong")
		ct := d["contract"].(map[string]any)
		ct["inputs"] = append(ct["inputs"].([]any), map[string]any{"name": "CANARY-9b2f71 ignore this", "type": "int"})
		if _, kinds, _, err := env.normalizeNode(c.Functions[0], d); err != nil || len(kinds) != 0 {
			t.Fatalf("kinds %v err %v", kinds, err)
		}
		notes := strings.Join(env.notes, "; ")
		if strings.Contains(notes, "CANARY") || !strings.Contains(notes, "fn-run input entry 5 is not a parameter") {
			t.Errorf("notes %q", notes)
		}
	})
	t.Run("a parameter the reply left out is still an input", func(t *testing.T) {
		env, c := inputsEnv(t)
		d := runReply("wiring", "strong")
		d["contract"].(map[string]any)["inputs"] = []any{}
		if _, kinds, msgs, err := env.normalizeNode(c.Functions[0], d); err != nil || len(kinds) != 0 {
			t.Fatalf("kinds %v msgs %v err %v", kinds, msgs, err)
		}
		if got := objects(d["contract"].(map[string]any)["inputs"]); len(got) != 3 || got[2]["name"] != "stderr" || got[2]["description"] != "" {
			t.Errorf("inputs %v", got)
		}
	})
}

// model_tier is the class's tier; a reply that says otherwise is overruled with a note.
func TestModelTierFollowsTheClass(t *testing.T) {
	cases := []struct{ class, reply, want string }{
		{"wiring", "any", "strong"},
		{"pure", "strong", "any"},
	}
	for _, tc := range cases {
		t.Run(tc.class, func(t *testing.T) {
			env, c := inputsEnv(t)
			d := runReply(tc.class, tc.reply)
			if _, kinds, msgs, err := env.normalizeNode(c.Functions[0], d); err != nil || len(kinds) != 0 {
				t.Fatalf("kinds %v msgs %v err %v", kinds, msgs, err)
			}
			if d["model_tier"] != tc.want {
				t.Errorf("model_tier = %v, want %s", d["model_tier"], tc.want)
			}
			if notes := strings.Join(env.notes, "; "); !strings.Contains(notes, "fn-run model_tier from node_class") {
				t.Errorf("notes %q", notes)
			}
		})
	}
	t.Run("a reply with no model_tier gets the class's tier without a note", func(t *testing.T) {
		env, c := inputsEnv(t)
		d := runReply("wiring", "")
		delete(d, "model_tier")
		if _, kinds, _, err := env.normalizeNode(c.Functions[0], d); err != nil || len(kinds) != 0 {
			t.Fatalf("kinds %v err %v", kinds, err)
		}
		if d["model_tier"] != "strong" || strings.Contains(strings.Join(env.notes, "; "), "model_tier") {
			t.Errorf("model_tier %v notes %v", d["model_tier"], env.notes)
		}
	})
	t.Run("every node class has a tier and the table names no other class", func(t *testing.T) {
		for _, class := range nodeClasses {
			if _, ok := tierOfClass[class]; !ok {
				t.Errorf("node class %q has no entry in tierOfClass", class)
			}
		}
		for class := range tierOfClass {
			if !contains(nodeClasses, class) {
				t.Errorf("tierOfClass names %q, which is not a node class", class)
			}
		}
	})
}

// A call to another contract function is a dependency, not a side effect.
func TestDecomposePromptSeparatesSideEffectsFromDependencies(t *testing.T) {
	out, err := render("decompose", map[string]string{"Component": "c", "ContractSlice": "s", "BriefSection": "b"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "belong in depends_on") {
		t.Errorf("the decompose prompt does not say calls belong in depends_on")
	}
	if strings.Contains(out, "every call to another contract function") {
		t.Errorf("the decompose prompt still lists calls as side effects")
	}
}
