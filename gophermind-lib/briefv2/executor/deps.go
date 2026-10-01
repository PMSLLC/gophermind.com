package executor

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/runner"
)

// depsHooks are package level test hooks; the defaults are the real values.
type depsHooks struct {
	GoProxy string // "https://proxy.golang.org"
	GoSumDB string // "" means the go default (sum.golang.org)
	// RedTimeout, when positive, is the time limit of one acceptance red check
	// command (tests make it short: a command that has not passed by then is red).
	RedTimeout time.Duration
}

var testHooks = depsHooks{GoProxy: "https://proxy.golang.org"}

var (
	// A dotted host first, so a standard library path or a local path is refused.
	depModulePath = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*\.[a-z][a-z0-9]*(/[A-Za-z0-9._~-]+)*$`)
	// A tagged release, a pre-release or a pseudo-version. Never latest, a branch or a range.
	depVersion = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+incompatible)?$`)
)

// vetDeps checks every dependency before any go command runs. A message names
// the entry's index and the kind of problem, never the value: the value came
// from a model.
func vetDeps(deps []planner.Dependency) error {
	seen := map[string]bool{}
	for i, d := range deps {
		kind := ""
		switch {
		case !depModulePath.MatchString(d.Module) || hasDotElement(d.Module):
			kind = "the module path is not a plausible module path"
		case !depVersion.MatchString(d.Version):
			kind = "the version is not a tagged release or pseudo-version"
		case seen[d.Module]:
			kind = "the module is listed twice"
		}
		if kind != "" {
			return &stopError{Status: "failed", Reason: "deps_invalid",
				Message: fmt.Sprintf("executor: dependencies.json entry %d is not acceptable: %s", i, kind)}
		}
		seen[d.Module] = true
	}
	return nil
}

func hasDotElement(module string) bool {
	for _, e := range strings.Split(module, "/") {
		if e == "." || e == ".." {
			return true
		}
	}
	return false
}

// unreachableText are fragments of a go command's output that mean the module
// proxy could not be reached (as opposed to a module or version it does not
// have). They are matched in memory and never stored.
var unreachableText = []string{
	"connection refused", "no such host", "dial tcp", "i/o timeout", "tls handshake", "network is unreachable",
	"temporary failure in name resolution", "proxyconnect", "bad gateway", "service unavailable",
	"gateway timeout", "context deadline exceeded", "connection reset",
}

func unreachable(res runner.Result) bool {
	if res.TimedOut {
		return true
	}
	text := strings.ToLower(res.Out.Text())
	for _, frag := range unreachableText {
		if strings.Contains(text, frag) {
			return true
		}
	}
	return false
}

// depsStop is the stop for a failed deps command. The message holds the module,
// the version and an exit code, never command output.
func depsStop(what string, res runner.Result) *stopError {
	if res.Err == nil && unreachable(res) {
		return &stopError{Status: "failed", Reason: "deps_unreachable",
			Message: fmt.Sprintf("executor: deps step: the module proxy is not reachable (%s): exit %d", what, res.ExitCode)}
	}
	if res.Err != nil {
		return &stopError{Status: "failed", Reason: "deps_failed",
			Message: fmt.Sprintf("executor: deps step: %s: could not run the go command", what)}
	}
	return &stopError{Status: "failed", Reason: "deps_failed",
		Message: fmt.Sprintf("executor: deps step: %s: exit %d", what, res.ExitCode)}
}

// runDeps is spec 14 steps 2 to 4 (step 1 and 5 are in wave0): for each
// dependency go get module@version, then go mod download, with the proxy and
// the checksum database allowed. It is the only networked step of a run. An
// empty dependency list touches nothing.
func (rc *runCtx) runDeps(ctx context.Context) error {
	deps := append([]planner.Dependency(nil), rc.plan.Deps...)
	if len(deps) == 0 {
		return nil
	}
	if err := vetDeps(deps); err != nil {
		return err
	}
	sort.Slice(deps, func(i, j int) bool { return deps[i].Module < deps[j].Module })
	rc.emit("deps_start", "", fmt.Sprintf("%d module(s)", len(deps)))
	for _, d := range deps {
		spec := d.Module + "@" + d.Version
		if res := rc.goCmd(ctx, "deps", envDeps, "get", spec); res.Err != nil || res.ExitCode != 0 {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return depsStop(spec, res)
		}
		rc.emit("deps_module", "", spec)
	}
	if res := rc.goCmd(ctx, "deps", envDeps, "mod", "download"); res.Err != nil || res.ExitCode != 0 {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return depsStop("go mod download", res)
	}
	rc.emit("deps_done", "", fmt.Sprintf("%d module(s)", len(deps)))
	return nil
}

// verifyModules is go mod verify, offline. Finish (Tasks 12b and 13) calls it
// before landing; a mismatch stops the run failed.
func (rc *runCtx) verifyModules(ctx context.Context) error {
	res := rc.goCmd(ctx, "verify", envLeaf, "mod", "verify")
	if res.Err != nil || res.ExitCode != 0 {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &stopError{Status: "failed", Reason: "mod_verify",
			Message: fmt.Sprintf("executor: go mod verify failed: exit %d", res.ExitCode)}
	}
	return nil
}
