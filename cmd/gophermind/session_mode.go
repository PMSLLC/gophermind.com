package main

import (
	"os"
	"path/filepath"
	"strings"

	"gophermind/internal/persona"
	"gophermind/internal/session"
)

// sessionModePath returns the path of the plaintext sidecar file that stores
// id's chosen mode, next to its session history (<id>.jsonl -> <id>.mode).
func sessionModePath(id string) (string, error) {
	p, err := session.Path(id)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(p, ".jsonl") + ".mode", nil
}

// writeSessionMode records id's chosen mode in its sidecar file. An empty
// mode removes the sidecar (best-effort) so the session falls back to the
// default coding system prompt.
func writeSessionMode(id, mode string) error {
	p, err := sessionModePath(id)
	if err != nil {
		return err
	}
	if mode == "" {
		_ = os.Remove(p)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(mode), 0o600)
}

// readSessionMode returns id's stored mode, or "" if none is set or the
// sidecar can't be read.
func readSessionMode(id string) string {
	p, err := sessionModePath(id)
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// conversationalSystemPrompt is the standalone system prompt for "conversational"
// mode: a general-purpose assistant, deliberately free of any repo/coding-agent
// framing, so a session can be a normal conversational partner rather than a
// coding agent operating on a codebase.
const conversationalSystemPrompt = `You are GopherMind, a helpful, direct, and warm conversational assistant.

You talk with the person like a knowledgeable friend: clear, concise, and honest,
without unnecessary hedging or filler. Match the tone and depth of the
conversation — brief for quick questions, thorough when the topic calls for it.

You can help with anything: everyday questions, writing, brainstorming, planning,
explanations, and general problem-solving. You have access to tools; use them when
they would genuinely help answer the question or complete a request, but don't
reach for them by default — most conversations don't need one.

If you're unsure what someone wants, ask a short clarifying question rather than
guessing. If you don't know something, say so plainly.`

// systemPromptForMode returns the system prompt to use for a session in mode,
// given the server's default (coding) basePrompt and the repo root (used to
// resolve custom personas):
//   - "" or "coding": basePrompt, unchanged.
//   - "conversational": a standalone general-assistant prompt, with no
//     repo/coding framing.
//   - anything else (reviewer/architect/tester/custom persona name): basePrompt
//     with the resolved persona text appended, or basePrompt unchanged if the
//     persona can't be resolved.
func systemPromptForMode(mode, basePrompt, root string) string {
	switch mode {
	case "", "coding":
		return basePrompt
	case "conversational":
		return conversationalSystemPrompt
	default:
		if p, ok := persona.Resolve(root, mode); ok {
			return basePrompt + "\n\n" + p
		}
		return basePrompt
	}
}
