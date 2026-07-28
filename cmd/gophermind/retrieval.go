package main

import (
	"context"
	"strings"

	"gophermind/internal/agent"
	"gophermind/internal/embed"
)

// retrievalPaths bundles the stores retrieval reads, so every entry point (the
// one-shot run/ask arm and each serve turn) injects from exactly the same set.
type retrievalPaths struct {
	index    string
	memory   string
	profile  string
	episodes string
}

// retrievalBlocks returns the context blocks to inject for task: semantic index
// hits when GOPHERMIND_RAG is enabled, and remembered facts (per-repo, global
// profile, episodic) when GOPHERMIND_MEMORY is. It returns "" when both are
// disabled, embeddings are unconfigured, or nothing relevant was found — so an
// unconfigured install behaves exactly as before.
func retrievalBlocks(ctx context.Context, p embed.Provider, paths retrievalPaths, task string) string {
	if p == nil {
		return ""
	}
	var b strings.Builder
	if envTruthy("GOPHERMIND_RAG") {
		appendBlock(&b, retrieveContext(ctx, p, paths.index, task, 5))
	}
	if envTruthy("GOPHERMIND_MEMORY") {
		if mc := retrieveContext(ctx, p, paths.memory, task, 5); mc != "" {
			appendBlock(&b, strings.Replace(mc, "retrieved_context", "long_term_memory", 2))
		}
		if pc := retrieveContext(ctx, p, paths.profile, task, 3); pc != "" {
			appendBlock(&b, strings.Replace(pc, "retrieved_context", "profile_memory", 2))
		}
		if ec := retrieveContext(ctx, p, paths.episodes, task, 3); ec != "" {
			appendBlock(&b, strings.Replace(ec, "retrieved_context", "episodic_memory", 2))
		}
	}
	return b.String()
}

// appendBlock adds a non-empty block, separating blocks the way the system
// prompt itself is assembled.
func appendBlock(b *strings.Builder, s string) {
	if s == "" {
		return
	}
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	b.WriteString(s)
}

// injectRetrieval appends the retrieval blocks for task to ag's system prompt
// and returns a func restoring the prompt to what it was.
//
// The restore matters for persisted sessions: AppendSystemPrompt mutates the
// system message in place and session.Save writes the whole history, so without
// it every turn would bake its own retrieved context into the stored session and
// the prompt would grow without bound. Callers that persist must restore before
// saving. The returned func is always safe to call.
func injectRetrieval(ctx context.Context, ag *agent.Agent, p embed.Provider, paths retrievalPaths, task string) func() {
	noop := func() {}
	prev := ag.SystemPrompt()
	if prev == "" {
		// No system message to attach to. Injecting would be a no-op anyway, and
		// restoring an empty prompt would reset it to the built-in default.
		return noop
	}
	blocks := retrievalBlocks(ctx, p, paths, task)
	if blocks == "" {
		return noop
	}
	ag.AppendSystemPrompt(blocks)
	return func() { ag.SetSystemPrompt(prev) }
}
