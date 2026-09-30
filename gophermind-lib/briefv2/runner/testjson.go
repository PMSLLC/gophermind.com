package runner

import (
	"bufio"
	"encoding/json"
	"io"
	"regexp"
	"sort"
	"strings"
)

// TestReport is what a go test -json stream says. Names are safe to persist only
// after SafeName.
type TestReport struct {
	Events      int             // test-level events seen (run, pass, fail, skip with a Test name)
	Passed      map[string]bool // by "pkg.Test/sub"
	Failed      []string        // sorted, names only, as "pkg.Test/sub"; leaf failures (a parent of a failing subtest is dropped)
	Panicked    bool
	TimedOut    bool
	BuildFailed bool
	PkgFailed   bool // a package-level fail with no failing test (TestMain exit, race report)

	names []string // Failed without the package prefix, parallel to Failed
	text  string   // decoded output events and raw lines, for locations and the next prompt
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

// ParseTestJSON reads a go test -json stream and tolerates non-JSON lines.
func ParseTestJSON(r io.Reader) TestReport {
	rep := TestReport{Passed: map[string]bool{}}
	type key struct{ pkg, test string }
	running := map[key]bool{}
	failed := map[key]bool{}
	pkgFailed := map[string]bool{}
	pkgHasFailedTest := map[string]bool{}
	var text strings.Builder

	scan := func(line string) {
		if strings.Contains(line, "[build failed]") {
			rep.BuildFailed = true
		}
		if strings.Contains(line, "WARNING: DATA RACE") {
			rep.PkgFailed = true
		}
		if strings.HasPrefix(line, "panic: test timed out") {
			rep.TimedOut = true
		} else if strings.HasPrefix(line, "panic: ") {
			rep.Panicked = true
		}
	}

	br := bufio.NewReaderSize(r, 64*1024)
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			trimmed := strings.TrimRight(line, "\r\n")
			var e jsonEvent
			if len(trimmed) > 0 && trimmed[0] == '{' && json.Unmarshal([]byte(trimmed), &e) == nil && e.Action != "" {
				k := key{e.Package, e.Test}
				if e.FailedBuild != "" {
					rep.BuildFailed = true
				}
				switch e.Action {
				case "output":
					text.WriteString(e.Output)
					for _, l := range strings.Split(e.Output, "\n") {
						scan(l)
					}
				case "run", "pass", "fail", "skip":
					if e.Test != "" {
						rep.Events++
						switch e.Action {
						case "run":
							running[k] = true
						case "pass":
							delete(running, k)
							rep.Passed[e.Package+"."+e.Test] = true
						case "fail":
							delete(running, k)
							failed[k] = true
							pkgHasFailedTest[e.Package] = true
						case "skip":
							delete(running, k)
						}
					} else if e.Action == "fail" {
						pkgFailed[e.Package] = true
					}
				}
			} else {
				text.WriteString(line)
				scan(trimmed)
			}
		}
		if err != nil {
			break
		}
	}

	if rep.Panicked || rep.TimedOut {
		for k := range running {
			failed[k] = true
			pkgHasFailedTest[k.pkg] = true
		}
	}
	for p := range pkgFailed {
		if !pkgHasFailedTest[p] {
			rep.PkgFailed = true
		}
	}
	// keep leaf failures only: drop a name that is the parent of another failed name
	var all []key
	for k := range failed {
		all = append(all, k)
	}
	for _, k := range all {
		parent := false
		for _, o := range all {
			if o.pkg == k.pkg && strings.HasPrefix(o.test, k.test+"/") {
				parent = true
				break
			}
		}
		if !parent {
			rep.Failed = append(rep.Failed, k.pkg+"."+k.test)
			rep.names = append(rep.names, k.test)
		}
	}
	sort.Strings(rep.Failed)
	sort.Strings(rep.names)
	rep.text = text.String()
	return rep
}
