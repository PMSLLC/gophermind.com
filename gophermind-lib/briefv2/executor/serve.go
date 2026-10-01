package executor

// The server of the Go acceptance tests. When the plan declares serve (a
// command and a ready path, coverage.json), the executor starts it once for the
// acceptance phase of a round, waits until the ready path answers, runs the
// bullets whose root test is a go test of the acceptance package against it
// (GM_ACCEPTANCE_URL), and kills its process group afterwards. Tests start
// nothing themselves. Simpler and sturdier than a TestMain in every package:
// one place owns the process, the readiness wait and the leak check.

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"gophermind/gophermind-lib/briefv2/acceptcheck"
	"gophermind/gophermind-lib/briefv2/runner"
)

// maxServeReady caps how long a server may take to answer its ready path.
const maxServeReady = 60 * time.Second

type serveProc struct {
	cancel context.CancelFunc
	done   chan runner.Result
	addr   string
	over   bool
}

func serveStop(msg string) *stopError {
	return &stopError{Status: "failed", Reason: "acceptance_serve", Message: "executor: " + msg}
}

// needsServe says the plan declares a server and some root test runs the
// acceptance package.
func (rc *runCtx) needsServe(p acceptPlan) bool {
	if rc.plan.Serve == nil {
		return false
	}
	for _, list := range [][]planBullet{p.acceptance, p.constraints} {
		for _, pb := range list {
			for _, t := range pb.tests {
				if gt, ok := acceptcheck.ParseGoTest(t.Command); ok && isAcceptancePkg(gt.Pkg) {
					return true
				}
			}
		}
	}
	return false
}

// startServe starts the declared server and waits for it. The error is a
// *stopError for a server that is refused, exits or never answers.
func (rc *runCtx) startServe(ctx context.Context) (*serveProc, error) {
	sv := rc.plan.Serve
	if acceptcheck.Vacuous(sv.Command, rc.binNames()) {
		return nil, serveStop("the serve command of the plan does not run a built binary (acceptance_serve)")
	}
	env, err := rc.env("acceptance", envAcceptance)
	if err != nil {
		return nil, errors.New("executor: the environment of the acceptance run could not be built")
	}
	addr, err := freeAddr()
	if err != nil {
		return nil, errors.New("executor: no free loopback port for the acceptance run")
	}
	env = append(env, "GM_ACCEPTANCE_ADDR="+addr, "GM_ACCEPTANCE_URL=http://"+addr,
		"GM_ACCEPTANCE_BIN="+rc.binDir, "GM_ACCEPTANCE_PIDFILE="+rc.acceptPidFile())
	_ = os.Remove(rc.acceptPidFile())
	sctx, cancel := context.WithCancel(ctx)
	sp := &serveProc{cancel: cancel, done: make(chan runner.Result, 1), addr: addr}
	go func() {
		sp.done <- rc.chk.Run(sctx, runner.Spec{Dir: rc.o.Repo, Argv: runner.Shell(sv.Command), Env: env})
	}()
	rc.serveAddr = addr

	wait := rc.acceptanceTimeout()
	if wait <= 0 || wait > maxServeReady {
		wait = maxServeReady
	}
	deadline := time.Now().Add(wait)
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for {
		select {
		case <-sp.done:
			sp.over = true
			rc.serveAddr = ""
			cancel()
			if ctx.Err() != nil {
				return nil, ctxErrOr(ctx)
			}
			return nil, serveStop("the server of the plan's serve declaration exited before it was ready (acceptance_serve)")
		default:
		}
		if ctx.Err() != nil {
			rc.dropServe(sp)
			return nil, ctxErrOr(ctx)
		}
		if resp, err := client.Get("http://" + addr + sv.Ready); err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return sp, nil
			}
		}
		if time.Now().After(deadline) {
			rc.dropServe(sp)
			return nil, serveStop("the server of the plan's serve declaration was not ready in time (acceptance_serve)")
		}
		select {
		case <-time.After(100 * time.Millisecond):
		case <-sp.done:
			sp.over = true
			rc.serveAddr = ""
			cancel()
			return nil, serveStop("the server of the plan's serve declaration exited before it was ready (acceptance_serve)")
		}
	}
}

// dropServe kills the server's process group and waits for it. Safe to call
// more than once.
func (rc *runCtx) dropServe(sp *serveProc) {
	if sp == nil || sp.over {
		return
	}
	sp.over = true
	sp.cancel()
	<-sp.done
	rc.serveAddr = ""
}

// stopServe ends the server and runs the leak check on its address.
func (rc *runCtx) stopServe(sp *serveProc) *stopError {
	rc.dropServe(sp)
	return rc.afterCommand(sp.addr, nil)
}

// checkAcceptTests proves every Go acceptance test is still the file the
// planner recorded: nothing between planning and the run may edit a test.
func (rc *runCtx) checkAcceptTests() *stopError {
	ids := make([]string, 0, len(rc.plan.AcceptTests))
	for id := range rc.plan.AcceptTests {
		ids = append(ids, id)
	}
	sortStrings(ids)
	for _, id := range ids {
		at := rc.plan.AcceptTests[id]
		raw, err := readNoFollow(joinRepo(rc.o.Repo, at.TestFile))
		if err != nil || hashHex(raw) != at.SHA256 {
			return &stopError{Status: "failed", Reason: "test_file_changed",
				Message: "executor: the acceptance test of " + id + " changed since it was planned"}
		}
	}
	return nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func joinRepo(repo, rel string) string {
	return repo + "/" + strings.TrimPrefix(rel, "/")
}
