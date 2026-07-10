package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"gophermind/internal/agent"
)

func TestRenderJSONResultSuccess(t *testing.T) {
	var b strings.Builder
	u := agent.UsageSnapshot{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15, CostUSD: 0.02}
	if err := renderJSONResult(&b, "the answer", u, "qwen", nil); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(b.String()), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, b.String())
	}
	if got["result"] != "the answer" || got["is_error"] != false {
		t.Errorf("result/is_error wrong: %v", got)
	}
	if got["model"] != "qwen" {
		t.Errorf("model = %v", got["model"])
	}
	if got["input_tokens"].(float64) != 10 || got["output_tokens"].(float64) != 5 {
		t.Errorf("token fields wrong: %v", got)
	}
	if got["total_cost_usd"].(float64) != 0.02 {
		t.Errorf("cost = %v", got["total_cost_usd"])
	}
	if _, ok := got["error"]; ok {
		t.Errorf("error key should be absent on success: %v", got)
	}
}

func TestRenderJSONResultError(t *testing.T) {
	var b strings.Builder
	if err := renderJSONResult(&b, "", agent.UsageSnapshot{}, "m", errors.New("boom")); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(b.String()), &got); err != nil {
		t.Fatal(err)
	}
	if got["is_error"] != true || got["error"] != "boom" {
		t.Errorf("error fields wrong: %v", got)
	}
}
