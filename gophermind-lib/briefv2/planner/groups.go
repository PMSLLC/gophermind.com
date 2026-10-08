package planner

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Group defect kinds, a fixed vocabulary like the Decompose defect kinds: they
// say what is wrong with a node's groups without quoting the reply.
const (
	grpMissing   = "group_missing"
	grpNAForbid  = "na_forbidden"
	grpNAReason  = "na_reason"
	grpEnum      = "enum"
	grpConstruct = "construction_short"
	grpTypeRes   = "go_type_unresolved"
	grpSecret    = "security_secret"
	grpThreats   = "security_threats"
	grpGoMin     = "portability_go"
	grpDeps      = "portability_deps"
	grpObsFields = "observability_fields"
	grpDecision  = "decision_unknown"
	grpOpenQ     = "open_questions_left"
	grpHookBench = "hook_bench"
	grpErrTest   = "error_test_missing"
	grpPolarity  = "polarity_missing"
	grpSchema    = "group_schema"
)

// groupDefect is one thing wrong with a node's groups. field names the group
// to ask for again.
type groupDefect struct{ kind, field, msg string }

// groupEnv is what the group checks need besides the node.
type groupEnv struct {
	Secrets   map[string]bool // declared secret names of the brief
	Decisions map[string]bool // ids of settled questions; nil skips the decision check
	Params    map[string]bool // parameter names of the function's signature
	GoMin     string          // the Go version in the target repo's go.mod; "" when unknown
	DepMods   []string        // modules a plan may import beyond the standard library; nil skips the check
}

// naGroups are the groups that may be answered "not applicable" (where
// naAllowed permits it for the node's class).
var naGroups = []string{"alternatives", "portability", "security", "performance", "observability", "refactor_notes", "profile_hooks"}

// naAllowed says where a group may be answered not applicable. Security is a
// real question for anything that handles input, calls out, stores or runs
// concurrently; performance and observability may be dismissed only for a pure
// function.
func naAllowed(group, class string) bool {
	switch group {
	case "alternatives", "portability", "refactor_notes", "profile_hooks":
		return true
	case "security":
		switch class {
		case "handler", "client", "storage", "concurrency":
			return false
		}
		return true
	case "performance", "observability":
		return class == "pure"
	}
	return false
}

// notApplicable reports whether v is a not-applicable answer and its reason.
func notApplicable(v any) (reason string, ok bool) {
	m, isMap := v.(map[string]any)
	if !isMap {
		return "", false
	}
	r, has := m["not_applicable"]
	if !has {
		return "", false
	}
	s, _ := r.(string)
	return s, true
}

// naReasonOK is true when a not-applicable reason is a sentence and not a
// non-answer: at least 20 characters and 4 words, and at least 3 words are
// left after the stock non-answers (none, n/a, no, nothing, not applicable,
// pure function) are removed.
func naReasonOK(s string) bool {
	t := strings.ToLower(strings.TrimSpace(s))
	if len([]rune(t)) < 20 || len(strings.Fields(t)) < 4 {
		return false
	}
	words := strings.Fields(t)
	clean := func(w string) string { return strings.Trim(w, ".,;:!?") }
	kept := 0
	for i := 0; i < len(words); i++ {
		w := clean(words[i])
		switch w {
		case "n/a", "na", "none", "nothing", "no":
			continue
		case "not":
			if i+1 < len(words) && clean(words[i+1]) == "applicable" {
				i++
				continue
			}
		case "pure":
			if i+1 < len(words) && clean(words[i+1]) == "function" {
				i++
				continue
			}
		}
		kept++
	}
	return kept >= 3
}

var logVerbRE = regexp.MustCompile(`%[-+# 0-9.*]*[a-zA-Z]`)

func strField(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

// groupDefects lists every group check that fails for a function node, in a
// stable order. d is the merged draft (Decompose fields plus Enrich groups).
func groupDefects(d map[string]any, class string, env groupEnv) []groupDefect {
	var out []groupDefect
	add := func(kind, field, format string, a ...any) {
		out = append(out, groupDefect{kind, field, fmt.Sprintf(format, a...)})
	}
	for _, g := range []string{"rationale", "construction", "assumptions", "open_questions", "decision_ids"} {
		if _, ok := d[g]; !ok {
			add(grpMissing, g, "%s is missing", g)
		}
	}
	if s, ok := d["rationale"].(string); ok && len([]rune(strings.TrimSpace(s))) < 20 {
		add(grpMissing, "rationale", "rationale is shorter than 20 characters")
	}
	if c, ok := d["construction"].(map[string]any); ok {
		steps := strList(c["steps"])
		blank := strings.TrimSpace(strField(c, "approach_chosen")) == ""
		for _, s := range steps {
			if strings.TrimSpace(s) == "" {
				blank = true
			}
		}
		if len(steps) < 2 || blank {
			add(grpConstruct, "construction", "construction needs an approach and at least 2 non-empty steps")
		}
	}
	real := map[string]map[string]any{}
	for _, g := range naGroups {
		v, ok := d[g]
		if !ok {
			add(grpMissing, g, "%s is missing", g)
			continue
		}
		if reason, na := notApplicable(v); na {
			if !naAllowed(g, class) {
				add(grpNAForbid, g, "%s may not be not applicable for a %s node", g, class)
				continue
			}
			if !naReasonOK(reason) {
				add(grpNAReason, g, "the not_applicable reason of %s is not a real reason", g)
			}
			continue
		}
		if m, ok := v.(map[string]any); ok {
			real[g] = m
		}
	}

	if sec := real["security"]; sec != nil {
		for i, n := range strList(sec["untrusted_inputs"]) {
			if !env.Params[n] {
				add(grpSecret, "security", "untrusted_inputs entry %d is not a parameter of the function", i+1)
			}
		}
		for i, n := range strList(sec["secret_use"]) {
			if !env.Secrets[n] {
				add(grpSecret, "security", "secret_use entry %d is not a declared secret", i+1)
			}
		}
		if strField(sec, "trust_boundary") != "none" && len(objects(sec["threats"])) == 0 {
			add(grpThreats, "security", "a trust boundary other than none needs at least one threat with its mitigation")
		}
	}
	if p := real["portability"]; p != nil {
		if env.GoMin != "" && goVersionLess(env.GoMin, strField(p, "go_min")) {
			add(grpGoMin, "portability", "go_min is newer than the Go version in the repository's go.mod")
		}
		if env.DepMods != nil {
			for i, dep := range strList(p["deps"]) {
				if !depAllowed(dep, env.DepMods) {
					add(grpDeps, "portability", "deps entry %d is not the standard library, this module or a planned dependency", i+1)
				}
			}
		}
	}
	if o := real["observability"]; o != nil {
		for i, ev := range objects(o["log_events"]) {
			if logVerbRE.MatchString(strField(ev, "msg")) {
				add(grpObsFields, "observability", "log event %d has a format verb in its message; use a constant message and fields", i+1)
			}
			for _, f := range strList(ev["fields"]) {
				if secretLike(f, env.Secrets) {
					add(grpObsFields, "observability", "log event %d lists a field named like a declared secret", i+1)
				}
			}
		}
	}
	if env.Decisions != nil {
		for i, id := range strList(d["decision_ids"]) {
			if !env.Decisions[id] {
				add(grpDecision, "decision_ids", "decision_ids entry %d is not a settled question", i+1)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].field != out[j].field {
			return out[i].field < out[j].field
		}
		return out[i].kind < out[j].kind
	})
	return out
}

// openQuestionDefects is the check made at approval: no node may still list an
// open question.
func openQuestionDefects(d map[string]any) []groupDefect {
	if n := len(strList(d["open_questions"])); n > 0 {
		return []groupDefect{{grpOpenQ, "open_questions", fmt.Sprintf("%d open question(s) are still listed", n)}}
	}
	return nil
}

func secretLike(field string, secrets map[string]bool) bool {
	for name := range secrets {
		if strings.EqualFold(field, name) {
			return true
		}
	}
	return false
}

func depAllowed(dep string, mods []string) bool {
	first, _, _ := strings.Cut(dep, "/")
	if stdTopLevel[first] && !strings.Contains(first, ".") {
		return true
	}
	for _, m := range mods {
		if dep == m || strings.HasPrefix(dep, m+"/") {
			return true
		}
	}
	return false
}

func goVersionParts(s string) [3]int {
	var v [3]int
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(s), "go"), ".")
	for i := 0; i < 3 && i < len(parts); i++ {
		v[i], _ = strconv.Atoi(parts[i])
	}
	return v
}

// goVersionLess reports whether Go version a is older than b.
func goVersionLess(a, b string) bool {
	x, y := goVersionParts(a), goVersionParts(b)
	for i := range x {
		if x[i] != y[i] {
			return x[i] < y[i]
		}
	}
	return false
}
