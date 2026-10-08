package projectrun

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// keyPaths lists every key path of a decoded JSON value; array elements are
// "[]" and the keys of by_answered_by are data, shown as "*".
func keyPaths(prefix string, v any, out map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		for k, c := range x {
			if strings.HasSuffix(prefix, "by_answered_by") {
				k = "*"
			}
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			out[p] = true
			keyPaths(p, c, out)
		}
	case []any:
		for _, c := range x {
			keyPaths(prefix+"[]", c, out)
		}
	}
}

func TestProjectJSONKeySetIsGolden(t *testing.T) {
	raw, err := json.Marshal(sampleReport())
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	set := map[string]bool{}
	keyPaths("", v, set)
	var got []string
	for k := range set {
		got = append(got, k)
	}
	sort.Strings(got)
	if g := strings.Join(got, "\n"); g != strings.TrimSpace(goldenKeys) {
		t.Fatalf("project.json key set changed:\n%s", g)
	}
}

const goldenKeys = `
ambiguity
ambiguity.brief_setting
ambiguity.by_answered_by
ambiguity.by_answered_by.*
ambiguity.clarify_calls
ambiguity.clarify_defaulted
ambiguity.clarify_defaulted[].answer
ambiguity.clarify_defaulted[].id
ambiguity.clarify_defaulted[].question
ambiguity.conservative_assumptions
ambiguity.effective
ambiguity.milestone_approvals
ambiguity.rounds
approval
approval.by
approval.plan_hash
approval.understanding_hash
binary
binary.commit
binary.date
binary.path
binary.version
by_node_class
by_node_class[].attempts
by_node_class[].calls
by_node_class[].class
by_node_class[].completion_tokens
by_node_class[].escalated
by_node_class[].failed
by_node_class[].first_pass_rate
by_node_class[].first_try_wins
by_node_class[].leaves
by_node_class[].not_run
by_node_class[].passes
by_node_class[].prompt_tokens
by_node_class[].verified
executor
executor.acceptance
executor.acceptance.passed
executor.acceptance.total
executor.by_task_type
executor.constraints_checked
executor.constraints_checked.passed
executor.constraints_checked.total
executor.exit_code
executor.finished_at
executor.leaves
executor.nodes
executor.nodes.blocked
executor.nodes.escalated
executor.nodes.failed
executor.nodes.total
executor.nodes.verified
executor.repairs
executor.requirements_covered
executor.requirements_covered.covered
executor.requirements_covered.total
executor.resumed
executor.run_id
executor.sandbox
executor.schema_version
executor.started_at
executor.status
executor.stop_reason
executor.waves
executor.weak_tests
exit_code
finished_at
graded
mode
plan
plan.acceptance_total
plan.functions
plan.requirements_covered
plan.requirements_covered.covered
plan.requirements_covered.total
plan.warnings
plan.waves
planner_warning_lines
planner_warnings
planner_warnings.doc_defaulted
planner_warnings.duplicates_ignored
planner_warnings.leaf_defaulted
planner_warnings.leaf_normalized
planner_warnings.outline_id_normalized
preflight
preflight[].detail
preflight[].fix
preflight[].name
preflight[].ok
providers
providers[].fallback
providers[].host
providers[].name
repo
repo.base_branch
repo.brief_repo
repo.head_at_start
repo.path
resumed
run_dir
run_id
secrets
secrets[].name
secrets[].source
stages
stages[].name
stages[].status
started_at
status
stop_reason
title
understanding
understanding.confirmed_by
understanding.hash

`
