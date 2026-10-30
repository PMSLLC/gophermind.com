package planner

import "testing"

// Run gm-2026-10-30-066: func getHashAlgorithm(name string) (hash.Hash, error)
// was refused as an unresolvable qualifier through every repair, because the
// table had no "hash". Every entry must also be a real standard path.
func TestResolveQualifierKnowsTheHashPackages(t *testing.T) {
	for q, want := range map[string]string{"hash": "hash", "sha512": "crypto/sha512", "md5": "crypto/md5", "crc32": "hash/crc32", "slices": "slices", "csv": "encoding/csv", "flag": "flag"} {
		if got, ok := ResolveQualifier(q, "", nil, nil); !ok || got != want {
			t.Errorf("%s: got %q %v, want %q", q, got, ok, want)
		}
	}
	for _, ambiguous := range []string{"template", "pprof", "scanner"} {
		if _, ok := ResolveQualifier(ambiguous, "", nil, nil); ok {
			t.Errorf("%s resolved, but it names more than one standard package", ambiguous)
		}
	}
	for q, p := range StdQualifiers {
		if top := stdTopOf(p); !stdTopLevel[top] {
			t.Errorf("%s -> %s is not a standard library path", q, p)
		}
	}
}

func stdTopOf(p string) string {
	for i := 0; i < len(p); i++ {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return p
}
