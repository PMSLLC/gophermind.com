// Package executor runs an approved v2 plan: it loads the plan the planner
// left in the run folder, refuses to run it if the approval no longer matches,
// and builds each leaf wave by wave. This file is the seams: the small
// interfaces the executor depends on, each with a compile-time assertion that
// the real implementation satisfies it.
package executor

import (
	"context"

	"gophermind/gophermind-lib/briefv2/gitland"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/runner"
	"gophermind/gophermind-lib/briefv2/vault"
)

// Checker is the part of *runner.Runner the executor calls. *runner.Runner
// satisfies it unchanged; tests use a fake that returns scripted Verdicts.
// There is no other seam: packer, gitland and proxy are used by their own types.
type Checker interface {
	Run(ctx context.Context, s runner.Spec) runner.Result
	CheckLeaf(ctx context.Context, c runner.LeafCheck) runner.Verdict
	BuildVet(ctx context.Context, repo string, env []string) runner.Verdict
	Test(ctx context.Context, t runner.TestSet) runner.Verdict
}

// Caller is the router method the executor uses; *router.Router satisfies it
// (same shape as planner.Caller).
type Caller interface {
	CallParsed(ctx context.Context, info router.CallInfo, req provider.Request, parse func(text string) error) (router.Result, error)
}

// Secrets is the part of *vault.Vault the executor needs.
type Secrets interface {
	Env(scope string, names []string) ([]string, error)
	Get(scope, name string) (string, bool)
}

var (
	_ Checker      = (*runner.Runner)(nil)
	_ Caller       = (*router.Router)(nil)
	_ Secrets      = (*vault.Vault)(nil)
	_ gitland.Repo = (*gitland.CLI)(nil)

	// Options.LedgerErrors is (*router.Router).LedgerErrors.
	_ func() int = (*router.Router)(nil).LedgerErrors
)
