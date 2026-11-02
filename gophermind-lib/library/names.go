// Package library holds brief library naming helpers and storage.
package library

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// NextName returns the name for a new child of parent. all is every version
// name in the family; children are those whose parent is parent.
func NextName(parent string, all []string, children []string) (string, error) {
	if parent == "" {
		return "v1", nil
	}
	taken := make(map[string]bool, len(all)+len(children))
	for _, n := range all {
		taken[n] = true
	}
	for _, n := range children {
		taken[n] = true
	}
	if len(children) == 0 {
		if c := continuation(parent); c != "" && !taken[c] {
			return c, nil
		}
	}
	for c := byte('a'); c <= 'z'; c++ {
		cand := parent + ".1"
		if c != 'a' {
			cand = parent + string(c) + ".1"
		}
		if !taken[cand] {
			return cand, nil
		}
	}
	return "", fmt.Errorf("no unused branch name left under %q", parent)
}

// continuation is the first-child name: v1 gives v1.1, v1.1 gives v1.2.
func continuation(parent string) string {
	i := strings.LastIndex(parent, ".")
	if i < 0 {
		return parent + ".1"
	}
	n, err := strconv.Atoi(parent[i+1:])
	if err != nil || n < 0 {
		return ""
	}
	return parent[:i+1] + strconv.Itoa(n+1)
}

// Slug turns a title into a lowercase [a-z0-9-] slug of at most 60 characters.
func Slug(title string) (string, error) {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		} else {
			dash = true
		}
	}
	s := b.String()
	if len(s) > 60 {
		s = strings.TrimRight(s[:60], "-")
	}
	if s == "" {
		return "", errors.New("title has no usable characters")
	}
	return s, nil
}

// RunName builds <family>-<version>-<YYYYMMDD>-rNNN.
func RunName(family, version string, day time.Time, n int) string {
	return fmt.Sprintf("%s-%s-%s-r%03d", family, version, day.Format("20060102"), n)
}
