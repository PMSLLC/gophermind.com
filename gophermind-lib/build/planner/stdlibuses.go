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

// stdlibIdents are the exported types and functions of the standard library a
// model is likely to list as if they were contract functions, written as an id:
// io.Writer is "io-writer". Only well-known names are listed: an id that merely
// starts with a package name (fn-io-handler) may be a real function.
var stdlibIdents = func() map[string]bool {
	m := map[string]bool{}
	for _, p := range strings.Fields(`io.Reader io.Writer io.ReadCloser io.WriteCloser io.ReadWriter io.Closer io.EOF
		io.Copy io.ReadAll io.Discard os.File os.Args os.Exit os.Stdin os.Stdout os.Stderr os.Open os.Create os.ReadFile
		os.WriteFile os.Getenv os.Stat bufio.Reader bufio.Writer bufio.Scanner bufio.NewReader bufio.NewScanner
		bufio.NewWriter strings.Builder strings.Reader strings.NewReader strings.Split strings.Join strings.TrimSpace
		bytes.Buffer bytes.NewBuffer bytes.NewReader errors.New errors.Is errors.As errors.Error fmt.Errorf fmt.Sprintf
		fmt.Fprintf fmt.Println fmt.Fprintln fmt.Sprint strconv.Atoi strconv.Itoa strconv.ParseInt strconv.ParseFloat
		strconv.FormatInt time.Time time.Duration time.Now time.Sleep context.Context sync.Mutex sync.WaitGroup
		sync.RWMutex http.Client http.Request http.Handler http.HandlerFunc http.ResponseWriter http.Server
		json.Marshal json.Unmarshal json.NewDecoder json.NewEncoder regexp.Regexp regexp.MustCompile unicode.IsLetter
		unicode.IsDigit unicode.IsSpace flag.Parse flag.String flag.Int flag.FlagSet sort.Slice sort.Strings log.Printf
		log.Fatal testing.T testing.B`) {
		m[strings.ToLower(strings.ReplaceAll(p, ".", "-"))] = true
	}
	return m
}()

// isStdlibName reports whether id names a standard library package or one of
// its well-known identifiers, in id form, with or without the fn- prefix.
func isStdlibName(id string) bool {
	id = strings.TrimPrefix(strings.ToLower(id), "fn-")
	return stdlibPackages[id] || stdlibIdents[id]
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
				if s, ok := u.(string); ok && isStdlibRef(s, declared) {
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

// isStdlibRef reports whether a uses entry names the standard library and
// nothing in the contract declares it. An entry may be a pending reference,
// "type-form|fn-form", written before the planner knows which one exists.
func isStdlibRef(s string, declared map[string]bool) bool {
	forms := strings.Split(s, pendingSep)
	for _, f := range forms {
		if declared[f] {
			return false
		}
	}
	for _, f := range forms {
		if isStdlibName(f) {
			return true
		}
	}
	return false
}
