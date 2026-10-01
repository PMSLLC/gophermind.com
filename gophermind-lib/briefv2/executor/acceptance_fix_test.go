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
		"go install":     `go install ./cmd/greeter && curl -sf "$GM_ACCEPTANCE_URL"`,
		"go get":         `go get example.com/x && curl -sf "$GM_ACCEPTANCE_URL"`,
		"go generate":    `go generate ./... && curl -sf "$GM_ACCEPTANCE_URL"`,
		"rm the bin dir": `rm -f "$GM_ACCEPTANCE_BIN"/greeter; curl -sf "$GM_ACCEPTANCE_URL"`,
		"cp over a bin":  `cp /bin/ls "$GM_ACCEPTANCE_BIN/greeter"; curl -sf "$GM_ACCEPTANCE_URL"`,
		"comments only":  "# nothing here\n",
		"go build alone": `go build`,
		"relative path":  `./greeter --addr "$GM_ACCEPTANCE_ADDR" & curl -sf "$GM_ACCEPTANCE_URL"`,
		"absolute path":  `/tmp/x/greeter --addr "$GM_ACCEPTANCE_ADDR" & curl -sf "$GM_ACCEPTANCE_URL"`,
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
		"go build all":           `go build ./...`,
		"go vet all":             `go vet ./...`,
		"go test all":            `go test ./...`,
		"go test tags":           `go test ./... -tags integration`,
		"go test a path":         `go test ./internal/db`,
		"curl elsewhere":         `curl -sf https://example.com/`,
		"go build then curl":     `go build -o /tmp/x ./cmd/greeter; curl -sf "$GM_ACCEPTANCE_URL"`,
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

	t.Run("a built binary name is a server reference when the names are known", func(t *testing.T) {
		cmd := `venture-server migrate up && venture-server seed-templates`
		if _, err := mapAcceptance(cmdCov(cmd)); err == nil {
			t.Error("a bare program was accepted with no built binary named like it")
		}
		reqs, cov := cmdCov(cmd)
		if _, err := mapAcceptance(reqs, cov, "venture-server"); err != nil {
			t.Errorf("the built binary by name was refused: %v", err)
		}
		if got := vacuousBullets(mustPlan(t, cmd), []string{"greeter"}); len(got) != 1 {
			t.Errorf("a program that is not a built binary was accepted at finish: %v", got)
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
		needDB := func(rc *runCtx, h *hybridChecker) {
			bullet(t, rc, "A2").req.Text += " with TEST_DATABASE_URL set"
		}
		rep, err, _ := g.accRun(t, goodScript(g), g.fastChecker(), needDB, nil)
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
		needDB := func(rc *runCtx, h *hybridChecker) {
			bullet(t, rc, "A2").req.Text += " with TEST_DATABASE_URL set"
		}
		rep, err, _ := g.accRun(t, goodScript(g), g.fastChecker(), needDB, nil)
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

// aivsBullets are the twelve acceptance commands of the AI Venture Studio
// brief as the planner would write them.
var aivsBullets = []string{
	`go build ./...`,
	`go vet ./...`,
	`env -u TEST_DATABASE_URL go test ./...`,
	`go test ./... -tags integration`,
	"venture-server migrate up && venture-server seed-templates && venture-server migrate up && venture-server seed-templates",
	"venture-server serve & pid=$!\ni=0; until curl -s localhost:8080/healthz | grep -qx ok || [ $i -ge 50 ]; do i=$((i+1)); sleep 0.1; done\ncurl -s localhost:8080/healthz | grep -qx ok; rc=$?; kill $pid; exit $rc",
	"venture-server serve & pid=$!\nsleep 1\ntest \"$(curl -s localhost:8080/v1/openapi.json | jq '.paths | length')\" -ge 90; rc=$?; kill $pid; exit $rc",
	"venture-server fake-llm & llm=$!\nventure-server serve & pid=$!\nsleep 1\ncurl -s -N \"$GM_ACCEPTANCE_URL/v1/companies/1/stream\" > \"$TMPDIR/sse\" &\ncurl -s -X POST \"$GM_ACCEPTANCE_URL/v1/signup\" -d '{}'\nkill $pid $llm",
	"venture-server serve & pid=$!\nsleep 1\ncurl -s -X POST localhost:8080/v1/revenue-streams -d '{}' | jq .id\nkill $pid",
	"venture-server serve & pid=$!\nsleep 1\ncurl -s localhost:8080/ownership?as_of=2020-01-01 | jq .\nkill $pid",
	"STUDIO_FAKE_NOW=2030-01-01T00:00:00Z venture-server serve & pid=$!\nsleep 1\ncurl -s localhost:8080/accountability | jq .\nkill $pid",
	"venture-server serve & pid=$!\nsleep 1\ntest \"$(curl -s -o /dev/null -w '%{http_code}' localhost:8080/v1/ventures/other)\" = 404; rc=$?; kill $pid; exit $rc",
}

func mustPlan(t *testing.T, cmd string) acceptPlan {
	t.Helper()
	reqs, cov := cmdCov("curl http://127.0.0.1:1/") // accepted at start
	cov.RootTests[0].Command = cmd
	var p acceptPlan
	for _, rq := range reqs {
		p.acceptance = append(p.acceptance, planBullet{req: rq, tests: cov.RootTests})
	}
	return p
}

func TestAcceptanceAIVSBulletsAreAccepted(t *testing.T) {
	t.Parallel()
	if len(aivsBullets) != 12 {
		t.Fatalf("%d bullet commands, want 12", len(aivsBullets))
	}
	for i, cmd := range aivsBullets {
		reqs, cov := cmdCov(cmd)
		if _, err := mapAcceptance(reqs, cov, "venture-server"); err != nil {
			t.Errorf("bullet %d was refused: %v (%q)", i+1, err, cmd)
		}
		if got := vacuousBullets(mustPlan(t, cmd), []string{"venture-server"}); len(got) != 0 {
			t.Errorf("bullet %d is vacuous at finish: %q", i+1, cmd)
		}
	}
}

func TestAcceptanceUnsetPhrase(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, goodScript(g), false)
	plan := oneBullets(`test -z "$GREETER_TOKEN" && go vet ./...`)
	// without the phrase the declared secret is in the environment and the command fails
	if _, failed, err := rc.acceptanceRun(context.Background(), plan, 0); err != nil || len(failed) != 1 {
		t.Fatalf("without the phrase: failed %v, %v", failed, err)
	}
	plan.acceptance[0].req.Text = "`go vet ./...` reports nothing with `GREETER_TOKEN` unset"
	file, failed, err := rc.acceptanceRun(context.Background(), plan, 0)
	if err != nil || len(failed) != 0 {
		t.Fatalf("with the phrase: failed %v, %v", failed, err)
	}
	if got := file.Bullets[0].Commands[0].Command; got != "root test A1 #1 (unset GREETER_TOKEN)" {
		t.Errorf("proof id = %q", got)
	}
	// conservative: wrong case, an undeclared name, or another wording unset nothing
	for _, text := range []string{"with greeter_token unset", "with NOT_DECLARED unset", "GREETER_TOKEN unset", "with GREETER_TOKEN removed"} {
		if got := rc.unsetNames(text); len(got) != 0 {
			t.Errorf("unsetNames(%q) = %v", text, got)
		}
	}
}

// go build, go vet and go test as acceptance commands, then the final check
// after them, with the real runner for everything.
func TestAcceptanceGoCommandsAndFinalCheck(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	g.wire(goodScript(g))
	hook := func(rc *runCtx) {
		setCommand(t, rc, "A1", "go build ./... && go vet ./...")
		setCommand(t, rc, "A2", "go test ./... -count=1")
	}
	rep, err := run(context.Background(), g.options(), runFlags{afterStart: hook})
	if err != nil || rep.Status != "verified" || rep.Acceptance.Passed != 2 {
		t.Fatalf("run = %s (%s), %v, %v", rep.Status, rep.StopReason, rep.Failures, err)
	}
	raw, _ := os.ReadFile(filepath.Join(g.runDir, "acceptance.json"))
	if !strings.Contains(string(raw), `"complete": true`) {
		t.Error("acceptance.json is not complete")
	}
}

// A secret URL is dialled only when a bullet that needs it names it.
func TestAcceptanceEnvironmentOnlyWhenReferenced(t *testing.T) {
	t.Parallel()
	g := newRig(t, func(o *rigOpts) {
		o.BriefEdit = func(s string) string {
			return strings.Replace(s, "secrets:\n", "secrets:\n  - name: TEST_DATABASE_URL\n    purpose: \"Postgres\"\n", 1)
		}
	})
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := strings.TrimPrefix(l.Addr().String(), "127.0.0.1:")
	l.Close()
	if err := g.secrets.(*memSecrets).Set(vault.RunScope(g.id), "TEST_DATABASE_URL", "postgres://u:p@127.0.0.1:"+port+"/db"); err != nil {
		t.Fatal(err)
	}
	// no bullet names it: the unreachable database is not this run's business
	rep, err, _ := g.accRun(t, goodScript(g), g.fastChecker(), nil, nil)
	if err != nil || rep.Status != "verified" {
		t.Fatalf("unreferenced: %s (%s), %v, %v", rep.Status, rep.StopReason, rep.Failures, err)
	}
	// a bullet that says "with TEST_DATABASE_URL unset" does not need it either
	g2 := newRig(t, func(o *rigOpts) {
		o.BriefEdit = func(s string) string {
			return strings.Replace(s, "secrets:\n", "secrets:\n  - name: TEST_DATABASE_URL\n    purpose: \"Postgres\"\n", 1)
		}
	})
	if err := g2.secrets.(*memSecrets).Set(vault.RunScope(g2.id), "TEST_DATABASE_URL", "postgres://u:p@127.0.0.1:"+port+"/db"); err != nil {
		t.Fatal(err)
	}
	edit := func(rc *runCtx, h *hybridChecker) {
		bullet(t, rc, "A1").req.Text = "go test ./... passes with `TEST_DATABASE_URL` unset"
	}
	if rep, err, _ := g2.accRun(t, goodScript(g2), g2.fastChecker(), edit, nil); err != nil || rep.Status != "verified" {
		t.Fatalf("unset: %s (%s), %v, %v", rep.Status, rep.StopReason, rep.Failures, err)
	}
}
