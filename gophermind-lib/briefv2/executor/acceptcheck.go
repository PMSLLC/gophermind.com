package executor

import (
	"regexp"
	"sort"
	"strings"
)

// vacuousError is the tripwire for acceptance root tests that cannot prove the
// server: it names bullet ids only, never a command.
type vacuousError struct{ IDs []string }

func (e *vacuousError) Error() string {
	return "executor: the root tests of " + strings.Join(e.IDs, ", ") + " do not exercise the built server (acceptance_vacuous)"
}

// noopCommands are shell words whose whole effect is nothing the server can
// be blamed for.
var noopCommands = map[string]bool{
	"true": true, ":": true, "false": true, "exit": true, "echo": true, "printf": true,
	"sleep": true, "test": true, "[": true, "[[": true,
}

var shellKeywords = map[string]bool{
	"if": true, "then": true, "else": true, "elif": true, "fi": true, "do": true, "done": true,
	"while": true, "until": true, "!": true, "{": true, "}": true, "(": true, ")": true, "time": true,
}

var (
	assignRE  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	goBuildRE = regexp.MustCompile(`(^|[\s;&|(])go\s+(run|build|install|test)(\s|$)`)
	systemDir = []string{"/usr/bin/", "/bin/", "/usr/sbin/", "/sbin/"}
	binDirRef = []string{"$GM_ACCEPTANCE_BIN/", "${GM_ACCEPTANCE_BIN}/", `"$GM_ACCEPTANCE_BIN"/`, `"${GM_ACCEPTANCE_BIN}"/`}
)

// shellSplit cuts a command at top-level separators (newline ; & |), or, with
// words set, at whitespace, leaving quotes and $( ) groups whole.
func shellSplit(cmd string, words bool) []string {
	var out []string
	var cur strings.Builder
	var single, double bool
	depth := 0
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}
	rs := []rune(cmd)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch {
		case single:
			if c == '\'' {
				single = false
			}
		case double:
			if c == '\\' && i+1 < len(rs) {
				cur.WriteRune(c)
				i++
				c = rs[i]
			} else if c == '"' {
				double = false
			}
		case c == '\\' && i+1 < len(rs):
			cur.WriteRune(c)
			i++
			c = rs[i]
		case c == '\'':
			single = true
		case c == '"':
			double = true
		case c == '(':
			depth++
		case c == ')':
			if depth > 0 {
				depth--
			}
		case depth == 0 && !words && (c == '\n' || c == ';' || c == '|' || c == '&'):
			redirect := c == '&' && ((i > 0 && (rs[i-1] == '>' || rs[i-1] == '<')) || (i+1 < len(rs) && rs[i+1] == '>'))
			if !redirect {
				flush()
				continue
			}
		case depth == 0 && words && (c == ' ' || c == '\t' || c == '\n'):
			flush()
			continue
		}
		cur.WriteRune(c)
	}
	flush()
	return out
}

// firstCommand is the command word of a simple statement: keywords and
// NAME=value words skipped. Empty for a bare assignment.
func firstCommand(stmt string) string {
	for _, w := range shellSplit(stmt, true) {
		w = strings.TrimLeft(w, "({")
		if w == "" || shellKeywords[w] || assignRE.MatchString(w) {
			continue
		}
		return w
	}
	return ""
}

var loopbackLiterals = []string{"127.0.0.1", "localhost", "[::1]"}

var (
	goRefuseRE = regexp.MustCompile(`(^|[\s;&|(])go\s+(run|install|get|generate)(\s|$)`)
	goPkgRE    = regexp.MustCompile(`(^|[\s;&|(])go\s+(build|vet|test)\b[^\n;&|]*\s\./`)
	curlRE     = regexp.MustCompile(`(^|[^A-Za-z0-9_./-])curl([^A-Za-z0-9_-]|$)`)
	binDirOpRE = regexp.MustCompile(`(^|[\s/"])bin(/|\s|"|$)`)
	mutators   = map[string]bool{"rm": true, "mv": true, "cp": true, "chmod": true, "ln": true}
)

// vacuousCommand says that an acceptance root test cannot prove anything. It
// is refused when its whole effect is a no-op, when it runs go run, install,
// get or generate, runs a binary by a path outside the built bin directory, or
// removes, moves, copies, changes or links into the bin directory. Otherwise it
// is accepted when it mentions go build, vet or test with a package pattern, a
// built binary's name (bins), curl, the acceptance address variables or a
// loopback address. bins are the main packages' directory names under cmd/.
func vacuousCommand(cmd string, bins []string) bool {
	real := false
	for _, st := range shellSplit(cmd, false) {
		c := firstCommand(st)
		if c == "" || noopCommands[c] {
			continue
		}
		real = true
		if strings.Contains(c, "/") {
			ok := false
			for _, p := range append(append([]string{}, systemDir...), binDirRef...) {
				if strings.HasPrefix(c, p) {
					ok = true
				}
			}
			if !ok {
				return true // a binary run by path, not the one built
			}
		}
		if mutators[c] && (strings.Contains(st, "GM_ACCEPTANCE_BIN") || binDirOpRE.MatchString(st)) {
			return true
		}
	}
	if !real || goRefuseRE.MatchString(cmd) {
		return true
	}
	if goPkgRE.MatchString(cmd) || curlRE.MatchString(cmd) ||
		strings.Contains(cmd, "GM_ACCEPTANCE_URL") || strings.Contains(cmd, "GM_ACCEPTANCE_ADDR") {
		return false
	}
	for _, l := range loopbackLiterals {
		if strings.Contains(cmd, l) {
			return false
		}
	}
	for _, b := range bins {
		if regexp.MustCompile(`(^|[^A-Za-z0-9_./-])` + regexp.QuoteMeta(b) + `([^A-Za-z0-9_-]|$)`).MatchString(cmd) {
			return false
		}
	}
	return true
}

// vacuousBullets is the ids of the acceptance bullets that have a vacuous root test.
func vacuousBullets(p acceptPlan, bins []string) []string {
	var ids []string
	for _, pb := range p.acceptance {
		for _, t := range pb.tests {
			if vacuousCommand(t.Command, bins) {
				ids = append(ids, pb.req.ID)
				break
			}
		}
	}
	return ids
}

var unsetRE = regexp.MustCompile("with [`'\"]?([A-Za-z_][A-Za-z0-9_]*)[`'\"]? unset")

// unsetNames are the declared secret and env names a bullet's text says it runs
// without, by the exact phrase "with NAME unset" (case sensitive).
func (rc *runCtx) unsetNames(text string) []string {
	declared := map[string]bool{}
	for _, s := range rc.plan.Brief.Front.Secrets {
		declared[s.Name] = true
	}
	for _, e := range rc.plan.Brief.Front.Env {
		declared[e.Name] = true
	}
	var out []string
	for _, m := range unsetRE.FindAllStringSubmatch(text, -1) {
		if declared[m[1]] {
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}
