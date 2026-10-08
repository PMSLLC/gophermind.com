package planner_test

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"

	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/router"
)

// enrichNode is a node as the Enrich prompt shows it.
type enrichNode struct {
	ID       string `json:"id"`
	Class    string `json:"node_class"`
	Contract struct {
		Errors []any `json:"errors"`
	} `json:"contract"`
}

var nodesRE = regexp.MustCompile(`(?s)<nodes>\n(.*?)\n</nodes>`)

// enrichCalls records every Enrich request.
type enrichCalls struct {
	mu      sync.Mutex
	stages  []string
	prompts []string
}

func (c *enrichCalls) count(stage string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, s := range c.stages {
		if s == stage {
			n++
		}
	}
	return n
}

// goodEnrichment is a valid answer for a node of the given class.
func goodEnrichment(n enrichNode) map[string]any {
	kinds := make([]any, len(n.Contract.Errors))
	for i := range kinds {
		kinds[i] = "sentinel"
	}
	m := map[string]any{
		"id":             n.ID,
		"rationale":      "Serves the " + n.ID + " part of the brief so callers get the behaviour the contract promises.",
		"construction":   map[string]any{"approach_chosen": "write it directly", "steps": []any{"Check the input.", "Build and return the result."}},
		"alternatives":   map[string]any{"not_applicable": "there is one obvious way to write a function this small"},
		"error_kinds":    kinds,
		"portability":    map[string]any{"os": []any{"linux", "darwin"}, "arch": []any{"amd64", "arm64"}, "go_min": "1.21", "cgo": false, "deps": []any{}},
		"security":       map[string]any{"trust_boundary": "none", "untrusted_inputs": []any{}, "authz": "none", "secret_use": []any{}, "threats": []any{}},
		"performance":    map[string]any{"complexity": "O(1)", "max_latency_ms": nil, "alloc_budget": "none", "concurrency": "none", "hot_path": false},
		"observability":  map[string]any{"log_events": []any{}, "metrics": []any{}, "trace_span": nil},
		"refactor_notes": map[string]any{"not_applicable": "nothing worth restructuring in a function this small"},
		"profile_hooks":  map[string]any{"not_applicable": "this function is too small to profile usefully"},
		"assumptions":    []any{}, "open_questions": []any{}, "decision_ids": []any{},
	}
	return m
}

func goodEnrichmentJSON(nodes []enrichNode) string {
	out := make([]map[string]any, len(nodes))
	for i, n := range nodes {
		out[i] = goodEnrichment(n)
	}
	b, _ := json.Marshal(out)
	return string(b)
}

const goodStructureJSON = `{"rationale":"Groups the functions that implement one part of the brief so they can be built and tested together.","assumptions":[],"open_questions":[],"decision_ids":[]}`

// enrichResponder answers one Enrich function-batch request: the stage, the
// 1-based number of that stage's call, and the nodes in the prompt.
type enrichResponder func(stage string, call int, nodes []enrichNode, prompt string) string

// withEnrichment puts a provider in front of the rig's fixture provider that
// answers the Enrich stages itself and passes every other stage through.
func withEnrichment(g *rig, respond enrichResponder) *enrichCalls {
	return withEnrichmentErr(g, func(stage string, call int, nodes []enrichNode, prompt string) (string, error) {
		return respond(stage, call, nodes, prompt), nil
	})
}

// enrichResponderErr is an enrichResponder that may fail the provider call.
type enrichResponderErr func(stage string, call int, nodes []enrichNode, prompt string) (string, error)

// withEnrichmentErr is withEnrichment for a responder that can return an error,
// for example provider.ErrTruncated.
func withEnrichmentErr(g *rig, respond enrichResponderErr, structure ...func(stage, prompt string) string) *enrichCalls {
	calls := &enrichCalls{}
	base := g.fake
	var mu sync.Mutex
	seen := map[string]int{}
	wrapped := provider.NewFake("fake", base.Models(), func(_ int, req provider.Request) (provider.Response, error) {
		stage := planner.StageOf(req)
		if !strings.HasPrefix(stage, "enrich") {
			return base.Complete(context.Background(), req)
		}
		prompt := ""
		for _, m := range req.Messages {
			if m.Role == provider.RoleUser {
				prompt = m.Content
			}
		}
		calls.mu.Lock()
		calls.stages = append(calls.stages, stage)
		calls.prompts = append(calls.prompts, prompt)
		calls.mu.Unlock()
		mu.Lock()
		seen[stage]++
		n := seen[stage]
		mu.Unlock()
		if stage == "enrich_root" || strings.HasPrefix(stage, "enrich_comp:") {
			text := goodStructureJSON
			if len(structure) > 0 {
				text = structure[0](stage, prompt)
			}
			return provider.Response{Text: text, Model: "fixture", Usage: provider.Usage{PromptTokens: 1, CompletionTokens: 1}}, nil
		}
		var nodes []enrichNode
		if m := nodesRE.FindStringSubmatch(prompt); m != nil {
			_ = json.Unmarshal([]byte(m[1]), &nodes)
		}
		text, err := respond(stage, n, nodes, prompt)
		if err != nil {
			return provider.Response{}, err
		}
		return provider.Response{Text: text, Model: "fixture", Usage: provider.Usage{PromptTokens: 1, CompletionTokens: 1}}, nil
	})
	g.router = router.New(g.cfg, map[string]provider.Provider{"fake": wrapped}, g.led, g.sink)
	g.deps.Caller = g.router
	return calls
}

func allGood(_ string, _ int, nodes []enrichNode, _ string) string { return goodEnrichmentJSON(nodes) }

// dropGroup returns a responder that drops one group of one node (from its
// first answer only, or from every answer when always is true) and answers
// everything else correctly.
func dropGroup(id, group string, always bool) enrichResponder {
	return func(stage string, call int, nodes []enrichNode, _ string) string {
		out := make([]map[string]any, len(nodes))
		for i, n := range nodes {
			out[i] = goodEnrichment(n)
			if n.ID == id && (always || (stage != "enrich:_fix" && call == 1)) {
				delete(out[i], group)
			}
		}
		b, _ := json.Marshal(out)
		return string(b)
	}
}
