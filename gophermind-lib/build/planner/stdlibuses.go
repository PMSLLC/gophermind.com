package planner

import "strings"

// stdlibPackages are the standard library import paths a model is likely to
// write into a function's uses, with the slashes written as dashes the way an
// id is: "encoding/json" is "encoding-json".
var stdlibPackages = func() map[string]bool {
	m := map[string]bool{}
	for _, p := range strings.Fields(`archive/tar archive/zip bufio bytes cmp compress/gzip container/list context
		crypto/hmac crypto/md5 crypto/rand crypto/sha1 crypto/sha256 crypto/sha512 database/sql embed encoding/base64
		encoding/binary encoding/csv encoding/hex encoding/json encoding/xml errors flag fmt hash hash/fnv html
		html/template io io/fs io/ioutil iter log log/slog maps math math/big math/bits math/rand mime net net/http
		net/http/httptest net/url os os/exec os/signal path path/filepath reflect regexp runtime slices sort strconv
		strings sync sync/atomic syscall testing text/tabwriter text/template time unicode unicode/utf8 unsafe`) {
		m[strings.ReplaceAll(p, "/", "-")] = true
	}
	return m
}()

// isStdlibName reports whether id names a standard library package: the
// package path in id form, with or without the fn- prefix.
func isStdlibName(id string) bool {
	return stdlibPackages[strings.TrimPrefix(strings.ToLower(id), "fn-")]
}

// dropStdlibUses removes from every function's and type's uses the entries that
// name a standard library package and that nothing in the contract declares, and
// returns them in the order found. A package is imported, not listed: leaving
// it makes the repair step ask the model to declare it, and the model answers
// with an empty function named after the package (func fmt(), func os()) that
// shadows the import. It runs at the final check, when every component is
// written and no later pass can declare an id, so a function that really is
// called fn-log or fn-sort is kept.
func dropStdlibUses(doc map[string]any) []string {
	declared := declaredIDs(doc)
	var dropped []string
	for _, key := range []string{"types", "functions"} {
		for _, o := range objects(doc[key]) {
			uses, ok := o["uses"].([]any)
			if !ok {
				continue
			}
			kept := make([]any, 0, len(uses))
			for _, u := range uses {
				if s, ok := u.(string); ok && !declared[s] && isStdlibName(s) {
					dropped = append(dropped, s)
					continue
				}
				kept = append(kept, u)
			}
			if len(kept) != len(uses) {
				o["uses"] = kept
			}
		}
	}
	return dropped
}
