package acceptcheck

// The quality level of the checker. Vacuous (vacuous.go) asks whether a root
// test can prove anything about the built server at all; Quality asks whether
// the command can actually fail: no masked exit status, no placeholder, an
// assertion behind every curl and jq. Findings are a fixed vocabulary and the
// bullet id is the only thing a caller attaches to one: a command is never
// quoted.

import (
	"regexp"
	"sort"
	"strings"
)

// Finding is one fixed-vocabulary defect of an acceptance root test command.
// A finding never carries command text.
type Finding string

const (
	// FindMasked: the exit status is thrown away (|| true, || echo, ; true,
	// exit 0, set +e, 2>/dev/null ||, a trailing echo after a semicolon).
	FindMasked Finding = "masked_failure"
	// FindSuccessEcho: a trailing "&& echo OK" is the only thing that "asserts".
	FindSuccessEcho Finding = "success_echo"
	// FindPlaceholder: {id}, <id>, ..., TODO or (simulate ...) left in the command.
	FindPlaceholder Finding = "placeholder"
	// FindUndefinedVar: a $VARIABLE no earlier statement sets and the harness
	// does not provide.
	FindUndefinedVar Finding = "undefined_variable"
	// FindCurlUnasserted: curl without -f whose output is not checked.
	FindCurlUnasserted Finding = "curl_unasserted"
	// FindJqUnasserted: jq without -e whose output is not checked.
	FindJqUnasserted Finding = "jq_unasserted"
	// FindPrintOnly: nothing in the command can fail (echo, cat, ls, find, head).
	FindPrintOnly Finding = "print_only"
)

// FindingNames are the findings as strings, in the order given.
func FindingNames(fs []Finding) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = string(f)
	}
	return out
}

// Options names what the checker may treat as defined.
type Options struct {
	Bins []string // built binary names (cmd/<name>)
	Env  []string // the brief's declared env and secret names
}

var printOnly = map[string]bool{
	"echo": true, "printf": true, "cat": true, "ls": true, "head": true, "tail": true, "wc": true,
	"sort": true, "uniq": true, "tr": true, "cut": true, "pwd": true, "date": true, "sleep": true,
	"tee": true, "column": true, "true": true, ":": true, "find": true, "which": true, "type": true,
}

// neutral commands change the shell, not the verdict.
var neutral = map[string]bool{
	"cd": true, "export": true, "unset": true, "set": true, "mkdir": true, "rm": true, "cp": true, "mv": true,
	"trap": true, "wait": true, "kill": true, "local": true, "readonly": true, "declare": true, "read": true,
	"shift": true, "alias": true, "source": true, ".": true, "exit": true, "exec": true, "false": true,
}

// testCmds compare a value: a command substitution handed to one is asserted.
var testCmds = map[string]bool{"test": true, "[": true, "[[": true, "diff": true, "cmp": true, "grep": true, "egrep": true, "fgrep": true, "expr": true}

var (
	successWordRE  = regexp.MustCompile(`(?i)(^|[^A-Za-z0-9])(ok|pass|passed|success|succeeded|done|all good)($|[^A-Za-z0-9])`)
	braceHolderRE  = regexp.MustCompile(`[/=?&:]\{[A-Za-z_][A-Za-z0-9_.-]*\}`)
	angleHolderRE  = regexp.MustCompile(`([/=?&:]|(^|\s))<[A-Za-z_][A-Za-z0-9_-]*>($|[/?&\s'"])`)
	simulateRE     = regexp.MustCompile(`(?i)\(\s*simulat`)
	todoRE         = regexp.MustCompile(`\b(TODO|FIXME|TBD)\b`)
	varUseRE       = regexp.MustCompile(`\$(?:\{([A-Za-z_][A-Za-z0-9_]*)(:?[-=+?])?|([A-Za-z_][A-Za-z0-9_]*))`)
	varDefRE       = regexp.MustCompile(`(?:^|[\s;&|(])(?:export\s+|local\s+|readonly\s+|declare\s+(?:-[A-Za-z]+\s+)?)?([A-Za-z_][A-Za-z0-9_]*)\+?=`)
	varLoopRE      = regexp.MustCompile(`(?:^|[\s;&|(])(?:for|read(?:\s+-[A-Za-z]+)*|getopts\s+\S+)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	wellKnownNames = map[string]bool{"HOME": true, "PATH": true, "PWD": true, "OLDPWD": true, "USER": true, "TMPDIR": true,
		"RANDOM": true, "SECONDS": true, "LINENO": true, "HOSTNAME": true, "SHELL": true, "IFS": true, "GOFLAGS": true,
		"GOPATH": true, "GOROOT": true, "GOOS": true, "GOARCH": true, "CI": true, "PPID": true, "UID": true}
)

// Quality returns the quality findings of one root test command, sorted and
// unique; none means the command can fail for the right reason.
func Quality(cmd string, o Options) []Finding {
	set := map[Finding]bool{}
	body := cmd
	for depth := 0; depth < 4; depth++ { // sh -c 'body' is read as its body
		stmts, _, _ := shellParse(body)
		if len(stmts) != 1 {
			break
		}
		i := commandIndex(stmts[0].words)
		if i < 0 || !shells[baseOf(stmts[0].words[i].lit)] {
			break
		}
		next, found := "", false
		for j, a := range stmts[0].words[i+1:] {
			if a.lit == "-c" && i+j+2 < len(stmts[0].words) {
				next, found = stmts[0].words[i+j+2].lit, true
				break
			}
		}
		if !found {
			break
		}
		body = next
	}
	qualityPlaceholders(body, o, set)
	q := &qual{set: set}
	q.script(body, false, true)
	out := make([]Finding, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	if len(out) == 0 {
		return nil
	}
	return out
}

func baseOf(cmd string) string {
	if hasPrefixAny(cmd, systemDir) {
		return cmd[strings.LastIndex(cmd, "/")+1:]
	}
	return cmd
}

// strip removes single-quoted text and comments, returning the rest and the
// comment text. Double quotes are kept: the shell expands inside them.
func strip(s string) (rest string, comments []string) {
	rs := []rune(s)
	var b strings.Builder
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch {
		case c == '\\' && i+1 < len(rs):
			b.WriteRune(c)
			i++
			b.WriteRune(rs[i])
		case c == '\'':
			for i++; i < len(rs) && rs[i] != '\''; i++ {
			}
			b.WriteString("''")
		case c == '"':
			b.WriteRune(c)
			for i++; i < len(rs) && rs[i] != '"'; i++ {
				if rs[i] == '\\' && i+1 < len(rs) {
					b.WriteRune(rs[i])
					i++
				}
				b.WriteRune(rs[i])
			}
			b.WriteRune('"')
		case c == '#' && (i == 0 || strings.ContainsRune(" \t\n;&|(", rs[i-1])):
			j := i
			for j < len(rs) && rs[j] != '\n' {
				j++
			}
			comments = append(comments, string(rs[i:j]))
			i = j - 1
		default:
			b.WriteRune(c)
		}
	}
	return b.String(), comments
}

// qualityPlaceholders finds placeholders and variables nothing defines.
func qualityPlaceholders(script string, o Options, set map[Finding]bool) {
	if braceHolderRE.MatchString(script) || angleHolderRE.MatchString(script) ||
		strings.Contains(script, "{...}") || strings.Contains(script, "{…}") || simulateRE.MatchString(script) {
		set[FindPlaceholder] = true
	}
	rest, comments := strip(script)
	for _, c := range comments {
		if todoRE.MatchString(c) {
			set[FindPlaceholder] = true
		}
	}
	stmts, _, _ := shellParse(script)
	for _, st := range stmts {
		i := commandIndex(st.words)
		if i < 0 {
			continue
		}
		base := baseOf(st.words[i].lit)
		for _, a := range st.words[i+1:] {
			if base != "go" && (a.lit == "..." || a.lit == "…") {
				set[FindPlaceholder] = true
			}
			if (base == "echo" || base == "printf") && todoRE.MatchString(a.lit) {
				set[FindPlaceholder] = true
			}
		}
	}
	known := map[string]bool{}
	for _, n := range o.Env {
		known[n] = true
	}
	defAt := map[string]int{}
	for _, re := range []*regexp.Regexp{varDefRE, varLoopRE} {
		for _, m := range re.FindAllStringSubmatchIndex(rest, -1) {
			name := rest[m[2]:m[3]]
			if at, ok := defAt[name]; !ok || m[2] < at {
				defAt[name] = m[2]
			}
		}
	}
	for _, m := range varUseRE.FindAllStringSubmatchIndex(rest, -1) {
		name, op := "", ""
		if m[2] >= 0 {
			name = rest[m[2]:m[3]]
			if m[4] >= 0 {
				op = rest[m[4]:m[5]]
			}
		} else {
			name = rest[m[6]:m[7]]
		}
		if op == ":-" || op == "-" || op == ":=" || op == "=" {
			continue // has a default
		}
		if strings.HasPrefix(name, "GM_ACCEPTANCE_") || known[name] || wellKnownNames[name] {
			continue
		}
		if at, ok := defAt[name]; ok && at < m[0] {
			continue
		}
		set[FindUndefinedVar] = true
	}
}

// qual reads the statements of a script and notes findings in set.
type qual struct{ set map[Finding]bool }

type pipeline struct{ idx []int }

func (q *qual) script(script string, ctxAsserted, top bool) {
	stmts, _, _ := shellParse(script)
	trimmed := strings.TrimSpace(script)
	trailingBG := strings.HasSuffix(trimmed, "&") && !strings.HasSuffix(trimmed, "&&")
	bg := func(i int) bool {
		return (i+1 < len(stmts) && stmts[i+1].op == "&") || (trailingBG && i == len(stmts)-1)
	}
	// pipelines
	var pls []pipeline
	for i := range stmts {
		if stmts[i].op == "|" && len(pls) > 0 {
			pls[len(pls)-1].idx = append(pls[len(pls)-1].idx, i)
		} else {
			pls = append(pls, pipeline{idx: []int{i}})
		}
	}
	cmdAt := func(i int) (string, []word) {
		ws := stmts[i].words
		k := commandIndex(ws)
		if k < 0 {
			return "", nil
		}
		return baseOf(ws[k].lit), ws[k+1:]
	}
	// assertive: the exit status of this statement can fail the command for a
	// reason the author checked.
	assertive := func(i int) bool {
		base, args := cmdAt(i)
		switch {
		case base == "":
			return false
		case base == "find":
			for _, a := range args {
				if a.lit == "-exec" || a.lit == "-execdir" {
					return true
				}
			}
			return false
		case printOnly[base] || neutral[base]:
			return false
		case base == "awk" || base == "gawk":
			for _, a := range args {
				if strings.Contains(a.lit, "exit") {
					return true
				}
			}
			return false
		case base == "jq":
			return jqExit(args)
		case base == "curl":
			return curlFail(args)
		}
		return true
	}
	outputChecked := func(p pipeline) bool {
		last := p.idx[len(p.idx)-1]
		if bg(last) {
			return false
		}
		base, args := cmdAt(last)
		if base == "" || printOnly[base] || neutral[base] && !testCmds[base] {
			return false
		}
		switch base {
		case "awk", "gawk", "find":
			return assertive(last)
		case "jq":
			return jqExit(args)
		case "curl":
			return curlFail(args)
		}
		return true
	}
	// which statements assert, and the variables a test statement reads
	hasAssert := func(skipLast bool) bool {
		for pi, p := range pls {
			if skipLast && pi == len(pls)-1 {
				continue
			}
			last := p.idx[len(p.idx)-1]
			if !bg(last) && assertive(last) {
				return true
			}
		}
		return false
	}
	testReads := func(from int, name string) bool {
		for i := from; i < len(stmts); i++ {
			base, _ := cmdAt(i)
			if !testCmds[base] {
				continue
			}
			for _, w := range stmts[i].words {
				if strings.Contains(w.lit, "$"+name) || strings.Contains(w.lit, "${"+name+"}") {
					return true
				}
			}
		}
		return false
	}

	for _, p := range pls {
		checked := ctxAsserted || outputChecked(p)
		for _, i := range p.idx {
			base, args := cmdAt(i)
			switch base {
			case "curl":
				if !curlFail(args) && !checked {
					q.set[FindCurlUnasserted] = true
				}
			case "jq":
				if !jqExit(args) && !checked {
					q.set[FindJqUnasserted] = true
				}
			}
		}
	}
	// command substitutions: asserted when the statement that holds one compares it
	for i, st := range stmts {
		base, _ := cmdAt(i)
		for _, w := range st.words {
			for _, sub := range extractSubs(w.lit) {
				asserted := testCmds[base]
				if !asserted && base == "" || !asserted && assignRE.MatchString(w.lit) {
					if eq := strings.IndexByte(w.lit, '='); eq > 0 && assignRE.MatchString(w.lit) {
						asserted = testReads(i+1, w.lit[:eq])
					}
				}
				q.script(sub, asserted, false)
			}
		}
	}
	if !top {
		return
	}

	// masking constructs
	for i, st := range stmts {
		base, args := cmdAt(i)
		if base == "set" {
			for _, a := range args {
				if a.lit == "+e" {
					q.set[FindMasked] = true
				}
			}
		}
		if st.op != "||" {
			continue
		}
		// the right side: this statement, the && chain after it, and a group it opens
		depth := groupDepth(st.words)
		chain := []int{i}
		for j := i + 1; j < len(stmts) && (depth > 0 || stmts[j].op == "&&"); j++ {
			chain = append(chain, j)
			depth += groupDepth(stmts[j].words)
		}
		failing := false
		for _, j := range chain {
			b, a := cmdAt(j)
			if b == "false" || (b == "exit" && len(a) > 0 && a[0].lit != "0") || (b == "return" && len(a) > 0 && a[0].lit != "0") {
				failing = true
			}
		}
		if failing {
			continue
		}
		first, fargs := cmdAt(i)
		rhsMask := first == "true" || first == ":" || first == "echo" || first == "printf" ||
			(first == "exit" && (len(fargs) == 0 || fargs[0].lit == "0"))
		devnull := false
		if i > 0 && len(stmts[i-1].words) > 0 {
			devnull = stmts[i-1].words[len(stmts[i-1].words)-1].lit == "2>/dev/null"
		}
		if rhsMask || devnull {
			q.set[FindMasked] = true
		}
	}
	if n := len(pls); n > 0 {
		lastP := pls[n-1]
		last := lastP.idx[len(lastP.idx)-1]
		base, args := cmdAt(last)
		op := stmts[lastP.idx[0]].op
		if (op == ";" || op == "") && n > 1 && !bg(last) {
			if base == "true" || base == ":" || base == "echo" || base == "printf" ||
				(base == "exit" && len(args) == 1 && args[0].lit == "0") {
				q.set[FindMasked] = true
			}
		}
		if op == "&&" && (base == "echo" || base == "printf") && len(lastP.idx) == 1 {
			for _, a := range args {
				if successWordRE.MatchString(a.lit) {
					if !hasAssert(true) {
						q.set[FindSuccessEcho] = true
					}
					break
				}
			}
		}
	}
	if !hasAssert(false) {
		q.set[FindPrintOnly] = true
	}
}

func groupDepth(ws []word) int {
	d := 0
	for _, w := range ws {
		switch w.lit {
		case "(", "{":
			d++
		case ")", "}":
			d--
		}
	}
	return d
}

// curlFail: curl exits non-zero on an HTTP error status.
func curlFail(args []word) bool {
	for _, a := range args {
		l := a.lit
		if l == "--fail" || l == "--fail-with-body" {
			return true
		}
		if len(l) > 1 && l[0] == '-' && l[1] != '-' && strings.ContainsRune(l[1:], 'f') && !strings.ContainsAny(l, "/=:{") {
			return true
		}
	}
	return false
}

// jqExit: jq sets its exit status from the last output (-e, --exit-status).
func jqExit(args []word) bool {
	for _, a := range args {
		l := a.lit
		if l == "--exit-status" {
			return true
		}
		if len(l) > 1 && l[0] == '-' && l[1] != '-' && strings.ContainsRune(l[1:], 'e') && !strings.ContainsAny(l, " /=:{.|") {
			return true
		}
	}
	return false
}

// extractSubs are the scripts of the $( ) and backtick substitutions in a word.
func extractSubs(lit string) []string {
	var out []string
	rs := []rune(lit)
	for i := 0; i < len(rs); i++ {
		switch {
		case rs[i] == '$' && i+2 < len(rs) && rs[i+1] == '(' && rs[i+2] != '(':
			depth, j := 1, i+2
			for ; j < len(rs) && depth > 0; j++ {
				if rs[j] == '(' {
					depth++
				} else if rs[j] == ')' {
					depth--
				}
			}
			if depth == 0 {
				out = append(out, string(rs[i+2:j-1]))
				i = j - 1
			}
		case rs[i] == '`':
			j := i + 1
			for j < len(rs) && rs[j] != '`' {
				j++
			}
			if j < len(rs) {
				out = append(out, string(rs[i+1:j]))
				i = j
			}
		}
	}
	return out
}
