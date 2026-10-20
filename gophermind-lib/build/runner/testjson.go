package runner

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"hash"
	"io"
	"regexp"
	"sort"
	"strings"
)

const (
	maxJSONLine   = 1 << 20 // a longer line is dropped, not buffered
	maxTrackTests = 100000  // distinct tests kept; beyond it the report says Overflow
)

// TestReport is what a go test -json stream says.
//
// Decisions come from the structured events (per-test and per-package actions and the
// FailedBuild field). The text markers (Panicked, TimedOut, RaceWarning, RawBuildFailed)
// are evidence only: check.go uses them to name the kind of a failure that the structure
// and the exit code already established, never to create one.
type TestReport struct {
	Events      int               // test-level events seen (run, pass, fail, skip with a Test name)
	Passed      map[string]bool   // by "pkg.Test/sub"; passed and never failed
	Failed      []string          // sorted "pkg.Test/sub"; leaf failures (a parent of a failing subtest is dropped)
	Pkgs        map[string]string // package to "pass", "fail" or "skip"; fail is sticky
	PkgFailed   bool              // a package-level fail with no failing test in it (TestMain exit, race report, build)
	BuildFailed bool              // a fail event carried FailedBuild (structured)
	Structured  bool              // at least one JSON event was seen
	Overflow    bool              // more than maxTrackTests distinct tests; the report is incomplete
	Dropped     int               // lines longer than maxJSONLine that were skipped

	Panicked       bool // text: an output line began with "panic: "
	TimedOut       bool // text: an output line began with "panic: test timed out"
	RaceWarning    bool // text: WARNING: DATA RACE
	RawBuildFailed bool // text: "[build failed]" outside a structured event

	names  []string        // test names of Failed without the package
	qnames []string        // package-qualified names of Failed
	tops   map[string]bool // top-level test names (no "/") that passed and never failed, any package
	out    Output          // decoded output: the first textCap bytes, with the size and hash of all of it
}

type jsonEvent struct {
	Action      string
	Package     string
	Test        string
	Output      string
	FailedBuild string
}

var safeNameRE = regexp.MustCompile(`^[A-Za-z0-9_./-]{1,80}$`)

// SafeName returns name when it matches ^[A-Za-z0-9_./-]{1,80}$, else "(unnamed)".
// Test and subtest names are model-written and reach failure_reason.
func SafeName(name string) string {
	if safeNameRE.MatchString(name) {
		return name
	}
	return "(unnamed)"
}

// SafeQualified returns "pkg.test" for persistence. The test name must be safe on its own;
// the package must match the SafeName characters and is trimmed from the left to fit 80.
func SafeQualified(pkg, test string) string {
	t := SafeName(test)
	if t == "(unnamed)" || !safeNameRE.MatchString(pkg) {
		return SafeName(pkg + "." + test)
	}
	if budget := 80 - len(t) - 1; budget >= 1 {
		if len(pkg) > budget {
			pkg = pkg[len(pkg)-budget:]
		}
		return pkg + "." + t
	}
	return t
}

type testKey struct{ pkg, test string }

type testState struct{ running, passed, failed bool }

// streamParser reads a go test -json stream line by line as the child writes it. It keeps
// one small state per test, bounds each line and the number of tests, and never stores
// more than textCap bytes of decoded output.
type streamParser struct {
	maxLine, maxTests, textCap int

	buf         []byte
	discarding  bool
	tests       map[testKey]*testState
	pkgs        map[string]string
	pkgFailTest map[string]bool
	events      int
	rep         TestReport
	text        []byte
	textSize    int
	textHash    hash.Hash
}

func newStreamParser(textCap int) *streamParser {
	if textCap <= 0 {
		textCap = defaultOutputCap
	}
	return &streamParser{
		maxLine: maxJSONLine, maxTests: maxTrackTests, textCap: textCap,
		textHash: sha256.New(), tests: map[testKey]*testState{}, pkgs: map[string]string{}, pkgFailTest: map[string]bool{},
	}
}

func (p *streamParser) Write(b []byte) (int, error) {
	n := len(b)
	for len(b) > 0 {
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			p.push(b)
			break
		}
		p.push(b[:i])
		p.endLine()
		b = b[i+1:]
	}
	return n, nil
}

func (p *streamParser) push(b []byte) {
	if p.discarding {
		return
	}
	if len(p.buf)+len(b) > p.maxLine {
		p.discarding = true
		p.buf = p.buf[:0]
		p.rep.Dropped++
		return
	}
	p.buf = append(p.buf, b...)
}

func (p *streamParser) endLine() {
	if p.discarding {
		p.discarding = false
		return
	}
	line := string(p.buf)
	p.buf = p.buf[:0]
	p.line(line)
}

func (p *streamParser) addText(s string) {
	p.textSize += len(s)
	p.textHash.Write([]byte(s))
	if room := p.textCap - len(p.text); room > 0 {
		if len(s) > room {
			s = s[:room]
		}
		p.text = append(p.text, s...)
	}
}

func (p *streamParser) marker(line string, structured bool) {
	if strings.Contains(line, "WARNING: DATA RACE") {
		p.rep.RaceWarning = true
	}
	if !structured && strings.Contains(line, "[build failed]") {
		p.rep.RawBuildFailed = true
	}
	if strings.HasPrefix(line, "panic: test timed out") {
		p.rep.TimedOut = true
	} else if strings.HasPrefix(line, "panic: ") {
		p.rep.Panicked = true
	}
}

func (p *streamParser) state(k testKey) *testState {
	if st, ok := p.tests[k]; ok {
		return st
	}
	if len(p.tests) >= p.maxTests {
		p.rep.Overflow = true
		return nil
	}
	st := &testState{}
	p.tests[k] = st
	return st
}

func (p *streamParser) line(line string) {
	trimmed := strings.TrimRight(line, "\r")
	var e jsonEvent
	if len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal([]byte(trimmed), &e) != nil || e.Action == "" {
		p.addText(line + "\n")
		p.marker(trimmed, false)
		return
	}
	p.rep.Structured = true
	if e.Action == "output" {
		p.addText(e.Output)
		for _, l := range strings.Split(e.Output, "\n") {
			p.marker(l, true)
		}
		return
	}
	if e.Action == "fail" && e.FailedBuild != "" {
		p.rep.BuildFailed = true
	}
	switch e.Action {
	case "run", "pass", "fail", "skip":
	default:
		return
	}
	if e.Test == "" {
		if e.Action == "pass" || e.Action == "fail" || e.Action == "skip" {
			if p.pkgs[e.Package] != "fail" {
				p.pkgs[e.Package] = e.Action
			}
		}
		return
	}
	p.events++
	st := p.state(testKey{e.Package, e.Test})
	if st == nil {
		return
	}
	switch e.Action {
	case "run":
		st.running = true
	case "pass":
		st.running, st.passed = false, true
	case "fail":
		st.running, st.failed = false, true
		p.pkgFailTest[e.Package] = true
	case "skip":
		st.running = false
	}
}

func (p *streamParser) finish() TestReport {
	if len(p.buf) > 0 && !p.discarding {
		p.line(string(p.buf))
	}
	p.buf = p.buf[:0]
	rep := p.rep
	rep.Events = p.events
	rep.Passed = map[string]bool{}
	rep.tops = map[string]bool{}
	rep.Pkgs = map[string]string{}
	for k, v := range p.pkgs {
		rep.Pkgs[k] = v
	}
	var fails []testKey
	for k, st := range p.tests {
		switch {
		case st.failed || st.running: // an unfinished test means the run crashed
			fails = append(fails, k)
			p.pkgFailTest[k.pkg] = true
		case st.passed:
			rep.Passed[k.pkg+"."+k.test] = true
			if !strings.Contains(k.test, "/") {
				rep.tops[k.test] = true
			}
		}
	}
	for pkg, action := range p.pkgs {
		if action == "fail" && !p.pkgFailTest[pkg] {
			rep.PkgFailed = true
		}
	}
	for _, k := range fails {
		parent := false
		for _, o := range fails {
			if o.pkg == k.pkg && strings.HasPrefix(o.test, k.test+"/") {
				parent = true
				break
			}
		}
		if !parent {
			rep.Failed = append(rep.Failed, k.pkg+"."+k.test)
		}
	}
	sort.Strings(rep.Failed)
	for _, k := range sortedKeys(fails, rep.Failed) {
		rep.names = append(rep.names, k.test)
		rep.qnames = append(rep.qnames, SafeQualified(k.pkg, k.test))
	}
	rep.out = Output{text: p.text, size: p.textSize, truncated: p.textSize > len(p.text)}
	copy(rep.out.sum[:], p.textHash.Sum(nil))
	return rep
}

// sortedKeys returns the leaf keys named in failed, in that order.
func sortedKeys(keys []testKey, failed []string) []testKey {
	by := map[string]testKey{}
	for _, k := range keys {
		by[k.pkg+"."+k.test] = k
	}
	out := make([]testKey, 0, len(failed))
	for _, f := range failed {
		out = append(out, by[f])
	}
	return out
}

// ParseTestJSON reads a go test -json stream and tolerates non-JSON lines. It reads line
// by line with a bounded line length; see streamParser.
func ParseTestJSON(r io.Reader) TestReport {
	p := newStreamParser(0)
	_, _ = io.Copy(p, r)
	return p.finish()
}
