package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"gophermind/gophermind-lib/safety"
	"gophermind/gophermind-lib/tools"
)

// burrowInput is one named, typed input of a contract.
type burrowInput struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// burrowLookupTool is a read-only lookup of an exact contract in Burrow. It is
// not gated: it changes nothing.
func (c *chatTools) burrowLookupTool() tools.Tool {
	return tools.Tool{
		Name:        "burrow_lookup",
		Description: "Ask Burrow whether a function with this exact contract is already stored, and get its Go code. Exact contract only: similar functions are not searched. Call the returned function with its inputs in the order shown under 'parameter order', which can differ from the order you asked for. Types: string, int64, float64, bool, bytes, list<T>, optional<T>, map<string,V>, record{name:T,...}.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"verb":   map[string]any{"type": "string", "description": "the function's verb, for example Count"},
				"object": map[string]any{"type": "string", "description": "the function's object, for example Text.Words"},
				"inputs": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"name": map[string]any{"type": "string"},
							"type": map[string]any{"type": "string"},
						},
						"required":             []string{"name", "type"},
						"additionalProperties": false,
					},
				},
				"outputs":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "output types, at least one"},
				"failures": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"traits":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"lang":     map[string]any{"type": "string", "description": "default go"},
			},
			"required":             []string{"verb", "object", "inputs", "outputs"},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			const tool = "burrow_lookup"
			if err := checkArgKeys(tool, raw, "verb", "object", "inputs", "outputs", "failures", "traits", "lang"); err != nil {
				return "", err
			}
			var a struct {
				Verb     string        `json:"verb"`
				Object   string        `json:"object"`
				Inputs   []burrowInput `json:"inputs"`
				Outputs  []string      `json:"outputs"`
				Failures []string      `json:"failures"`
				Traits   []string      `json:"traits"`
				Lang     string        `json:"lang"`
			}
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&a); err != nil {
				return "", fmt.Errorf("%s: bad arguments: %w", tool, err)
			}
			if strings.TrimSpace(a.Verb) == "" || strings.TrimSpace(a.Object) == "" {
				return "", fmt.Errorf("%s: verb and object are required", tool)
			}
			if a.Inputs == nil {
				return "", fmt.Errorf("%s: inputs is required (an empty list if none)", tool)
			}
			for i, in := range a.Inputs {
				if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Type) == "" {
					return "", fmt.Errorf("%s: input %d needs a name and a type", tool, i)
				}
			}
			if len(a.Outputs) == 0 {
				return "", fmt.Errorf("%s: at least one output type is required", tool)
			}
			for i, o := range a.Outputs {
				if strings.TrimSpace(o) == "" {
					return "", fmt.Errorf("%s: output %d is empty", tool, i)
				}
			}
			lang := strings.TrimSpace(a.Lang)
			if lang == "" {
				lang = "go"
			}
			contract := map[string]any{
				"verb": a.Verb, "object": a.Object,
				"inputs": a.Inputs, "outputs": a.Outputs,
				"effects":  []string{},
				"failures": burrowList(a.Failures),
				"traits":   burrowList(a.Traits),
			}
			res, err := c.burrow.lookup(ctx, contract, lang)
			if err != nil {
				return c.cleanWith(burrowErrorSentence(err), "the Burrow catalogue"), nil
			}
			return headCap(redactText(renderBurrowLookup(res)), maxToolOutput), nil
		},
	}
}

func burrowList(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// burrowErrorSentence turns a client error into one sentence for the model.
func burrowErrorSentence(err error) string {
	var be *burrowError
	if !errors.As(err, &be) {
		return "the Burrow lookup failed."
	}
	switch be.Kind {
	case "unauthorized":
		return "the Burrow token was rejected; John needs to check the Burrow token file."
	case "unreachable":
		return "Burrow did not answer; it may be down or unreachable from this server."
	case "timeout":
		return "Burrow took too long to answer; try again."
	case "bad_request":
		return "Burrow refused the contract: " + oneLine(be.Msg)
	case "server":
		return "Burrow failed on its side (" + oneLine(be.Msg) + "); try again later."
	}
	switch {
	case strings.Contains(be.Msg, "status 404"):
		return "Burrow does not know that route/version; the server and Burrow may be mismatched."
	case strings.Contains(be.Msg, "status 429"):
		return "Burrow is busy, try again."
	}
	return "the Burrow lookup failed (" + oneLine(be.Msg) + ")."
}

// storedContract is the part of a stored contract the tool displays.
type storedContract struct {
	Verb     string        `json:"verb"`
	Object   string        `json:"object"`
	Inputs   []burrowInput `json:"inputs"`
	Outputs  []string      `json:"outputs"`
	Failures []string      `json:"failures"`
	Traits   []string      `json:"traits"`
}

// renderBurrowLookup formats a lookup result. The hit is decided by Outcome
// only (a miss carries a literal null contract). The body is capped here, head
// first, so the key and parameter order always survive the output cap.
func renderBurrowLookup(r burrowLookup) string {
	if r.Outcome != "exact" && r.Outcome != "similar" {
		out := "found: no (exact contract only; similar functions are not searched without test vectors)"
		if r.AwaitingConfirmation {
			out += "\nA generated body exists for this contract but is not confirmed, so it is not shown."
		}
		return out
	}
	if r.AwaitingConfirmation || r.Rendering == nil {
		return "found: yes (" + clampField(r.Outcome, 32) + "), key " + clampField(r.Key, 200) + "\nA generated body exists but is not confirmed, so it is not shown."
	}
	var sc storedContract
	hasContract := len(r.Contract) > 0 && string(r.Contract) != "null" && json.Unmarshal(r.Contract, &sc) == nil
	var b strings.Builder
	fmt.Fprintf(&b, "found: yes (%s)\n", clampField(r.Outcome, 32))
	fmt.Fprintf(&b, "key: %s\n", clampField(r.Key, 200))
	if hasContract {
		ins := make([]string, len(sc.Inputs))
		names := make([]string, len(sc.Inputs))
		for i, in := range sc.Inputs {
			ins[i] = oneLine(in.Name) + ":" + oneLine(in.Type)
			names[i] = oneLine(in.Name)
		}
		line := fmt.Sprintf("%s %s(%s) -> (%s)", oneLine(sc.Verb), oneLine(sc.Object), strings.Join(ins, ", "), oneLine(strings.Join(sc.Outputs, ", ")))
		if len(sc.Failures) > 0 {
			line += " failures: " + oneLine(strings.Join(sc.Failures, ", "))
		}
		if len(sc.Traits) > 0 {
			line += " traits: " + oneLine(strings.Join(sc.Traits, ", "))
		}
		fmt.Fprintf(&b, "contract: %s\n", clampField(line, 2048))
		if len(names) == 0 {
			b.WriteString("parameter order: (no inputs)\n")
		} else {
			fmt.Fprintf(&b, "parameter order: %s\n", clampField(strings.Join(names, ", "), 1024))
		}
	} else {
		b.WriteString("parameter order: unavailable (the stored contract could not be read)\n")
	}
	fmt.Fprintf(&b, "verified at: %s\n", clampField(r.Rendering.VerifiedAt, 64))
	fmt.Fprintf(&b, "language: %s\n", clampField(r.Rendering.Lang, 32))
	fmt.Fprintf(&b, "eligible: %t\n", r.Eligible)
	b.WriteString("body:\n")
	body := r.Rendering.Body
	// Leave slack for redaction, which can lengthen text.
	if room := maxToolOutput - b.Len() - 512; len(body) > room {
		if room < 0 {
			room = 0
		}
		body = cutRunes(body, room) + fmt.Sprintf("\n[body cut: %d of %d bytes]", len(cutRunes(body, room)), len(r.Rendering.Body))
	}
	b.WriteString(body)
	return b.String()
}

// cutRunes returns the longest prefix of s of at most n bytes that ends on a
// rune boundary.
func cutRunes(s string, n int) string {
	if n >= len(s) {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// clampField flattens s to one line and cuts it to max bytes, marking the cut
// with "..." and the true length.
func clampField(s string, max int) string {
	s = oneLine(s)
	if len(s) <= max {
		return s
	}
	return cutRunes(s, max) + fmt.Sprintf("... (%d bytes)", len(s))
}

// redactText redacts secrets without capping.
func redactText(s string) string {
	return bearerRE.ReplaceAllString(safety.NewSecretScanner().Redact(s), "Bearer [REDACTED]")
}

// headCap keeps the head of s within max bytes, on a rune boundary, and says
// what it cut. Unlike capOutput it never drops the leading lines.
func headCap(s string, max int) string {
	if len(s) <= max {
		return s
	}
	note := fmt.Sprintf("\n[body cut: %d of %d bytes]", 0, len(s))
	keep := cutRunes(s, max-len(note)-8)
	return keep + fmt.Sprintf("\n[body cut: %d of %d bytes]", len(keep), len(s))
}
