package planner

// Go acceptance tests: a narrative acceptance bullet (one that does not begin
// with a command in backticks) is proven by a Go test that talks to the built
// server over HTTP, not by a long shell script. The plan says how the executor
// starts that server (Serve); the Coverage stage fixes the bullet's root test
// command, and the Test-writer stage writes acceptance/<id>_test.go after
// approval (testwriter.go).

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// Serve is how the executor starts the built server once for the Go acceptance
// tests. Command runs with sh -c from the repository root with the acceptance
// environment (GM_ACCEPTANCE_ADDR is the address to listen on, GM_ACCEPTANCE_URL
// its URL); Ready is the path polled on that URL until it answers.
type Serve struct {
	Command string `json:"command"`
	Ready   string `json:"ready"`
}

// check refuses a Serve that is not one line of command and a path. The error
// quotes nothing.
func (s *Serve) check() error {
	bad := func(v string, max int) bool {
		return strings.TrimSpace(v) == "" || len(v) > max || strings.ContainsAny(v, "\n\r\x00")
	}
	if bad(s.Command, 400) || bad(s.Ready, 200) || !strings.HasPrefix(s.Ready, "/") || strings.ContainsAny(s.Ready, " \t") {
		return errors.New("coverage reply: serve needs a one-line command and a ready path that starts with /")
	}
	return nil
}

// isNarrative says an acceptance bullet is prose: it does not begin with a
// command in backticks.
func isNarrative(q Requirement) bool {
	return q.Kind == ReqAcceptance && !strings.HasPrefix(strings.TrimSpace(q.Text), "`")
}

// goBullet is one narrative acceptance bullet proven by a Go test.
type goBullet struct {
	Requirement string // A8
	Func        string // TestA8
	File        string // acceptance/a8_test.go
	Command     string // the root test command
}

// AcceptanceDir is the package of the Go acceptance tests, and AcceptanceTag
// the build tag that keeps them out of the plain go test ./... runs.
const (
	AcceptanceDir = "acceptance"
	AcceptanceTag = "acceptance"
)

// goBullets are the narrative acceptance bullets, in brief order, when the plan
// declares a server; without one the bullets keep their shell commands.
func goBullets(reqs []Requirement, serve *Serve) []goBullet {
	if serve == nil {
		return nil
	}
	var out []goBullet
	for _, q := range reqs {
		if !isNarrative(q) {
			continue
		}
		fn := "Test" + q.ID
		out = append(out, goBullet{
			Requirement: q.ID, Func: fn,
			File:    path.Join(AcceptanceDir, strings.ToLower(q.ID)+"_test.go"),
			Command: GoAcceptanceCommand(fn),
		})
	}
	return out
}

// GoAcceptanceCommand is the root test command of a Go acceptance test.
func GoAcceptanceCommand(fn string) string {
	return fmt.Sprintf("go test -tags %s ./%s -run '^%s$' -count=1 -v", AcceptanceTag, AcceptanceDir, fn)
}

// withGoAcceptance fixes the root test of every Go acceptance bullet: the
// command is the harness's, whatever the model wrote, and a bullet the reply
// gave no root test gets one. The model's name, given and expect are kept.
func (r CoverageReply) withGoAcceptance(reqs []Requirement) CoverageReply {
	bullets := goBullets(reqs, r.Serve)
	if len(bullets) == 0 {
		return r
	}
	by := map[string]goBullet{}
	for _, b := range bullets {
		by[b.Requirement] = b
	}
	done := map[string]bool{}
	out := CoverageReply{Map: r.Map, Serve: r.Serve, RootTests: []RootTest{}}
	for _, t := range r.RootTests {
		b, ok := by[t.Requirement]
		if !ok {
			out.RootTests = append(out.RootTests, t)
			continue
		}
		if done[b.Requirement] {
			continue
		}
		done[b.Requirement] = true
		t.Command = b.Command
		out.RootTests = append(out.RootTests, t)
	}
	for _, b := range bullets {
		if !done[b.Requirement] {
			out.RootTests = append(out.RootTests, RootTest{Requirement: b.Requirement, Name: b.Func,
				Given: "the built server is running", Expect: "the test passes", Command: b.Command})
		}
	}
	return out
}
