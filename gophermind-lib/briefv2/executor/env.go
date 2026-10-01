package executor

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"gophermind/gophermind-lib/briefv2/execenv"
	"gophermind/gophermind-lib/briefv2/runner"
	"gophermind/gophermind-lib/briefv2/vault"
)

// envMode says which Go policy an environment carries.
type envMode int

const (
	// envLeaf: offline. A leaf command can never fetch (spec 14, R7).
	envLeaf envMode = iota
	// envDeps: the one step that may fetch, through the harness proxy. Nothing
	// else in the run uses it.
	envDeps
	// envAcceptance: envLeaf with <run>/bin first on PATH.
	envAcceptance
)

// goCmdTimeout bounds one Go command run through goCmd.
const goCmdTimeout = 10 * time.Minute

// env builds the exact environment of one command of node nodeID. Nothing is
// inherited from the harness's own environment. The deps step gets neither the
// brief's environment nor its secrets: it runs no code of the plan's and needs
// neither.
func (rc *runCtx) env(nodeID string, mode envMode) ([]string, error) {
	tc := map[string]string{}
	for k, v := range rc.cfg.Toolchain {
		tc[k] = v
	}
	if mode == envAcceptance {
		tc["PATH"] = rc.binDir + string(filepath.ListSeparator) + tc["PATH"]
	}
	tc["HOME"] = rc.scratch
	tc["TMPDIR"] = rc.scratch
	tc["GOCACHE"] = rc.goCache
	tc["GOMODCACHE"] = rc.modCache
	tc["GOPATH"] = filepath.Join(rc.scratch, "gopath")
	tc["CGO_ENABLED"] = "0" // decision E13

	goEnv := map[string]string{"GOTOOLCHAIN": "local", "GOPRIVATE": "off"}
	switch mode {
	case envDeps:
		goEnv["GOPROXY"] = testHooks.GoProxy
		goEnv["GOFLAGS"] = "-mod=mod -buildvcs=false"
		if testHooks.GoSumDB != "" {
			goEnv["GOSUMDB"] = testHooks.GoSumDB
		}
	default:
		goEnv["GOPROXY"] = "off"
		goEnv["GOFLAGS"] = "-mod=readonly -buildvcs=false"
		goEnv["GOSUMDB"] = "off"
	}

	in := execenv.Inputs{NodeID: nodeID, Toolchain: tc, GoEnv: goEnv}
	if rc.prox != nil {
		in.ProxyURL = rc.prox.URL(nodeID)
	}
	if mode != envDeps {
		in.Env = rc.plan.Brief.Front.Env
		for _, s := range rc.plan.Brief.Front.Secrets {
			in.Secrets = append(in.Secrets, s.Name)
		}
		if rc.o.Secrets != nil {
			scope := vault.RunScope(rc.plan.RunID)
			in.SecretValues = func(names []string) ([]string, error) { return rc.o.Secrets.Env(scope, names) }
		}
	}
	out, err := execenv.Build(in)
	if err != nil || mode != envAcceptance {
		return out, err
	}
	// The acceptance run belongs to no node: execenv insists on an id, and the
	// variable that carries it is dropped again.
	kept := out[:0]
	for _, e := range out {
		if !strings.HasPrefix(e, "GOPHERMIND_NODE=") {
			kept = append(kept, e)
		}
	}
	return kept, nil
}

// goCmd runs one go command in the repository. Only envDeps makes the module
// cache writable, through the per-call Spec field. Output is reachable only
// through Result.Out.Text.
func (rc *runCtx) goCmd(ctx context.Context, nodeID string, mode envMode, args ...string) runner.Result {
	env, err := rc.env(nodeID, mode)
	if err != nil {
		return runner.Result{ExitCode: -1, Err: err}
	}
	return rc.chk.Run(ctx, runner.Spec{
		Dir: rc.o.Repo, Argv: append([]string{rc.goBin}, args...), Env: env,
		Timeout: goCmdTimeout, ModCacheWritable: mode == envDeps,
	})
}

func (rc *runCtx) testTimeout() time.Duration {
	return time.Duration(rc.cfg.Executor.TestTimeoutSeconds) * time.Second
}

// leafCheck is runner.LeafCheck over the named files of the leaf.
func (rc *runCtx) leafCheck(ctx context.Context, l *Leaf, files []string) runner.Verdict {
	env, err := rc.env(l.ID, envLeaf)
	if err != nil {
		return runner.Verdict{Class: runner.ClassHarness, Err: err}
	}
	return rc.chk.CheckLeaf(ctx, runner.LeafCheck{
		Repo: rc.o.Repo, Dir: l.Dir, TestFunc: l.TestFunc, Files: files, Env: env, TestTimeout: rc.testTimeout(),
	})
}

// checkLeaf is runner.LeafCheck on the leaf's real file.
func (rc *runCtx) checkLeaf(ctx context.Context, l *Leaf) runner.Verdict {
	return rc.leafCheck(ctx, l, []string{l.File})
}

// secretValues is every declared secret's value, for stripping from text.
// The values are never logged.
func (rc *runCtx) secretValues() []string {
	if rc.o.Secrets == nil {
		return nil
	}
	var out []string
	scope := vault.RunScope(rc.plan.RunID)
	for _, s := range rc.plan.Brief.Front.Secrets {
		if v, ok := rc.o.Secrets.Get(scope, s.Name); ok && v != "" {
			out = append(out, v)
		}
	}
	return out
}
