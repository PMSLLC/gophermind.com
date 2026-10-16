package acceptcheck

import (
	"regexp"
	"strings"
)

// GoTest is a root test command that is one plain go test invocation: the
// executor runs those through go test -json and holds them to the test runner's
// pass rule (a test ran, none failed, every named test passed) instead of
// trusting the exit code alone.
type GoTest struct {
	Pkg   string   // the one package argument
	Tags  string   // -tags value, a comma list
	Race  bool     // -race
	Short bool     // -short
	Funcs []string // the top-level tests -run names, when it is a plain name list
	Unset []string // names of leading `unset NAME` statements
	Env   []string // leading NAME=value words of the go test statement
}

var (
	goTagsRE    = regexp.MustCompile(`^[A-Za-z0-9_.,]+$`)
	goRunNameRE = regexp.MustCompile(`^\^?\(?([A-Za-z0-9_|]+)\)?\$?$`)
	identRE     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	acceptPkgRE = regexp.MustCompile(`^\./acceptance(/.*)?$`)
)

// goTestValueFlags take a separate value word.
var goTestValueFlags = map[string]bool{"-tags": true, "-run": true, "-count": true, "-timeout": true, "-p": true, "-parallel": true}

// ParseGoTest recognises `[unset NAME...;] [NAME=value] go test FLAGS PKG`: one
// package, the flags -tags -run -count -timeout -p -parallel -v -race -short.
// Anything else (a pipe, ||, a second command, another flag) is not a plain go
// test and is judged by its exit status alone.
func ParseGoTest(cmd string) (GoTest, bool) {
	var g GoTest
	trimmed := strings.TrimSpace(cmd)
	if strings.HasSuffix(trimmed, "&") && !strings.HasSuffix(trimmed, "&&") {
		return g, false
	}
	stmts, subs, bad := shellParse(cmd)
	if bad || len(subs) > 0 || len(stmts) == 0 {
		return g, false
	}
	for i, st := range stmts {
		if st.op != "" && st.op != ";" && st.op != "&&" {
			return g, false
		}
		if i < len(stmts)-1 {
			if st.words[0].lit != "unset" {
				return g, false
			}
			for _, w := range st.words[1:] {
				if !identRE.MatchString(w.lit) {
					return g, false
				}
				g.Unset = append(g.Unset, w.lit)
			}
		}
	}
	ws := stmts[len(stmts)-1].words
	k := 0
	for k < len(ws) && assignRE.MatchString(ws[k].lit) {
		g.Env = append(g.Env, ws[k].lit)
		k++
	}
	if k+1 >= len(ws) || baseOf(ws[k].lit) != "go" || ws[k+1].lit != "test" {
		return GoTest{}, false
	}
	args := ws[k+2:]
	pkgs := 0
	for i := 0; i < len(args); i++ {
		a := args[i].lit
		name, val, hasVal := a, "", false
		if strings.HasPrefix(a, "-") {
			if j := strings.IndexByte(a, '='); j > 0 {
				name, val, hasVal = a[:j], a[j+1:], true
			}
		}
		switch {
		case !strings.HasPrefix(a, "-"):
			if !goPkgArgRE.MatchString(a) {
				return GoTest{}, false
			}
			g.Pkg = a
			pkgs++
		case name == "-v":
		case name == "-race":
			g.Race = true
		case name == "-short":
			g.Short = true
		case goTestValueFlags[name]:
			if !hasVal {
				if i+1 >= len(args) {
					return GoTest{}, false
				}
				i++
				val = args[i].lit
			}
			switch name {
			case "-tags":
				if !goTagsRE.MatchString(val) {
					return GoTest{}, false
				}
				g.Tags = val
			case "-run":
				m := goRunNameRE.FindStringSubmatch(val)
				if m == nil {
					return GoTest{}, false
				}
				for _, n := range strings.Split(m[1], "|") {
					if !strings.HasPrefix(n, "Test") {
						return GoTest{}, false
					}
					g.Funcs = append(g.Funcs, n)
				}
			}
		default:
			return GoTest{}, false
		}
	}
	if pkgs != 1 {
		return GoTest{}, false
	}
	return g, true
}

// ServerExercising says a command exercises the built server (or the built
// binaries): it runs a built binary, curl, a go test of the acceptance
// package, or hands a command the acceptance address or a loopback address.
// A command that only checks the code statically (go build, go vet, gofmt,
// grep) does not.
func ServerExercising(cmd string, bins []string) bool {
	return exercises(cmd, bins, 0)
}

func exercises(script string, bins []string, depth int) bool {
	if depth > 4 {
		return false
	}
	stmts, subs, _ := shellParse(script)
	for _, st := range stmts {
		i := commandIndex(st.words)
		if i < 0 {
			continue
		}
		cmd := st.words[i].lit
		args := st.words[i+1:]
		base := baseOf(cmd)
		if base == "curl" || hasPrefixAny(cmd, binDirRef) {
			return true
		}
		for _, b := range bins {
			if base == b {
				return true
			}
		}
		if base == "go" && len(args) > 0 && args[0].lit == "test" {
			for _, a := range args[1:] {
				if acceptPkgRE.MatchString(a.lit) {
					return true
				}
			}
		}
		if shells[base] {
			for j, a := range args {
				if a.lit == "-c" && j+1 < len(args) && exercises(args[j+1].lit, bins, depth+1) {
					return true
				}
			}
		}
		if !noopCommands[base] {
			for _, a := range args {
				if strings.Contains(a.lit, "GM_ACCEPTANCE_") || loopbackRE.MatchString(a.lit) {
					return true
				}
			}
		}
	}
	for _, s := range subs {
		if exercises(s, bins, depth+1) {
			return true
		}
	}
	return false
}
