// Package fortune serves random quotes from an embedded fortune-cookie database.
//
// The database is Brian M. Clapper's fortune collection
// (https://github.com/bmc/fortunes), used under the Creative Commons Attribution
// 4.0 International License (https://creativecommons.org/licenses/by/4.0/). See
// CREDITS.md for attribution.
package fortune

import (
	_ "embed"
	"math/rand"
	"strings"
)

//go:embed fortunes.txt
var data string

// fortunes is the parsed set of entries, split on lines containing only "%".
var fortunes = parse(data)

// parse splits a fortune-cookie file into entries. Entries are separated by a
// line containing only "%"; surrounding blank lines are trimmed and empty
// entries dropped.
func parse(s string) []string {
	var out []string
	var cur []string
	flush := func() {
		if block := strings.TrimSpace(strings.Join(cur, "\n")); block != "" {
			out = append(out, block)
		}
		cur = cur[:0]
	}
	for _, line := range strings.Split(s, "\n") {
		if line == "%" {
			flush()
			continue
		}
		cur = append(cur, line)
	}
	flush()
	return out
}

// Count returns the number of available fortunes.
func Count() int { return len(fortunes) }

// Random returns a random fortune, or "" if the database is empty.
func Random() string {
	if len(fortunes) == 0 {
		return ""
	}
	return fortunes[rand.Intn(len(fortunes))]
}
