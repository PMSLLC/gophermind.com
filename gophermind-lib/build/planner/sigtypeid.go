package planner

import (
	"regexp"
	"strings"
)

// A model sometimes writes, in a function signature, the kebab-case id of a
// contract type where Go needs the type's name: `[]column-summary` for
// `[]ColumnSummary`. Such a signature does not parse. The rewrite is
// deterministic: a hyphenated token that equals the id of a contract type is
// replaced by that type's Go name, wherever it stands in the signature (behind
// pointer, slice, array, map and chan prefixes, in parameters, results and the
// receiver). A token that matches no type id is left as written.

var hyphenTokenRE = regexp.MustCompile(`[a-z][a-z0-9]*(?:-[a-z0-9]+)+`)

// hyphenatedIDHint is appended to the syntax error of a signature that holds a
// hyphenated token. It is fixed text and never quotes the signature.
const hyphenatedIDHint = "; a signature uses a hyphenated id where a Go type name is required; write the type's Go name"

func isIdentByte(c byte) bool {
	return c == '_' || c == '.' || isLower(c) || isUpper(c) || isDigit(c) || c >= 0x80
}

// hasHyphenToken reports whether sig holds a standalone hyphenated lower case token.
func hasHyphenToken(sig string) bool {
	for _, loc := range hyphenTokenRE.FindAllStringIndex(sig, -1) {
		if tokenStandsAlone(sig, loc[0], loc[1]) {
			return true
		}
	}
	return false
}

func tokenStandsAlone(sig string, lo, hi int) bool {
	if lo > 0 && (isIdentByte(sig[lo-1]) || sig[lo-1] == '-') {
		return false
	}
	return hi >= len(sig) || !(isIdentByte(sig[hi]) || sig[hi] == '-')
}

// typeGoNames maps the id of each type of types to the Go name its declaration
// declares: the name that normalises to the id, else the only name.
func typeGoNames(types []map[string]any) map[string]string {
	out := map[string]string{}
	for _, t := range types {
		id, _ := t["id"].(string)
		decl, _ := t["decl"].(string)
		if id == "" {
			continue
		}
		names := declNames(decl)
		pick := ""
		for _, n := range names {
			if normalizeID(n, false) == id {
				pick = n
				break
			}
		}
		if pick == "" && len(names) == 1 {
			pick = names[0]
		}
		if pick != "" {
			out[id] = pick
		}
	}
	return out
}

// replaceSigTypeIDs replaces the hyphenated tokens of sig that are type ids.
func replaceSigTypeIDs(sig string, names map[string]string) (string, int) {
	n := 0
	var b strings.Builder
	last := 0
	for _, loc := range hyphenTokenRE.FindAllStringIndex(sig, -1) {
		name, ok := names[sig[loc[0]:loc[1]]]
		if !ok || !tokenStandsAlone(sig, loc[0], loc[1]) {
			continue
		}
		b.WriteString(sig[last:loc[0]])
		b.WriteString(name)
		last = loc[1]
		n++
	}
	if n == 0 {
		return sig, 0
	}
	b.WriteString(sig[last:])
	return b.String(), n
}

// normalizeSigTypeIDs rewrites the signatures of functions against the types
// of the contract written so far and of the same reply. It returns the number
// of tokens replaced and at most maxIDExamples function ids (bounded).
func normalizeSigTypeIDs(doc map[string]any, replyTypes, functions []map[string]any) (int, []string) {
	names := typeGoNames(append(append([]map[string]any{}, objects(doc["types"])...), replyTypes...))
	total := 0
	var ex []string
	for _, f := range functions {
		sig, ok := f["signature"].(string)
		if !ok {
			continue
		}
		out, n := replaceSigTypeIDs(sig, names)
		if n == 0 {
			continue
		}
		f["signature"] = out
		total += n
		if len(ex) < maxIDExamples {
			id, _ := f["id"].(string)
			ex = append(ex, boundedID(id))
		}
	}
	return total, ex
}
