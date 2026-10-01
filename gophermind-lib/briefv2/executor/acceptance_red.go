package executor

// The red check of acceptance (spec 9): a root test that exercises the server
// and passes against the stubs proves nothing. At Wave 0, once the stubs
// compile, every such root test runs against the stub repository and must fail.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gophermind/gophermind-lib/briefv2/acceptcheck"
	"gophermind/gophermind-lib/briefv2/planner"
)

// maxRedTimeout caps one red check command: a command that is still running
// then did not pass quickly against a stub, which counts as red.
const maxRedTimeout = 120 * time.Second

func (rc *runCtx) redTimeout() time.Duration {
	d := rc.acceptanceTimeout()
	if d <= 0 || d > maxRedTimeout {
		d = maxRedTimeout
	}
	if testHooks.RedTimeout > 0 {
		d = testHooks.RedTimeout
	}
	return d
}

// notRedError names the bullets whose server-exercising root tests all pass
// against the stubs. Ids only.
type notRedError struct{ IDs []string }

func (e *notRedError) Error() string {
	return "executor: the root tests of " + strings.Join(e.IDs, ", ") + " pass against the stubs, so they prove nothing (acceptance_not_red)"
}

// redCheckAcceptance runs the server-exercising root tests of every acceptance
// bullet and checked constraint against the Wave 0 stubs. Commands that only
// check the code statically (go build, go vet, gofmt, grep) are exempt. A bullet
// is red when at least one of its server-exercising tests fails or times out.
func (rc *runCtx) redCheckAcceptance(ctx context.Context) error {
	if rc.noRedCheck {
		return nil
	}
	bins := rc.plan.binNames()
	type cand struct {
		pb    planBullet
		tests []planner.RootTest
	}
	var cands []cand
	for _, list := range [][]planBullet{rc.accept.acceptance, rc.accept.constraints} {
		for _, pb := range list {
			var ts []planner.RootTest
			for _, t := range pb.tests {
				if acceptcheck.ServerExercising(t.Command, bins) {
					ts = append(ts, t)
				}
			}
			if len(ts) > 0 {
				cands = append(cands, cand{pb, ts})
			}
		}
	}
	if len(cands) == 0 {
		return nil
	}
	// The binaries of the stubs; a repository with no cmd packages yet has none
	// and every command that needs one fails, which is red.
	if err := rc.buildBinaries(ctx); err != nil {
		if se, ok := stopOf(err); !ok || se.Reason != "acceptance_build" {
			return err
		}
		rc.bins = map[string]string{}
	}
	defer func() { rc.removeBinaries(); rc.bins = nil }()
	var notRed []string
	for _, c := range cands {
		red := false
		unset := rc.unsetNames(c.pb.req.Text)
		for _, t := range c.tests {
			proof, _, err := rc.runRootTestFull(ctx, t, rc.redTimeout(), unset...)
			if err != nil {
				if _, ok := stopOf(err); ok {
					return err
				}
				if ctx.Err() != nil {
					return ctxErrOr(ctx)
				}
				return fmt.Errorf("executor: the red check of %s could not run: %w", c.pb.req.ID, err)
			}
			if !proof.Passed {
				red = true
			}
		}
		if red {
			rc.emit("acceptance_red", c.pb.req.ID, "fails against the stubs")
			continue
		}
		rc.emit("acceptance_red", c.pb.req.ID, "passes against the stubs")
		notRed = append(notRed, c.pb.req.ID)
	}
	if len(notRed) > 0 {
		return &stopError{Status: "failed", Reason: "acceptance_not_red", Message: (&notRedError{IDs: notRed}).Error()}
	}
	return nil
}
