package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/runner"
	"gophermind/gophermind-lib/briefv2/vault"
)

func cmdCov(cmd string) (reqs []planner.Requirement, cov planner.CoverageFile) {
	reqs = []planner.Requirement{{ID: "A1", Kind: planner.ReqAcceptance, Text: "x"}}
	cov.RootTests = []planner.RootTest{{Requirement: "A1", Name: "t", Command: cmd}}
	return
}

func TestAcceptanceVacuousRootTests(t *testing.T) {
	t.Parallel()
	refused := map[string]string{
		"true":           "true",
		"colon":          ":",
		"exit 0":         "exit 0",
		"echo":           `echo "$GM_ACCEPTANCE_URL"`,
		"printf":         `printf ok`,
		"sleep":          `sleep 1`,
		"test alone":     `test -n "$GM_ACCEPTANCE_URL"`,
		"bracket alone":  `[ -n "$GM_ACCEPTANCE_URL" ]`,
		"several no-ops": "true; echo hi\nexit 0",
		"go run":         `go run ./cmd/greeter & curl -sf "$GM_ACCEPTANCE_URL"`,
		"go build":       `go build -o /tmp/x ./cmd/greeter; curl -sf "$GM_ACCEPTANCE_URL"`,
		"go install":     `go install ./cmd/greeter && curl -sf "$GM_ACCEPTANCE_URL"`,
		"go test":        `go test ./... && curl -sf "$GM_ACCEPTANCE_URL"`,
		"relative path":  `./greeter --addr "$GM_ACCEPTANCE_ADDR" & curl -sf "$GM_ACCEPTANCE_URL"`,
		"absolute path":  `/tmp/x/greeter --addr "$GM_ACCEPTANCE_ADDR" & curl -sf "$GM_ACCEPTANCE_URL"`,
		"no server":      `curl -sf https://example.com/`,
		"no url at all":  `grep -q x README.md`,
	}
	for name, cmd := range refused {
		t.Run("refuses "+name, func(t *testing.T) {
			reqs, cov := cmdCov(cmd)
			_, err := mapAcceptance(reqs, cov)
			var v *vacuousError
			if err == nil || !errors.As(err, &v) || !strings.Contains(err.Error(), "A1") {
				t.Fatalf("mapAcceptance(%q) = %v, want a vacuous error naming A1", cmd, err)
			}
			if strings.Contains(err.Error(), "curl") || strings.Contains(err.Error(), "example") {
				t.Error("the error quotes the command")
			}
		})
	}
	accepted := map[string]string{
		"curl with the base url": `curl -sf "$GM_ACCEPTANCE_URL/hello" | grep -q Hello`,
		"binary by name":         `greeter --addr "$GM_ACCEPTANCE_ADDR" & pid=$!; curl -sf "$GM_ACCEPTANCE_URL/hello"; kill $pid`,
		"echo then curl":         "echo start\ncurl -sf \"$GM_ACCEPTANCE_URL/hello\"",
		"system curl by path":    `/usr/bin/curl -sf "$GM_ACCEPTANCE_URL/hello"`,
		"from the bin dir":       `"$GM_ACCEPTANCE_BIN"/greeter --addr "$GM_ACCEPTANCE_ADDR" & curl -sf "$GM_ACCEPTANCE_URL"`,
	}
	for name, cmd := range accepted {
		t.Run("accepts "+name, func(t *testing.T) {
			reqs, cov := cmdCov(cmd)
			if _, err := mapAcceptance(reqs, cov); err != nil {
				t.Fatalf("mapAcceptance(%q) = %v", cmd, err)
			}
		})
	}
	// constraints are not probes of the server
	reqs := []planner.Requirement{{ID: "C1", Kind: planner.ReqConstraint, Text: "x"}}
	cov := planner.CoverageFile{RootTests: []planner.RootTest{{Requirement: "C1", Name: "t", Command: "go vet ./..."}}}
	if _, err := mapAcceptance(reqs, cov); err != nil {
		t.Errorf("a constraint command was refused: %v", err)
	}

	t.Run("at finish a command must run a built binary or use the base url", func(t *testing.T) {
		p, err := mapAcceptance(cmdCov(`curl -sf http://127.0.0.1:1/hello`))
		if err != nil {
			t.Fatal(err)
		}
		if got := vacuousBullets(p, []string{"greeter"}); len(got) != 1 || got[0] != "A1" {
			t.Errorf("vacuousBullets = %v, want A1", got)
		}
	})

	t.Run("through Run the stop comes before any call", func(t *testing.T) {
		g := newRig(t)
		editCoverage(t, g.runDir, func(c *planner.CoverageFile) {
			for i := range c.RootTests {
				if c.RootTests[i].Requirement == "A2" {
					c.RootTests[i].Command = "true"
				}
			}
		})
		g.wire(goodScript(g))
		rep, err := run(context.Background(), g.options(), runFlags{afterStart: useChecker(g.fastChecker())})
		if err != nil || rep.Status != "failed" || rep.StopReason != "acceptance_vacuous" {
			t.Fatalf("run = %s (%s), %v", rep.Status, rep.StopReason, err)
		}
		if n := len(g.fake.Requests()); n != 0 {
			t.Errorf("%d model calls before the stop", n)
		}
		if !strings.Contains(strings.Join(rep.Failures, " "), "A2") {
			t.Errorf("failures %v do not name A2", rep.Failures)
		}
	})
}

func TestAcceptanceBinaryTamperFails(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, goodScript(g), false)
	if err := rc.buildBinaries(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(rc.bins) != 1 || len(rc.bins["greeter"]) != 64 {
		t.Fatalf("bins = %v", rc.bins)
	}
	_, _, err := rc.acceptanceRun(context.Background(), oneBullets(`echo tampered > "$GM_ACCEPTANCE_BIN/greeter"; exit 0`), 0)
	se, ok := stopOf(err)
	if !ok || se.Status != "failed" || se.Reason != "acceptance_tampered" {
		t.Fatalf("acceptanceRun error = %v, want acceptance_tampered", err)
	}
	raw, _ := os.ReadFile(filepath.Join(g.runDir, "acceptance.json"))
	var f AcceptanceFile
	if err := json.Unmarshal(raw, &f); err != nil || len(f.Binaries) != 1 || f.Binaries[0].Name != "greeter" || f.Binaries[0].SHA256 != rc.bins["greeter"] {
		t.Errorf("acceptance.json binaries = %+v (%v)", f.Binaries, err)
	}
	if f.Complete {
		t.Error("a tampered round is marked complete")
	}
}

func TestAcceptanceProofHasCommandHash(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, goodScript(g), false)
	file, _, err := rc.acceptanceRun(context.Background(), oneBullets(`printf hi`), 0)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("printf hi"))
	c := file.Bullets[0].Commands[0]
	if c.CommandSHA != hex.EncodeToString(sum[:]) || c.Command != "root test A1 #1" || !file.Complete || file.SchemaVersion != 2 {
		t.Errorf("proof = %+v complete %v schema %d", c, file.Complete, file.SchemaVersion)
	}
	raw, _ := os.ReadFile(filepath.Join(g.runDir, "acceptance.json"))
	if strings.Contains(string(raw), "printf hi") {
		t.Error("the command text is in acceptance.json")
	}
}

func TestAcceptanceStrayWriteFails(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, goodScript(g), false)
	_, _, err := rc.acceptanceRun(context.Background(), oneBullets(`echo x > stray-from-acceptance.txt`), 0)
	se, ok := stopOf(err)
	if !ok || se.Reason != "acceptance_stray" || se.Status != "failed" {
		t.Fatalf("error = %v, want acceptance_stray", err)
	}
	if strings.Contains(se.Message, "stray-from-acceptance") {
		t.Error("the message names the path")
	}
	if fileExists(filepath.Join(g.repo, "stray-from-acceptance.txt")) {
		t.Error("the stray file was not removed")
	}
}

func TestAcceptanceLeakIsReported(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, goodScript(g), false)
	cmd := `perl -MPOSIX -e 'if (fork) { exit 0 } else { POSIX::setsid(); open F, ">$ENV{GM_ACCEPTANCE_PIDFILE}"; print F $$; close F; exec "sleep", "300" }'
until [ -s "$GM_ACCEPTANCE_PIDFILE" ]; do sleep 0.05; done`
	_, _, err := rc.acceptanceRun(context.Background(), oneBullets(cmd), 0)
	se, ok := stopOf(err)
	if !ok || se.Reason != "acceptance_leak" || se.Status != "failed" {
		t.Fatalf("error = %v, want acceptance_leak", err)
	}
	if !gone(readPID(t, filepath.Join(rc.scratch, "acceptance.pid"))) {
		t.Error("the escaped process was not killed")
	}
}

func TestAcceptanceEnvIsNodeLessAndCarriesAddress(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, goodScript(g), false)
	env, err := rc.env("acceptance", envAcceptance)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range env {
		if strings.HasPrefix(e, "GOPHERMIND_NODE=") {
			t.Errorf("the acceptance environment carries %s", e)
		}
	}
	_, _, err = rc.acceptanceRun(context.Background(), oneBullets(`printf '%s %s' "$GM_ACCEPTANCE_ADDR" "$GM_ACCEPTANCE_URL" > "$TMPDIR/addr"`), 0)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(rc.scratch, "addr"))
	f := strings.Fields(string(raw))
	if len(f) != 2 || !strings.HasPrefix(f[0], "127.0.0.1:") || f[1] != "http://"+f[0] {
		t.Errorf("address and url = %q", raw)
	}
}

func TestAcceptanceEnvironmentPreflight(t *testing.T) {
	t.Parallel()
	dbBrief := func(o *rigOpts) {
		o.BriefEdit = func(s string) string {
			return strings.Replace(s, "secrets:\n", "secrets:\n  - name: TEST_DATABASE_URL\n    purpose: \"Postgres for the acceptance checks\"\n", 1)
		}
	}
	setURL := func(g *rig, u string) {
		if err := g.secrets.(*memSecrets).Set(vault.RunScope(g.id), "TEST_DATABASE_URL", u); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("an unreachable loopback database stops the run", func(t *testing.T) {
		g := newRig(t, dbBrief)
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := strings.TrimPrefix(l.Addr().String(), "127.0.0.1:")
		l.Close() // nothing listens now
		setURL(g, "postgres://user:SECRET-PW@127.0.0.1:"+port+"/db")
		rep, err, _ := g.accRun(t, goodScript(g), g.fastChecker(), nil, nil)
		if err != nil || rep.Status != "failed" || rep.StopReason != "acceptance_environment" {
			t.Fatalf("run = %s (%s), %v, %v", rep.Status, rep.StopReason, rep.Failures, err)
		}
		all := strings.Join(rep.Failures, " ")
		if !strings.Contains(all, "TEST_DATABASE_URL") || strings.Contains(all, "SECRET-PW") || strings.Contains(all, port) {
			t.Errorf("failures = %q, want the secret name only", all)
		}
		if n := len(g.leafCalls("fn-bye")) + len(g.leafCalls("fn-serve")); n != 2 {
			t.Errorf("%d implement calls, want 2 (no repair)", n)
		}
		if hasCanary(t, g, "SECRET-PW") {
			t.Error("the database password reached a store")
		}
	})
	t.Run("a reachable database lets the run go on", func(t *testing.T) {
		g := newRig(t, dbBrief)
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		setURL(g, "postgres://user:pw@"+l.Addr().String()+"/db")
		rep, err, _ := g.accRun(t, goodScript(g), g.fastChecker(), nil, nil)
		if err != nil || rep.Status != "verified" {
			t.Fatalf("run = %s (%s), %v, %v", rep.Status, rep.StopReason, rep.Failures, err)
		}
	})
	t.Run("output never decides it", func(t *testing.T) {
		g := newRig(t, dbBrief)
		script := goodScript(g)
		withRepairs(script, "fn-bye", 1, "MARKER-BYE")
		fc := g.fastChecker()
		fc.LeafScript["fn-bye"] = []runner.Verdict{passVerdict(), passVerdict()}
		l, _ := net.Listen("tcp", "127.0.0.1:0")
		defer l.Close()
		setURL(g, "postgres://u:p@"+l.Addr().String()+"/db")
		edit := func(rc *runCtx, h *hybridChecker) {
			setCommand(t, rc, "A2", vr("grep -q MARKER-BYE "+rc.plan.Leaf("fn-bye").File+" || { echo 'postgres database connection refused i/o timeout'; exit 1; }"))
			bullet(t, rc, "A2").nodes = []string{"fn-bye"}
		}
		rep, err, _ := g.accRun(t, script, fc, edit, nil)
		if err != nil || rep.Status != "verified" || rep.Repairs != 1 {
			t.Fatalf("run = %s (%s) repairs %d, %v, %v", rep.Status, rep.StopReason, rep.Repairs, rep.Failures, err)
		}
	})
}

// finish runs build, vet, the race tests, go mod verify and the scan again
// after acceptance.
func TestFinishChecksAgainAfterAcceptance(t *testing.T) {
	t.Parallel()
	t.Run("order", func(t *testing.T) {
		g := newRig(t)
		rep, err, log := g.accRun(t, goodScript(g), g.fastChecker(), nil, nil)
		if err != nil || rep.Status != "verified" {
			t.Fatalf("run = %s (%s), %v, %v", rep.Status, rep.StopReason, rep.Failures, err)
		}
		lastAccept, mod := log.index("accept", true), log.index("modverify", false)
		bv, tst := log.index("buildvet", true), log.index("test", true)
		if !(lastAccept < bv && bv < mod && lastAccept < tst && tst < mod) {
			t.Fatalf("the final checks do not sit between acceptance and go mod verify: %v", log.list())
		}
	})
	t.Run("a failing final test ends the run", func(t *testing.T) {
		g := newRig(t)
		fc := g.fastChecker()
		// the last wave's check passes, the one after acceptance fails
		fc.RepoScript.Test["./..."] = []runner.Verdict{passVerdict(), failVerdict("test_fail", "x")}
		rep, err, log := g.accRun(t, goodScript(g), fc, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Status != "failed" || rep.StopReason != "final_test" {
			t.Fatalf("report = %s (%s), %v", rep.Status, rep.StopReason, rep.Failures)
		}
		if log.count("modverify") != 0 || strings.Contains(g.gitCmd("log", "--format=%s", "main"), "gm(run):") {
			t.Error("a run with a failing final check went on to land")
		}
	})
}

// One end-to-end run with the real runner for every check and command.
func TestAcceptanceEndToEndRealChecker(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	g.wire(goodScript(g))
	rep, err := run(context.Background(), g.options(), runFlags{})
	if err != nil || rep.Status != "verified" {
		t.Fatalf("run = %s (%s), %v, %v", rep.Status, rep.StopReason, rep.Failures, err)
	}
	if rep.Acceptance.Passed != 2 || rep.Acceptance.Total != 2 || rep.Landing == nil || rep.Landing.Commit == "" {
		t.Errorf("acceptance %+v landing %+v", rep.Acceptance, rep.Landing)
	}
	if !strings.Contains(g.gitCmd("log", "-1", "--format=%s", "main"), "Acceptance passed 2 of 2") {
		t.Error("the final commit does not carry the proof line")
	}
}
