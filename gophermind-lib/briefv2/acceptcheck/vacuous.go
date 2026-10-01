// Package acceptcheck is the shell-aware reader of acceptance root test
// commands, shared by the planner (which refuses weak commands before a plan is
// approved) and the executor (which refuses ones that cannot prove the built
// server before it runs them). It never runs a command and never quotes one.
package acceptcheck

import (
	"regexp"
	"strings"
)

// noopCommands are shell words whose whole effect is nothing the server can
// be blamed for.
var noopCommands = map[string]bool{
	"true": true, ":": true, "false": true, "exit": true, "echo": true, "printf": true,
	"sleep": true, "test": true, "[": true, "[[": true,
}

var shellKeywords = map[string]bool{
	"if": true, "then": true, "else": true, "elif": true, "fi": true, "do": true, "done": true,
	"while": true, "until": true, "for": true, "!": true, "{": true, "}": true, "(": true, ")": true, "time": true,
}

var wrappers = map[string]bool{"env": true, "command": true, "exec": true, "nohup": true, "builtin": true, "timeout": true}

var (
	assignRE   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	systemDir  = []string{"/usr/bin/", "/bin/", "/usr/sbin/", "/sbin/"}
	binDirRef  = []string{"$GM_ACCEPTANCE_BIN/", "${GM_ACCEPTANCE_BIN}/"}
	goRefuseRE = regexp.MustCompile(`(^|[^A-Za-z0-9_])go\s+(run|install|get|generate)(\s|$|['"])`)
	binDirOpRE = regexp.MustCompile(`(^|[\s/"])bin(/|\s|"|$)`)
	mutators   = map[string]bool{"rm": true, "mv": true, "cp": true, "chmod": true, "ln": true}
	shells     = map[string]bool{"sh": true, "bash": true, "dash": true, "zsh": true}
)

// loopbackRE: a loopback host:port, a loopback URL, localhost followed by a
// slash, or a bare 127.x.x.x or ::1 word. A word that only contains
// "localhost" (localhost.txt) is not one.
var loopbackRE = regexp.MustCompile(`(^|[^A-Za-z0-9_.-])(` +
	`(localhost|127\.[0-9]+\.[0-9]+\.[0-9]+|\[::1\]):[0-9]+` +
	`|https?://(localhost|127\.[0-9]+\.[0-9]+\.[0-9]+|\[::1\])` +
	`|localhost/` +
	`|127\.[0-9]+\.[0-9]+\.[0-9]+|\[?::1\]?)($|[^A-Za-z0-9_.-])`)

// goFlagValue are go build, vet and test flags that take a separate value.
var goFlagValue = map[string]bool{
	"-o": true, "-tags": true, "-run": true, "-count": true, "-p": true, "-timeout": true, "-ldflags": true,
	"-gcflags": true, "-asmflags": true, "-mod": true, "-modfile": true, "-coverprofile": true, "-bench": true,
	"-cpu": true, "-parallel": true, "-covermode": true, "-vet": true, "-exec": true, "-buildmode": true,
	"-compiler": true, "-pkgdir": true, "-toolexec": true, "-overlay": true, "-C": true, "-coverpkg": true,
	"-benchtime": true, "-skip": true, "-shuffle": true, "-fuzz": true, "-blockprofile": true, "-cpuprofile": true,
	"-memprofile": true, "-outputdir": true, "-trimpath": false,
}

var goPkgArgRE = regexp.MustCompile(`^(\.|\.\.\.|(\./|\.\./)?[A-Za-z0-9_][A-Za-z0-9_.\-]*(/[A-Za-z0-9_.\-]+)*(/\.\.\.)?|\./\.\.\.|(\./)[A-Za-z0-9_./\-]*(/\.\.\.)?)$`)

// goPackageArg reports a package argument after go build, vet or test: ".",
// "./...", "./path", "./path/...", or an import path, with the flags and
// their values skipped.
func goPackageArg(args []word) bool {
	for i := 0; i < len(args); i++ {
		a := args[i].lit
		if strings.HasPrefix(a, "-") {
			if goFlagValue[a] && !strings.Contains(a, "=") {
				i++
			}
			continue
		}
		if goPkgArgRE.MatchString(a) {
			return true
		}
	}
	return false
}

// word is one shell word: its text with the quotes taken off.
type word struct{ lit string }

// stmt is a run of words between operators; op is the operator before it
// ("" for the first, ";" also stands for a newline).
type stmt struct {
	op    string
	words []word
}

// shellParse reads cmd into statements and the scripts hidden in $( ) and
// backticks. Comments (an unquoted # at the start of a word) are dropped.
func shellParse(cmd string) (stmts []stmt, subs []string, unterminated bool) {
	rs := []rune(cmd)
	var cur strings.Builder
	var words []word
	have := false
	op := ""
	pushWord := func() {
		if have {
			words = append(words, word{lit: cur.String()})
		}
		cur.Reset()
		have = false
	}
	type heredoc struct {
		delim string
		strip bool
	}
	var pending []heredoc
	// skipBodies consumes the lines after the current one up to each pending
	// delimiter: they are data, not statements.
	skipBodies := func(i int) int {
		for _, h := range pending {
			found := false
			for i+1 < len(rs) {
				j := i + 1
				for j < len(rs) && rs[j] != '\n' {
					j++
				}
				line := string(rs[i+1 : j])
				i = j
				if h.strip {
					line = strings.TrimLeft(line, "\t")
				}
				if line == h.delim {
					found = true
					break
				}
			}
			if !found {
				unterminated = true
			}
		}
		pending = nil
		return i
	}
	pushStmt := func(next string) {
		pushWord()
		if len(words) > 0 {
			stmts = append(stmts, stmt{op: op, words: words})
		} else if op == "&" && next == ";" {
			return // "cmd &" then a newline: the next statement still follows a background one
		}
		words = nil
		op = next
	}
	// group reads from rs[i] (just after an opener) to the matching closer.
	group := func(i int, open, closeR rune) (string, int) {
		depth, start := 1, i
		for ; i < len(rs); i++ {
			switch rs[i] {
			case '\\':
				i++
			case '\'':
				for i++; i < len(rs) && rs[i] != '\''; i++ {
				}
			case '"':
				for i++; i < len(rs) && rs[i] != '"'; i++ {
					if rs[i] == '\\' {
						i++
					}
				}
			case open:
				if open != closeR {
					depth++
				}
			case closeR:
				depth--
				if depth == 0 {
					return string(rs[start:i]), i
				}
			}
		}
		return string(rs[start:]), len(rs)
	}
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			pushWord()
		case c == '\n':
			pushStmt(";")
			if len(pending) > 0 {
				i = skipBodies(i)
			}
		case c == '<' && i+1 < len(rs) && rs[i+1] == '<' && !(i+2 < len(rs) && rs[i+2] == '<'):
			pushWord()
			i += 2
			strip := false
			if i < len(rs) && rs[i] == '-' {
				strip = true
				i++
			}
			for i < len(rs) && (rs[i] == ' ' || rs[i] == '\t') {
				i++
			}
			var d strings.Builder
			for ; i < len(rs) && !strings.ContainsRune(" \t\n;&|()<>", rs[i]); i++ {
				if rs[i] != '\'' && rs[i] != '"' && rs[i] != '\\' {
					d.WriteRune(rs[i])
				}
			}
			i--
			pending = append(pending, heredoc{delim: d.String(), strip: strip})
		case c == '#' && !have:
			for i < len(rs) && rs[i] != '\n' {
				i++
			}
			i--
		case c == '\\' && i+1 < len(rs):
			i++
			if rs[i] != '\n' {
				cur.WriteRune(rs[i])
				have = true
			}
		case c == '\'':
			have = true
			for i++; i < len(rs) && rs[i] != '\''; i++ {
				cur.WriteRune(rs[i])
			}
		case c == '"':
			have = true
			for i++; i < len(rs) && rs[i] != '"'; i++ {
				if rs[i] == '\\' && i+1 < len(rs) {
					i++
				} else if rs[i] == '$' && i+1 < len(rs) && rs[i+1] == '(' && !(i+2 < len(rs) && rs[i+2] == '(') {
					s, j := group(i+2, '(', ')')
					subs = append(subs, s)
					cur.WriteString("$(" + s + ")")
					i = j
					continue
				} else if rs[i] == '`' {
					s, j := group(i+1, '`', '`')
					subs = append(subs, s)
					cur.WriteString("`" + s + "`")
					i = j
					continue
				}
				cur.WriteRune(rs[i])
			}
		case c == '$' && i+1 < len(rs) && rs[i+1] == '(' && !(i+2 < len(rs) && rs[i+2] == '('):
			have = true
			s, j := group(i+2, '(', ')')
			subs = append(subs, s)
			cur.WriteString("$(" + s + ")")
			i = j
		case c == '`':
			have = true
			s, j := group(i+1, '`', '`')
			subs = append(subs, s)
			cur.WriteString("`" + s + "`")
			i = j
		case c == ';':
			if i+1 < len(rs) && rs[i+1] == ';' {
				i++
			}
			pushStmt(";")
		case c == '&' && ((i > 0 && (rs[i-1] == '>' || rs[i-1] == '<')) || (i+1 < len(rs) && rs[i+1] == '>')):
			cur.WriteRune(c)
			have = true
		case c == '&' || c == '|':
			o := string(c)
			if i+1 < len(rs) && rs[i+1] == c {
				o += string(c)
				i++
			}
			pushStmt(o)
		case (c == '(' || c == ')') && !have:
			pushWord()
			words = append(words, word{lit: string(c)})
		default:
			cur.WriteRune(c)
			have = true
		}
	}
	pushStmt("")
	if len(pending) > 0 {
		unterminated = true
	}
	return stmts, subs, unterminated
}

// commandIndex is the index of the command word: assignments, keywords and
// wrappers (env and its options, nohup, timeout and its duration) skipped.
// -1 when there is none.
func commandIndex(ws []word) int {
	for i := 0; i < len(ws); i++ {
		w := ws[i].lit
		switch {
		case w == "" || assignRE.MatchString(w) || (shellKeywords[w] && w != "for"):
			continue
		case wrappers[w]:
			for i+1 < len(ws) {
				n := ws[i+1].lit
				if n == "-u" || n == "-C" || n == "-S" {
					i += 2
					continue
				}
				if strings.HasPrefix(n, "-") || assignRE.MatchString(n) || (w == "timeout" && regexp.MustCompile(`^[0-9.]+[smhd]?$`).MatchString(n)) {
					i++
					continue
				}
				break
			}
			continue
		}
		return i
	}
	return -1
}

type block struct {
	kind  string // if, while, until, for
	cond  int    // 1 known true, -1 known false, 0 unknown
	phase string // cond, then, else, do
}

func (b block) dead() bool {
	switch {
	case b.kind == "if" && b.phase == "then":
		return b.cond == -1
	case b.kind == "if" && b.phase == "else":
		return b.cond == 1
	case b.kind == "while" && b.phase == "do":
		return b.cond == -1
	case b.kind == "until" && b.phase == "do":
		return b.cond == 1
	}
	return false
}

func known(cmd string) int {
	switch cmd {
	case "true", ":":
		return 1
	case "false":
		return -1
	}
	return 0
}

type verdict struct{ accepted, refused bool }

func (v *verdict) merge(o verdict) {
	v.accepted = v.accepted || o.accepted
	v.refused = v.refused || o.refused
}

func hasPrefixAny(s string, ps []string) bool {
	for _, p := range ps {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// Known limits: the reader does not expand variables or follow function calls,
// so `c=true; $c || curl x` (a variable holding true) and a function that is
// defined and never called are accepted when a probe sits in them. Both need a
// planner that writes them on purpose.
//
// analyze reads a script and says whether a statement that will run is a
// probe (accepted) or something forbidden (refused). Statements after an
// unconditional exit, the right side of `true ||` and `false &&`, and the dead
// branch of `if true` / `if false` are ignored.
func analyze(script string, bins []string, depth int) verdict {
	var v verdict
	if depth > 4 {
		return v
	}
	stmts, subs, bad := shellParse(script)
	if bad {
		v.refused = true // a here-document that never ends: the script is not what it seems
	}
	var stack []block
	groups := 0 // open { } and ( ): what is inside runs only under a condition or in a subshell
	deadExit := false
	prevKnown := 0
	for _, st := range stmts {
		ws := st.words
		// keywords and their blocks
		for len(ws) > 0 {
			k := ws[0].lit
			if !shellKeywords[k] {
				break
			}
			ws = ws[1:]
			switch k {
			case "{", "(":
				groups++
			case "}", ")":
				if groups > 0 {
					groups--
				}
			case "if", "while", "until", "for":
				b := block{kind: k, phase: "cond"}
				if i := commandIndex(ws); i >= 0 && len(ws) == i+1 {
					b.cond = known(ws[i].lit)
				}
				stack = append(stack, b)
			case "elif":
				if len(stack) > 0 {
					stack[len(stack)-1].phase, stack[len(stack)-1].cond = "cond", 0
				}
			case "then", "do":
				if len(stack) > 0 {
					stack[len(stack)-1].phase = k
				}
			case "else":
				if len(stack) > 0 {
					stack[len(stack)-1].phase = "else"
				}
			case "fi", "done":
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
			}
		}
		if len(ws) > 1 && strings.HasSuffix(ws[0].lit, "()") { // f() { ...: the body is read as if it ran
			ws = ws[1:]
			for len(ws) > 0 && (ws[0].lit == "{" || ws[0].lit == "(") {
				groups++
				ws = ws[1:]
			}
		}
		if len(ws) == 0 {
			continue
		}
		live := !deadExit
		for _, b := range stack {
			if b.dead() {
				live = false
			}
		}
		if (st.op == "||" && prevKnown == 1) || (st.op == "&&" && prevKnown == -1) {
			live = false
		}
		i := commandIndex(ws)
		if i < 0 {
			continue
		}
		cmd := ws[i].lit
		args := ws[i+1:]
		if !live {
			continue
		}
		prevKnown = known(cmd)
		deadAfter := false
		// Only a top-level, unconditional exit (or exec, which replaces the
		// shell) ends the script; one inside a group, a subshell, an if or a loop,
		// or on the right of && or ||, is reached only under a condition.
		firstWord := ws[0].lit
		if (cmd == "exit" || firstWord == "exec") && len(stack) == 0 && groups == 0 && (st.op == "" || st.op == ";") {
			deadAfter = true
		}
		base := cmd
		if hasPrefixAny(cmd, systemDir) {
			base = cmd[strings.LastIndex(cmd, "/")+1:]
		}
		if strings.Contains(cmd, "/") && base == cmd && !hasPrefixAny(cmd, binDirRef) {
			v.refused = true // a binary run by a path that is not the built one
		}
		if mutators[base] {
			for _, a := range args {
				if strings.Contains(a.lit, "GM_ACCEPTANCE_BIN") || binDirOpRE.MatchString(a.lit) {
					v.refused = true
				}
			}
		}
		switch {
		case base == "go" && len(args) >= 2 && (args[0].lit == "build" || args[0].lit == "vet" || args[0].lit == "test"):
			if goPackageArg(args[1:]) {
				v.accepted = true
			}
		case base == "curl", hasPrefixAny(cmd, binDirRef):
			v.accepted = true
		case shells[base]:
			for j, a := range args {
				if a.lit == "-c" && j+1 < len(args) {
					v.merge(analyze(args[j+1].lit, bins, depth+1))
				}
			}
		}
		for _, b := range bins {
			if base == b {
				v.accepted = true
			}
		}
		if deadAfter {
			deadExit = true
		}
		if !noopCommands[base] {
			for _, a := range args {
				if strings.Contains(a.lit, "GM_ACCEPTANCE_URL") || strings.Contains(a.lit, "GM_ACCEPTANCE_ADDR") {
					v.accepted = true
				}
				if loopbackRE.MatchString(a.lit) {
					v.accepted = true
				}
			}
		}
	}
	for _, s := range subs {
		v.merge(analyze(s, bins, depth+1))
	}
	return v
}

// Vacuous says that an acceptance root test cannot prove anything: no
// statement that will run is a probe (go build, vet or test with a package
// pattern; curl; a built binary; a command given the acceptance address or a
// loopback address), or something forbidden runs (go run, install, get or
// generate anywhere in the text, a binary by a path outside the bin
// directory, or rm, mv, cp, chmod or ln on the bin directory).
func Vacuous(cmd string, bins []string) bool {
	if goRefuseRE.MatchString(cmd) {
		return true
	}
	v := analyze(cmd, bins, 0)
	return v.refused || !v.accepted
}

var unsetRE = regexp.MustCompile("with [`'\"]?([A-Za-z_][A-Za-z0-9_]*)[`'\"]? unset")

// UnsetNames are the names in declared that a bullet's text says it runs
// without, by the exact phrase "with NAME unset" (case sensitive), in the
// order they appear.
func UnsetNames(text string, declared map[string]bool) []string {
	var out []string
	for _, m := range unsetRE.FindAllStringSubmatch(text, -1) {
		if declared[m[1]] {
			out = append(out, m[1])
		}
	}
	return out
}
